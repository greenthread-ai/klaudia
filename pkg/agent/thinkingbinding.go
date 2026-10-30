package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/api"
)

// droppedThinkingNote words a response's input_transformations for the user,
// or "" when nothing was dropped. A prefix mismatch is the harness's doing:
// something before the block changed since it was written, and the path says
// where the check failed. A model mismatch follows a model switch (a fallback,
// /model) and is expected. Other entry types (a block let through on an
// unenforced request) are not news to the user. (PR #63.)
func droppedThinkingNote(ts []api.InputTransformation) string {
	reasons := map[string]int{}
	var paths []string
	for _, t := range ts {
		if t.Type != "thinking_dropped" {
			continue
		}
		reasons[t.Reason]++
		if t.Reason == "prefix_binding_mismatch" && t.Path != "" {
			paths = append(paths, t.Path)
		}
	}
	if len(reasons) == 0 {
		return ""
	}
	var parts []string
	if n := reasons["prefix_binding_mismatch"]; n > 0 {
		s := fmt.Sprintf("%d earlier thinking block(s) dropped: the conversation before them changed after they were written", n)
		if len(paths) > 0 {
			sort.Strings(paths)
			s += " (first at " + paths[0] + ")"
		}
		parts = append(parts, s)
	}
	if n := reasons["model_binding_mismatch"]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d thinking block(s) from another model dropped", n))
	}
	for r, n := range reasons {
		if r != "prefix_binding_mismatch" && r != "model_binding_mismatch" {
			parts = append(parts, fmt.Sprintf("%d thinking block(s) dropped (%s)", n, r))
		}
	}
	return strings.Join(parts, "; ")
}

// stripThinking returns messages with every thinking and redacted_thinking
// block removed from assistant turns. Removing all of them is removing a
// leading run, which preserved thinking's check accepts; removing some from
// the middle is not. An assistant turn left with no content keeps a short text
// block, since the API rejects an empty one.
func stripThinking(messages []anthropic.BetaMessageParam) []anthropic.BetaMessageParam {
	out := make([]anthropic.BetaMessageParam, len(messages))
	for i, m := range messages {
		out[i] = m
		if m.Role != anthropic.BetaMessageParamRoleAssistant {
			continue
		}
		var kept []anthropic.BetaContentBlockParamUnion
		changed := false
		for _, b := range m.Content {
			if b.OfThinking != nil || b.OfRedactedThinking != nil {
				changed = true
				continue
			}
			kept = append(kept, b)
		}
		if !changed {
			continue
		}
		if len(kept) == 0 {
			kept = []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock("(reasoning omitted)")}
		}
		out[i].Content = kept
	}
	return out
}

// withoutThinkingSigned removes the thinking blocks with the given signatures
// (or redacted-thinking data) from history, leaving every other block alone.
//
// Removing a block from the middle is safe here, unlike in general: the API
// already removed it from the conversation it worked from, so the blocks it
// wrote after are bound to the history without it — measured live, the next
// request reports no drops and the newer blocks stay valid. An assistant
// message left empty gets the same placeholder stripThinking uses.
func withoutThinkingSigned(messages []anthropic.BetaMessageParam, sigs []string) []anthropic.BetaMessageParam {
	if len(sigs) == 0 {
		return messages
	}
	dead := make(map[string]bool, len(sigs))
	for _, s := range sigs {
		dead[s] = true
	}
	out := make([]anthropic.BetaMessageParam, len(messages))
	for i, m := range messages {
		out[i] = m
		if m.Role != anthropic.BetaMessageParamRoleAssistant {
			continue
		}
		var kept []anthropic.BetaContentBlockParamUnion
		changed := false
		for _, b := range m.Content {
			if (b.OfThinking != nil && dead[b.OfThinking.Signature]) ||
				(b.OfRedactedThinking != nil && dead[b.OfRedactedThinking.Data]) {
				changed = true
				continue
			}
			kept = append(kept, b)
		}
		if !changed {
			continue
		}
		if len(kept) == 0 {
			kept = []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock("(reasoning omitted)")}
		}
		out[i].Content = kept
	}
	return out
}

// droppedSignatures picks out the signatures the client resolved for the
// dropped blocks; a transformation it could not resolve has none.
func droppedSignatures(ts []api.InputTransformation) []string {
	var sigs []string
	for _, t := range ts {
		if t.Signature != "" {
			sigs = append(sigs, t.Signature)
		}
	}
	return sigs
}
