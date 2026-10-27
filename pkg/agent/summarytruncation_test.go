package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/compaction"
)

// summaryProvider answers a summary request with fixed text and stop reason,
// recording the max_tokens it was asked for.
type summaryProvider struct {
	stop      anthropic.BetaStopReason
	text      string
	maxTokens []int64
}

func (p *summaryProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	p.maxTokens = append(p.maxTokens, params.MaxTokens)
	// Via JSON, as the stream accumulator builds it: AsText reads the raw
	// fields a struct literal leaves empty.
	var m anthropic.BetaMessage
	raw := fmt.Sprintf(`{"role":"assistant","stop_reason":%q,"content":[{"type":"text","text":%q}]}`, p.stop, p.text)
	return m, json.Unmarshal([]byte(raw), &m)
}

func summaryHistory() []anthropic.BetaMessageParam {
	return []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("where should plans live?")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock("asking")}},
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("~/.klaudia/plans/")),
	}
}

// Reported failure: both summary paths sent max_tokens=4096 and ignored the
// stop reason, so a summary that ran out mid-word ("**Goal m") replaced the
// whole history — including the user's answers, which the model then asked
// for again.
func TestSummaryIsSizedFromTheSessionAndRefusedWhenCutOff(t *testing.T) {
	const sessionCap = 128000
	tests := []struct {
		name        string
		stop        anthropic.BetaStopReason
		wantReplace bool
	}{
		{name: "a complete summary replaces the history", stop: "end_turn", wantReplace: true},
		{name: "a summary cut off at the cap is refused", stop: "max_tokens", wantReplace: false},
	}
	for _, tc := range tests {
		t.Run("/compact: "+tc.name, func(t *testing.T) {
			p := &summaryProvider{stop: tc.stop, text: "**Goal m"}
			_, summary, err := New(p, tools.NewRegistry()).Compact(context.Background(), summaryHistory(), "claude-opus-5-5", sessionCap, api.EffortUnset)
			if got := p.maxTokens[0]; got != sessionCap {
				t.Errorf("summary requested max_tokens=%d, want the session's %d", got, sessionCap)
			}
			if tc.wantReplace {
				if err != nil || summary == "" {
					t.Fatalf("Compact = %q, %v; want the summary", summary, err)
				}
				return
			}
			if !errors.Is(err, errSummaryTruncated) {
				t.Fatalf("Compact err = %v, want errSummaryTruncated — a partial summary was accepted", err)
			}
		})

		t.Run("autocompact: "+tc.name, func(t *testing.T) {
			p := &summaryProvider{stop: tc.stop, text: "**Goal m"}
			in := summaryHistory()
			var notices []string
			var calib compaction.Calibration
			out := New(p, tools.NewRegistry()).compact(context.Background(), in,
				Options{Model: "claude-opus-5-5", ContextWindow: 1_000_000}, sessionCap,
				func(e Event) {
					if e.Type == "notice" {
						notices = append(notices, e.Content)
					}
				}, &calib, true)
			if got := p.maxTokens[0]; got != sessionCap {
				t.Errorf("summary requested max_tokens=%d, want the session's %d", got, sessionCap)
			}
			if replaced := len(out) != len(in); replaced != tc.wantReplace {
				t.Fatalf("history replaced = %v, want %v (%d -> %d messages)", replaced, tc.wantReplace, len(in), len(out))
			}
			if !tc.wantReplace && (len(notices) != 1 || !strings.Contains(notices[0], "cut off")) {
				t.Errorf("notices = %q, want one saying the summary was cut off", notices)
			}
		})
	}
}

// boundaryRecorder is a Recorder that also takes compaction boundaries.
type boundaryRecorder struct{ boundaries []string }

func (r *boundaryRecorder) Record(string, json.RawMessage) error { return nil }
func (r *boundaryRecorder) RecordCompaction(s string) error {
	r.boundaries = append(r.boundaries, s)
	return nil
}

// --resume rebuilds from the last boundary in the transcript, so autocompact
// must write one through the turn's own recorder — the per-process OnSummary
// would put an ACP session's boundary in the wrong transcript. A refused
// summary replaced nothing, so it must not leave a boundary either: resume
// would then drop history the live session still had.
func TestAutocompactMarksTheBoundaryInTheTurnsTranscript(t *testing.T) {
	tests := []struct {
		name string
		stop anthropic.BetaStopReason
		want []string
	}{
		{name: "a complete summary leaves a boundary", stop: "end_turn", want: []string{"the summary"}},
		{name: "a refused summary leaves none", stop: "max_tokens"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &boundaryRecorder{}
			var calib compaction.Calibration
			New(&summaryProvider{stop: tc.stop, text: "the summary"}, tools.NewRegistry()).compact(
				context.Background(), summaryHistory(),
				Options{Model: "claude-opus-5-5", ContextWindow: 1_000_000, Recorder: rec},
				128000, nil, &calib, true)
			if fmt.Sprint(rec.boundaries) != fmt.Sprint(tc.want) {
				t.Errorf("boundaries = %q, want %q", rec.boundaries, tc.want)
			}
		})
	}
}
