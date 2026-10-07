package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// cutOffProvider streams some text, then fails with err, returning what it had
// accumulated — text plus an unfinished tool_use — as a provider does when the
// stream breaks after output.
type cutOffProvider struct {
	text string
	err  error
}

func (p cutOffProvider) StreamTurn(_ context.Context, _ anthropic.BetaMessageNewParams, sink api.StreamSink) (anthropic.BetaMessage, error) {
	if sink.OnText != nil && p.text != "" {
		sink.OnText(p.text)
	}
	var content []string
	if p.text != "" {
		content = append(content, fmt.Sprintf(`{"type":"text","text":%q}`, p.text))
	}
	content = append(content, `{"type":"thinking","thinking":"hmm","signature":""}`,
		`{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}`)
	var m anthropic.BetaMessage
	raw := `{"role":"assistant","content":[` + strings.Join(content, ",") + `]}`
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		panic(err)
	}
	return m, p.err
}

// rawRecorder keeps each recorded message as marshalled.
type rawRecorder struct{ rows []string }

func (r *rawRecorder) Record(role string, msg json.RawMessage) error {
	r.rows = append(r.rows, role+" "+string(msg))
	return nil
}

func TestCutOffReplyKeepsPartialText(t *testing.T) {
	stall := fmt.Errorf("%w: no data for 5m", api.ErrStreamStalled)
	cases := []struct {
		name   string
		err    error
		marker string
	}{
		{"stall", stall, interruptedStreamMarker},
		{"user interrupt", context.Canceled, interruptedByUserMarker},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			read, _ := tools.NewRead()
			loop := New(cutOffProvider{text: "The fix is to ", err: tc.err}, tools.NewRegistry(read))
			rec := &rawRecorder{}
			res, err := loop.Run(context.Background(), Options{Prompt: "fix it", Model: "test", Recorder: rec}, nil)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if len(res.Messages) != 2 {
				t.Fatalf("history has %d messages, want the prompt and the partial reply", len(res.Messages))
			}
			last := res.Messages[1]
			if last.Role != anthropic.BetaMessageParamRoleAssistant {
				t.Fatalf("last role = %q, want assistant", last.Role)
			}
			var texts []string
			for _, b := range last.Content {
				if b.OfText == nil {
					t.Fatalf("only text may survive a cut-off reply; got %+v", b)
				}
				texts = append(texts, b.OfText.Text)
			}
			want := []string{"The fix is to ", tc.marker}
			if strings.Join(texts, "|") != strings.Join(want, "|") {
				t.Fatalf("partial reply = %q, want %q", texts, want)
			}
			// The transcript gets it too, so a resumed session matches.
			if len(rec.rows) != 2 || !strings.HasPrefix(rec.rows[1], "assistant ") ||
				!strings.Contains(rec.rows[1], "The fix is to ") || strings.Contains(rec.rows[1], "tool_use") {
				t.Fatalf("transcript rows = %q", rec.rows)
			}
		})
	}
}

// A stream that failed before showing any text leaves the history as it was.
func TestFailureBeforeOutputAddsNothing(t *testing.T) {
	read, _ := tools.NewRead()
	loop := New(cutOffProvider{err: errors.New("boom")}, tools.NewRegistry(read))
	res, err := loop.Run(context.Background(), Options{Prompt: "fix it", Model: "test"}, nil)
	if err == nil {
		t.Fatal("want the stream error")
	}
	if len(res.Messages) != 1 {
		t.Fatalf("history has %d messages, want only the prompt", len(res.Messages))
	}
}
