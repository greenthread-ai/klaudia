package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

type failingRecorder struct{ calls int }

func (r *failingRecorder) Record(string, json.RawMessage) error {
	r.calls++
	return errors.New("no space left on device")
}

type okProvider struct{}

func (okProvider) StreamTurn(context.Context, anthropic.BetaMessageNewParams, api.StreamSink) (anthropic.BetaMessage, error) {
	var m anthropic.BetaMessage
	_ = json.Unmarshal([]byte(`{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"hi"}]}`), &m)
	return m, nil
}

// A transcript that could not be written used to fail silently; the session
// was simply not resumable afterwards.
func TestRecordFailureIsReportedOnce(t *testing.T) {
	rec := &failingRecorder{}
	var warnings []string
	_, err := New(okProvider{}, tools.NewRegistry()).Run(context.Background(), Options{Prompt: "hello", Recorder: rec}, func(e Event) {
		if e.Type == "warning" {
			warnings = append(warnings, e.Content)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.calls < 2 {
		t.Fatalf("recorder called %d times; the test needs more than one write", rec.calls)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "no space left on device") || !strings.Contains(warnings[0], "resumable") {
		t.Errorf("warnings = %q, want exactly one naming the error", warnings)
	}
}
