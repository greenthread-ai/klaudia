package agent

import "github.com/anthropics/anthropic-sdk-go"

// A turn that used web search records citations on its text blocks, and the SDK
// loses part of them on the way back to a request param.
//
// BetaCitationsWebSearchResultLocation.toParamUnion (SDK v1.45.0,
// betamessageutil.go) copies Title and CitedText but drops URL and
// EncryptedIndex — yet the API marks both required on input. So the moment that
// turn is replayed (the very next request, or any resume) the request fails with
//
//	messages.N.content.M.text.citations.0.web_search_result_location.url:
//	Value should have at least 1 item after validation, not 0
//
// and the conversation is stuck: every follow-up re-sends the same poisoned
// history. This bit a real session immediately after a successful web search.
//
// toParamRepaired closes the gap at the source — it copies the two dropped
// fields back from the response, which still carries them, before they are lost.
// repairEmptyWebSearchCitations is the matching sanitize step for history that
// was already recorded (or built in memory) before this fix: at send time the
// URL is gone for good, so the only sound repair is to drop the broken citation,
// leaving the text it annotated intact.

// toParamRepaired is ToParam with the web-search citation fields the SDK drops
// restored from the response. Use it everywhere assistant.ToParam() would feed
// a request or the transcript.
func toParamRepaired(m anthropic.BetaMessage) anthropic.BetaMessageParam {
	p := m.ToParam()
	for i := range p.Content {
		tb := p.Content[i].OfText
		if tb == nil || len(tb.Citations) == 0 || i >= len(m.Content) {
			continue
		}
		src := m.Content[i].AsText().Citations
		for j := range tb.Citations {
			c := tb.Citations[j].OfWebSearchResultLocation
			if c == nil || c.URL != "" || j >= len(src) {
				continue
			}
			c.URL = src[j].URL
			c.EncryptedIndex = src[j].EncryptedIndex
		}
	}
	return p
}

// repairEmptyWebSearchCitations drops any web-search citation whose URL did not
// survive into the param (the SDK bug above), since the API rejects it and the
// value cannot be recovered at send time. Returns the repaired content and
// whether anything changed; originals are never mutated.
func repairEmptyWebSearchCitations(content []anthropic.BetaContentBlockParamUnion) ([]anthropic.BetaContentBlockParamUnion, bool) {
	changed := false
	var out []anthropic.BetaContentBlockParamUnion
	for i, b := range content {
		tb := b.OfText
		if tb == nil || !hasEmptyWebSearchCitationBlock(tb) {
			if changed {
				out = append(out, b)
			}
			continue
		}
		// First broken block: copy everything seen so far, then diverge.
		if !changed {
			out = append(out, content[:i]...)
			changed = true
		}
		kept := make([]anthropic.BetaTextCitationParamUnion, 0, len(tb.Citations))
		for _, c := range tb.Citations {
			if w := c.OfWebSearchResultLocation; w != nil && w.URL == "" {
				continue
			}
			kept = append(kept, c)
		}
		fixed := *tb
		fixed.Citations = kept
		nb := b
		nb.OfText = &fixed
		out = append(out, nb)
	}
	if !changed {
		return content, false
	}
	return out, true
}

func hasEmptyWebSearchCitationBlock(tb *anthropic.BetaTextBlockParam) bool {
	for _, c := range tb.Citations {
		if w := c.OfWebSearchResultLocation; w != nil && w.URL == "" {
			return true
		}
	}
	return false
}

// hasEmptyWebSearchCitation reports whether any message carries one, for the
// sanitize fast path.
func hasEmptyWebSearchCitation(messages []anthropic.BetaMessageParam) bool {
	for _, m := range messages {
		for _, b := range m.Content {
			if tb := b.OfText; tb != nil && hasEmptyWebSearchCitationBlock(tb) {
				return true
			}
		}
	}
	return false
}
