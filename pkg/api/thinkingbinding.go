package api

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// Preserved thinking. On Fable 5.1, Opus 5.5 and Sonnet 5.5 a thinking
// block's signature records the conversation that produced it: the system
// prompt, the tool set and every message before it. Replaying a block after
// any of that changed fails the check, and for accounts created on or after
// 2026-08-31 a failing block is a 400 that every resend repeats. Older
// accounts are not enforced, which is why an edited-history probe on one
// passed (PR #63).
//
// The safety net: once the conversation holds a thinking block, requests ask
// the API to drop a mismatched block instead of refusing, and the response
// lists each drop in input_transformations, which the loop reports and then
// removes from its history (resolveDropped) — the API drops a block for one
// request only, so a kept block was dropped again every turn. Measured live:
// only the mismatched block goes; blocks written after it stay valid. The
// model re-plans without that reasoning: degraded, not broken. Setting the field also opts an unenforced account into the
// check, so the drops are visible from any account, which is how the causes
// are found and fixed.

// ThinkingBindingBeta gates thinking.block_binding and input_transformations.
const ThinkingBindingBeta = "thinking-binding-controls-2026-08-01"

// InputTransformation is one entry of a response's input_transformations: a
// thinking block the API dropped (Type "thinking_dropped") or let through
// although it failed the check. Reason is "prefix_binding_mismatch" (the
// history before Path changed since the block was written) or
// "model_binding_mismatch" (the conversation switched models). Unknown types
// and reasons are passed on, not interpreted: later checks add values.
type InputTransformation struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
	Path   string `json:"path"`
	// Signature identifies the dropped block in the caller's history: its
	// thinking signature (or redacted-thinking data), resolved by the client
	// from the request it actually sent. The path cannot be used for that
	// directly — it indexes the shaped request, and shaping adds and removes
	// messages (tool reveals) — but the signature survives shaping untouched.
	// Empty when the path did not resolve to a thinking block.
	Signature string `json:"-"`
}

// resolveDropped fills in each transformation's Signature from the request
// that was sent.
//
// It exists because a drop is permanent and the caller did not know it. The
// API drops a mismatched block for that request only; the caller's history
// still holds it, so it went out again on every later request and was dropped
// again — measured live: a system-prompt change at one request, then the same
// block dropped at messages.1.content.0 on every request after. That is the
// "N earlier thinking block(s) dropped" notice repeating every turn of a
// resumed session. With the signature the loop removes the block once, and
// (measured) the next request reports nothing and later blocks stay valid.
func resolveDropped(ts []InputTransformation, params anthropic.BetaMessageNewParams) {
	for i := range ts {
		var m, c int
		if _, err := fmt.Sscanf(ts[i].Path, "messages.%d.content.%d", &m, &c); err != nil {
			continue
		}
		if m < 0 || m >= len(params.Messages) || c < 0 || c >= len(params.Messages[m].Content) {
			continue
		}
		b := params.Messages[m].Content[c]
		switch {
		case b.OfThinking != nil:
			ts[i].Signature = b.OfThinking.Signature
		case b.OfRedactedThinking != nil:
			ts[i].Signature = b.OfRedactedThinking.Data
		}
	}
}

// historyHasThinking reports whether any assistant message carries a thinking
// block. Only then is block_binding sent, which needs thinking stated
// explicitly: Klaudia leaves the parameter unset and takes each model's
// default, and on a model whose default is no thinking (Opus 4.8) an explicit
// adaptive config would turn it on. A thinking block in the history means the
// model already thinks by default, so stating adaptive changes nothing, and
// no model list is needed to know it.
func historyHasThinking(messages []anthropic.BetaMessageParam) bool {
	for _, m := range messages {
		if m.Role != anthropic.BetaMessageParamRoleAssistant {
			continue
		}
		for _, b := range m.Content {
			if b.OfThinking != nil || b.OfRedactedThinking != nil {
				return true
			}
		}
	}
	return false
}

// bindThinking sets thinking to adaptive with block_binding "drop_block" and
// adds the beta. It leaves a caller's own thinking configuration alone.
func bindThinking(params *anthropic.BetaMessageNewParams) {
	if params.Thinking.OfAdaptive == nil && params.Thinking.OfEnabled == nil && params.Thinking.OfDisabled == nil {
		a := anthropic.BetaThinkingConfigAdaptiveParam{}
		params.Thinking = anthropic.BetaThinkingConfigParamUnion{OfAdaptive: &a}
	}
	extra := map[string]any{"block_binding": map[string]any{"prefix_mismatch_behavior": "drop_block"}}
	switch {
	case params.Thinking.OfAdaptive != nil:
		params.Thinking.OfAdaptive.SetExtraFields(extra)
	case params.Thinking.OfEnabled != nil:
		params.Thinking.OfEnabled.SetExtraFields(extra)
	default:
		return // thinking disabled: nothing to bind
	}
	for _, b := range params.Betas {
		if string(b) == ThinkingBindingBeta {
			return
		}
	}
	params.Betas = append(append([]anthropic.AnthropicBeta{}, params.Betas...), ThinkingBindingBeta)
}

// transformationsOf reads input_transformations from a message_start event's
// message. The SDK has no field for it, so it comes from the raw JSON.
func transformationsOf(ev anthropic.BetaRawMessageStreamEventUnion) []InputTransformation {
	if ev.Type != "message_start" {
		return nil
	}
	var m struct {
		InputTransformations []InputTransformation `json:"input_transformations"`
	}
	if json.Unmarshal([]byte(ev.Message.RawJSON()), &m) != nil {
		return nil
	}
	return m.InputTransformations
}

// isBindingRejected reports whether an endpoint refused the binding controls
// themselves (a proxy or partner endpoint that does not know the beta or the
// field), as opposed to a thinking block failing the check.
func isBindingRejected(err error) bool {
	status, ok := apiStatus(err)
	if !ok || status != 400 {
		return false
	}
	d := strings.ToLower(errDetail(err))
	return strings.Contains(d, "block_binding") || strings.Contains(d, ThinkingBindingBeta)
}

// IsThinkingBindingError reports whether err is the API refusing a thinking
// block bound to a different conversation (an enforced account, with no
// drop_block in effect):
//
//	Invalid `signature` in `thinking` block. The block is bound to a different
//	conversation. Remove the block, or set ... "drop_block".
func IsThinkingBindingError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(errDetail(err)), "bound to a different conversation")
}

// errDetail is the provider's own message, or the error text when there is
// none, as IsImmutableThinking reads it.
func errDetail(err error) string {
	if d := providerDetail(err); d != "" {
		return d
	}
	return err.Error()
}
