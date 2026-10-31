package agent

import (
	"encoding/base64"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/imagefit"
)

// fitImagesForRequest keeps a history that holds more than 20 image or
// document blocks within the stricter per-image limit the API then applies to
// every one of them, old ones included (see package imagefit). Over the
// threshold, each base64 image larger than maxSide is scaled down; one that
// cannot be (WebP, or undecodable) is replaced by a short note. At or under
// the threshold nothing is touched, so the common case costs one count.
//
// The caller keeps the result, so each image is fitted once, not on every
// turn. Fitting changes the history before the newest thinking block, which
// the API then drops once (and the loop removes) — the price of a session
// that can continue at all: the alternative was a 400 on every request.
//
// It returns how many images it scaled and how many it had to omit.
func fitImagesForRequest(messages []anthropic.BetaMessageParam, maxSide int) ([]anthropic.BetaMessageParam, int, int) {
	if countVisualBlocks(messages) <= imagefit.ManyImageThreshold {
		return messages, 0, 0
	}
	scaled, omitted := 0, 0
	// fit returns a replacement for img, or nil when img must be omitted;
	// the second result reports whether anything changed.
	fit := func(img *anthropic.BetaImageBlockParam) (*anthropic.BetaImageBlockParam, bool) {
		src := img.Source.OfBase64
		if src == nil {
			return img, false // a URL or file reference: nothing to measure
		}
		data, err := base64.StdEncoding.DecodeString(src.Data)
		if err != nil {
			omitted++
			return nil, true
		}
		if w, h, ok := imagefit.Size(data); ok && w <= maxSide && h <= maxSide {
			return img, false
		}
		f, err := imagefit.Fit(data, string(src.MediaType), maxSide)
		if err != nil {
			omitted++
			return nil, true
		}
		scaled++
		out := *img
		out.Source = anthropic.BetaImageBlockParamSourceUnion{OfBase64: &anthropic.BetaBase64ImageSourceParam{
			Data:      base64.StdEncoding.EncodeToString(f.Data),
			MediaType: anthropic.BetaBase64ImageSourceMediaType(f.MediaType),
		}}
		return &out, true
	}
	note := func() string {
		return fmt.Sprintf("[image omitted: over %d px on a side and could not be resized, in a request "+
			"holding more than %d images, where the API rejects it]", maxSide, imagefit.ManyImageThreshold)
	}

	out := make([]anthropic.BetaMessageParam, len(messages))
	copy(out, messages)
	for i, m := range out {
		// Copied only once something changes. Tracked by a flag, not by the
		// slice being non-nil: a copy of an empty prefix is nil, and the
		// first version lost any message whose first block was the one fitted
		// (its tool result vanished whole).
		var content []anthropic.BetaContentBlockParamUnion
		changed, copying := false, false
		for j, b := range m.Content {
			nb := b
			switch {
			case b.OfImage != nil:
				if img, ch := fit(b.OfImage); ch {
					changed = true
					if img == nil {
						nb = anthropic.NewBetaTextBlock(note())
					} else {
						nb = anthropic.BetaContentBlockParamUnion{OfImage: img}
					}
				}
			case b.OfToolResult != nil:
				var parts []anthropic.BetaToolResultBlockParamContentUnion
				partChanged, partCopying := false, false
				for k, p := range b.OfToolResult.Content {
					np := p
					if p.OfImage != nil {
						if img, ch := fit(p.OfImage); ch {
							partChanged = true
							if img == nil {
								np = anthropic.BetaToolResultBlockParamContentUnion{OfText: &anthropic.BetaTextBlockParam{Text: note()}}
							} else {
								np = anthropic.BetaToolResultBlockParamContentUnion{OfImage: img}
							}
						}
					}
					if partChanged && !partCopying {
						partCopying = true
						parts = append(make([]anthropic.BetaToolResultBlockParamContentUnion, 0, len(b.OfToolResult.Content)), b.OfToolResult.Content[:k]...)
					}
					if partCopying {
						parts = append(parts, np)
					}
				}
				if partChanged {
					changed = true
					tr := *b.OfToolResult
					tr.Content = parts
					nb = anthropic.BetaContentBlockParamUnion{OfToolResult: &tr}
				}
			}
			if changed && !copying {
				copying = true
				content = append(make([]anthropic.BetaContentBlockParamUnion, 0, len(m.Content)), m.Content[:j]...)
			}
			if copying {
				content = append(content, nb)
			}
		}
		if changed {
			out[i].Content = content
		}
	}
	return out, scaled, omitted
}

// countVisualBlocks counts image and document blocks, including those inside
// tool results — the API counts those too.
func countVisualBlocks(messages []anthropic.BetaMessageParam) int {
	n := 0
	for _, m := range messages {
		for _, b := range m.Content {
			if b.OfImage != nil || b.OfDocument != nil {
				n++
			}
			if b.OfToolResult != nil {
				for _, p := range b.OfToolResult.Content {
					if p.OfImage != nil || p.OfDocument != nil {
						n++
					}
				}
			}
		}
	}
	return n
}

// imageFitNote is the notice for a fit: what was changed, and why, so a
// sudden drop in an old screenshot's resolution is not a mystery.
func imageFitNote(scaled, omitted, maxSide int) string {
	msg := fmt.Sprintf("scaled %d earlier image(s) down to %d px", scaled, maxSide)
	if omitted > 0 {
		msg = fmt.Sprintf("%s and omitted %d that could not be resized", msg, omitted)
	}
	return msg + fmt.Sprintf(": a request with more than %d images may not hold larger ones", imagefit.ManyImageThreshold)
}
