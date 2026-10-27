package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// truncatingProvider cuts its first accepted turn off mid-tool-call (stop
// reason max_tokens, input patched to "{}" the way the stream layer does) and
// ends the run on the next. With limit set it first refuses any request above
// it, so the loop's 400-lowering path runs before the truncation.
type truncatingProvider struct {
	limit    int
	cut      anthropic.BetaMessage
	answered int
}

func newTruncatingProvider(t *testing.T, limit int) *truncatingProvider {
	cut := toolTurn(t, "cut", "Write", "{}")
	cut.StopReason = "max_tokens"
	return &truncatingProvider{limit: limit, cut: cut}
}

func (p *truncatingProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	if p.limit > 0 && params.MaxTokens > int64(p.limit) {
		return anthropic.BetaMessage{}, overCapError(int(params.MaxTokens), p.limit)
	}
	p.answered++
	if p.answered == 1 {
		return p.cut, nil
	}
	return anthropic.BetaMessage{
		StopReason: "end_turn",
		Content:    []anthropic.BetaContentBlockUnion{{Type: "text", Text: "done"}},
	}, nil
}

// The truncation note used to print only "max_tokens=8192". Under the Opus 5.5
// stale-table bug that was the unknown-model floor, and nothing in the note
// said so — it read as the model's limit. The note must name the source the
// loop actually ran with, including the two the loop decides for itself (the
// default path and a cap the provider rejected), and only a cap that is ours
// rather than the model's may tell the user to raise it.
func TestTruncationNoteNamesTheLimitSource(t *testing.T) {
	tests := []struct {
		name       string
		opts       Options
		limit      int
		wantLimit  string
		wantSource string
		wantHint   bool
	}{
		{
			name:       "config override says where to raise it",
			opts:       Options{MaxTokens: 4096, MaxTokensSource: api.LimitSourceConfig},
			wantLimit:  "max_tokens=4096",
			wantSource: api.LimitSourceConfig,
			wantHint:   true,
		},
		{
			name:       "provider figure is the model's real cap",
			opts:       Options{MaxTokens: 64000, MaxTokensSource: api.LimitSourceProvider},
			wantLimit:  "max_tokens=64000",
			wantSource: api.LimitSourceProvider,
		},
		{
			name:       "unset cap on a known model reports the table",
			opts:       Options{Model: "claude-opus-5-5"},
			wantLimit:  fmt.Sprintf("max_tokens=%d", api.MaxOutputTokens("claude-opus-5-5")),
			wantSource: api.LimitSourceModel,
		},
		{
			name:       "unset cap on an unknown model reports the floor",
			opts:       Options{Model: "claude-fictional-9"},
			wantLimit:  fmt.Sprintf("max_tokens=%d", api.DefaultMaxOutputTokens),
			wantSource: api.LimitSourceFallback,
			wantHint:   true,
		},
		{
			name:       "a cap the provider rejected replaces the startup label",
			opts:       Options{MaxTokens: 128000, MaxTokensSource: api.LimitSourceModel},
			limit:      64000,
			wantLimit:  "max_tokens=64000",
			wantSource: api.LimitSourceRejected,
		},
		{
			name:      "a caller that sets no source gets the number alone",
			opts:      Options{MaxTokens: 4096},
			wantLimit: "(max_tokens=4096)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := &probeTool{name: "Write", log: &runLog{}}
			loop := New(newTruncatingProvider(t, tt.limit), tools.NewRegistry(probe))
			opts := tt.opts
			opts.Prompt = "write the file"

			var note string
			_, err := loop.Run(context.Background(), opts, func(e Event) {
				if e.Type == "tool_result" && e.ToolUseID == "cut" {
					note = e.Content
				}
			})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if note == "" {
				t.Fatal("the truncated call produced no tool_result")
			}
			if !strings.Contains(note, tt.wantLimit) {
				t.Errorf("note does not carry %q:\n%s", tt.wantLimit, note)
			}
			if tt.wantSource != "" && !strings.Contains(note, "from "+tt.wantSource) {
				t.Errorf("note does not name source %q:\n%s", tt.wantSource, note)
			}
			if got := strings.Contains(note, "config.toml"); got != tt.wantHint {
				t.Errorf("raise-it hint present = %v, want %v:\n%s", got, tt.wantHint, note)
			}
			if len(probe.log.order()) != 0 {
				t.Error("the truncated call ran")
			}
		})
	}
}
