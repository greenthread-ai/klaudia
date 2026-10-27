package agent

import "github.com/anthropics/anthropic-sdk-go"

// Web-search citations recorded by an older build can be structurally broken,
// and the API rejects the whole request when they are.
//
// SDK versions before v1.68.0 lost part of a web-search citation on the way
// from response to request param: BetaCitationsWebSearchResultLocation.
// toParamUnion copied the title and cited text but dropped the url and
// encrypted_index, both of which the API marks required on input. A turn
// recorded that way (via assistant.ToParam()) carried an empty url, so the
// moment it was replayed the request failed with
//
//	messages.N.content.M.text.citations.0.web_search_result_location.url:
//	Value should have at least 1 item after validation, not 0
//
// and every follow-up re-sent the same poisoned history. The SDK bug is fixed
// as of v1.68.0 (this module is on it), so freshly recorded turns are sound.
// What remains is transcripts written by the old build: their url is gone for
// good, so the only repair at send time is to drop the broken citation, leaving
// the text it annotated intact. sanitizeMessages runs this on every request.

// repairEmptyWebSearchCitations drops any web-search citation whose URL is empty
// (unrecoverable), keeping the annotated text. Returns the repaired content and
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
