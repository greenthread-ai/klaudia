package api

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// Revealing a deferred tool without changing the tool list.
//
// Deferred tools (MCP, the rarer built-ins) stay out of the model's context
// until ToolSearch reveals them. Klaudia used to do that by adding the tool to
// the request's tools array, which on a preserved-thinking model (Opus 5.5,
// Fable 5.1, Sonnet 5.5) changed the prefix every earlier thinking block was
// bound to: each reveal dropped the reasoning so far, or for an enforced
// account was a 400 (thinkingbinding.go).
//
// The loop now sends every tool on every request, the deferred ones with
// defer_loading (hidden, and outside the binding check until referenced), and
// records each reveal as a marker block in the tool-result message that
// carried it. The marker is never sent as written. The Anthropic client turns
// each run of markers into a role "system" message of tool_addition blocks
// placed directly after the message that held them, which appends to the
// conversation rather than editing it. Where that is refused it falls back:
// a model without mid-conversation tool changes (Sonnet 5) gets the revealed
// tools with the flag cleared, and an endpoint that refuses defer_loading
// itself (and the OpenAI shim) gets unrevealed deferred tools left out, which
// is how every request used to look.
//
// Because the reveals live in the conversation, they survive the end of a run
// and a resume: the set used to start empty on every Run, so a tool revealed
// in one prompt was gone in the next and the tool list changed again.

// ToolChangesBeta gates role "system" messages carrying tool_addition.
const ToolChangesBeta = "mid-conversation-tool-changes-2026-07-01"

// revealPrefix starts a marker block's text; the tool's name follows.
const revealPrefix = "[klaudia:tool-revealed] "

// RevealBlock is the marker recording that ToolSearch revealed name.
func RevealBlock(name string) anthropic.BetaContentBlockParamUnion {
	return anthropic.NewBetaTextBlock(revealPrefix + name)
}

// RevealedName returns the tool a marker block names, and whether b is one.
func RevealedName(b anthropic.BetaContentBlockParamUnion) (string, bool) {
	if b.OfText == nil {
		return "", false
	}
	return strings.CutPrefix(b.OfText.Text, revealPrefix)
}

// RevealedTools is every tool the conversation has revealed.
func RevealedTools(messages []anthropic.BetaMessageParam) map[string]bool {
	out := map[string]bool{}
	for _, m := range messages {
		for _, b := range m.Content {
			if name, ok := RevealedName(b); ok {
				out[name] = true
			}
		}
	}
	return out
}

// revealMode is how a request expresses the reveals in its history.
type revealMode int

const (
	// revealByAddition: tool_addition system messages; the tools array never
	// changes.
	revealByAddition revealMode = iota
	// revealByFlag: a revealed tool is sent without defer_loading.
	revealByFlag
	// revealByOmission: no defer_loading at all; an unrevealed deferred tool
	// is left out.
	revealByOmission
)

// revealMode is how this client sends reveals to model.
func (c *Client) revealMode(model anthropic.Model) revealMode {
	if c.noDeferLoading.Load() {
		return revealByOmission
	}
	if _, refused := c.noToolChanges.Load(string(model)); refused {
		return revealByFlag
	}
	return revealByAddition
}

// shapeReveals rewrites params' markers and deferred tools for mode, and
// reports whether it added any tool_addition. It copies what it changes, so
// the caller's messages and tools are left as they were.
func shapeReveals(params *anthropic.BetaMessageNewParams, mode revealMode) (added bool) {
	deferred := map[string]bool{}
	for _, t := range params.Tools {
		if t.OfTool != nil && t.OfTool.DeferLoading.Value {
			deferred[t.OfTool.Name] = true
		}
	}
	revealed := map[string]bool{}
	var msgs []anthropic.BetaMessageParam
	for i, m := range params.Messages {
		var names []string
		var kept []anthropic.BetaContentBlockParamUnion
		for _, b := range m.Content {
			if name, ok := RevealedName(b); ok {
				names = append(names, name)
				continue
			}
			kept = append(kept, b)
		}
		if names == nil {
			if msgs != nil {
				msgs = append(msgs, m)
			}
			continue
		}
		if msgs == nil {
			msgs = append(make([]anthropic.BetaMessageParam, 0, len(params.Messages)+1), params.Messages[:i]...)
		}
		if len(kept) > 0 {
			m.Content = kept
			msgs = append(msgs, m)
		}
		var fresh []string
		for _, n := range names {
			if deferred[n] && !revealed[n] {
				revealed[n] = true
				fresh = append(fresh, n)
			}
		}
		if mode == revealByAddition && len(fresh) > 0 {
			msgs = append(msgs, toolAdditionMessage(fresh))
			added = true
		}
	}
	if msgs != nil {
		params.Messages = msgs
	}

	if mode == revealByAddition || len(deferred) == 0 {
		if added {
			addBeta(params, ToolChangesBeta)
		}
		return added
	}
	tools := make([]anthropic.BetaToolUnionParam, 0, len(params.Tools))
	for _, t := range params.Tools {
		if t.OfTool == nil || !t.OfTool.DeferLoading.Value {
			tools = append(tools, t)
			continue
		}
		switch {
		case revealed[t.OfTool.Name]:
			copied := *t.OfTool
			copied.DeferLoading = param.Opt[bool]{}
			tools = append(tools, anthropic.BetaToolUnionParam{OfTool: &copied})
		case mode == revealByFlag:
			tools = append(tools, t)
		}
	}
	params.Tools = tools
	return false
}

// toolAdditionMessage is the role "system" message that surfaces names. The
// SDK has no type for either, so it goes out as raw JSON.
func toolAdditionMessage(names []string) anthropic.BetaMessageParam {
	sort.Strings(names)
	type ref struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	type block struct {
		Type string `json:"type"`
		Tool ref    `json:"tool"`
	}
	msg := struct {
		Role    string  `json:"role"`
		Content []block `json:"content"`
	}{Role: "system"}
	for _, n := range names {
		msg.Content = append(msg.Content, block{Type: "tool_addition", Tool: ref{Type: "tool_reference", Name: n}})
	}
	raw, _ := json.Marshal(msg)
	return param.Override[anthropic.BetaMessageParam](json.RawMessage(raw))
}

// addBeta appends beta to params unless it is already there.
func addBeta(params *anthropic.BetaMessageNewParams, beta string) {
	for _, b := range params.Betas {
		if string(b) == beta {
			return
		}
	}
	params.Betas = append(append([]anthropic.AnthropicBeta{}, params.Betas...), anthropic.AnthropicBeta(beta))
}

// isToolChangeRejected reports whether a request carrying tool_addition was
// refused for it: a model without mid-conversation tool changes ("tool_addition/
// tool_removal is not supported on this model", measured on Sonnet 5), or an
// endpoint that knows neither the beta nor a role "system" message.
func isToolChangeRejected(err error) bool {
	status, ok := apiStatus(err)
	if !ok || status != 400 {
		return false
	}
	d := strings.ToLower(errDetail(err))
	return strings.Contains(d, "tool_addition") || strings.Contains(d, "tool_reference") ||
		strings.Contains(d, ToolChangesBeta) || strings.Contains(d, "system")
}

// isDeferRejected reports whether an endpoint refused defer_loading itself.
func isDeferRejected(err error) bool {
	status, ok := apiStatus(err)
	if !ok || status != 400 {
		return false
	}
	return strings.Contains(strings.ToLower(errDetail(err)), "defer_loading")
}
