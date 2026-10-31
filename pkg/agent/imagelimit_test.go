package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/tool"
)

// imageProvider records what each request carried; with failFirst it rejects
// the first one the way the API does when an image is over the many-image
// limit.
type imageProvider struct {
	mu        sync.Mutex
	failFirst string
	sent      []anthropic.BetaMessageNewParams
}

// ClearsToolResults makes this behave like the live Anthropic client, which
// clears old tool results server-side, so the loop does not microcompact them
// away client-side — the shape in which all 21 images reached the API.
func (p *imageProvider) ClearsToolResults([]anthropic.BetaMessageParam) bool { return true }

func (p *imageProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	p.mu.Lock()
	n := len(p.sent)
	p.sent = append(p.sent, params)
	p.mu.Unlock()
	if n == 0 && p.failFirst != "" {
		return anthropic.BetaMessage{}, errors.New(p.failFirst)
	}
	var m anthropic.BetaMessage
	_ = json.Unmarshal([]byte(`{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}]}`), &m)
	return m, nil
}

func encodedPNG(t *testing.T, w, h int) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

// historyWithScreenshots is n Read results, each carrying one w×h screenshot,
// as tool results in the history — the shape of the stranded session.
func historyWithScreenshots(t *testing.T, n, w, h int) []anthropic.BetaMessageParam {
	img := encodedPNG(t, w, h)
	var msgs []anthropic.BetaMessageParam
	for i := 0; i < n; i++ {
		id := "tu" + strings.Repeat("x", i+1)
		msgs = append(msgs,
			anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
				{OfToolUse: &anthropic.BetaToolUseBlockParam{ID: id, Name: "Read", Input: map[string]any{}}},
			}},
			anthropic.NewBetaUserMessage(toolResultWithImages(id, "[image]", false, []tool.ResultImage{{MediaType: "image/png", Base64: img}})),
		)
	}
	return append([]anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("look"))}, msgs...)
}

// largestImageSide is the longest side of any image in a request.
func largestImageSide(t *testing.T, params anthropic.BetaMessageNewParams) (largest, count int) {
	t.Helper()
	for _, m := range params.Messages {
		for _, b := range m.Content {
			if b.OfToolResult == nil {
				continue
			}
			for _, p := range b.OfToolResult.Content {
				if p.OfImage == nil || p.OfImage.Source.OfBase64 == nil {
					continue
				}
				count++
				raw, _ := base64.StdEncoding.DecodeString(p.OfImage.Source.OfBase64.Data)
				cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("an image no longer decodes: %v", err)
				}
				largest = max(largest, cfg.Width, cfg.Height)
			}
		}
	}
	return largest, count
}

// A session whose history held 21 screenshots over the many-image limit got a
// 400 on every request and could not continue (measured in goshapp: 21 images
// of 2634×2042 and 2748×1954). Past 20 images, every one is fitted before the
// request leaves; at 20 or fewer nothing is touched; and if the API names a
// lower limit, the history is fitted to that and the request retried, once.
func TestImagesFitTheManyImageLimit(t *testing.T) {
	tests := []struct {
		name        string
		n           int
		failFirst   string
		wantLargest int
		wantSent    int
		wantNotices int
	}{
		{name: "21 oversized images are fitted before sending", n: 21, wantLargest: 2000, wantSent: 1, wantNotices: 1},
		{name: "20 are under the threshold and untouched", n: 20, wantLargest: 2200, wantSent: 1},
		{
			name: "a lower limit named by the API is fitted to and retried",
			n:    21, failFirst: "400: messages.17.content.29.image.source.base64.data: At least one of the image dimensions exceed max allowed size for many-image requests: 1000 pixels",
			wantLargest: 1000, wantSent: 2, wantNotices: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &imageProvider{failFirst: tc.failFirst}
			var notices []string
			_, err := New(p, tool.NewRegistry()).Run(context.Background(), Options{
				Prompt:          "carry on",
				InitialMessages: historyWithScreenshots(t, tc.n, 2200, 1000),
				Permission:      permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
			}, func(e Event) {
				if e.Type == EventNotice && strings.Contains(e.Content, "image") {
					notices = append(notices, e.Content)
				}
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if len(p.sent) != tc.wantSent {
				t.Fatalf("sent %d requests, want %d", len(p.sent), tc.wantSent)
			}
			largest, count := largestImageSide(t, p.sent[len(p.sent)-1])
			if count != tc.n {
				t.Errorf("the last request carried %d images, want all %d", count, tc.n)
			}
			if largest != tc.wantLargest {
				t.Errorf("largest image side %d, want %d", largest, tc.wantLargest)
			}
			if len(notices) != tc.wantNotices {
				t.Errorf("notices = %q, want %d", notices, tc.wantNotices)
			}
		})
	}
}

// An image that cannot be decoded cannot be fitted; past the threshold it is
// replaced by a note rather than left to fail the request.
func TestUnfittableImageIsOmittedPastTheThreshold(t *testing.T) {
	msgs := historyWithScreenshots(t, 21, 10, 10)
	last := msgs[len(msgs)-1].Content[0].OfToolResult
	last.Content[1].OfImage.Source.OfBase64.Data = base64.StdEncoding.EncodeToString([]byte("RIFF....WEBP not decodable"))
	out, scaled, omitted := fitImagesForRequest(msgs, 2000)
	if scaled != 0 || omitted != 1 {
		t.Fatalf("scaled %d omitted %d, want 0 and 1", scaled, omitted)
	}
	got := out[len(out)-1].Content[0].OfToolResult.Content[1]
	if got.OfImage != nil || got.OfText == nil || !strings.Contains(got.OfText.Text, "image omitted") {
		t.Errorf("the undecodable image was not replaced by a note: %+v", got)
	}
	if msgs[len(msgs)-1].Content[0].OfToolResult.Content[1].OfImage == nil {
		t.Error("fitting mutated the caller's history")
	}
}
