package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

type failingRecorder struct{ calls int }

func (r *failingRecorder) Record(string, json.RawMessage) error {
	r.calls++
	return errors.New("no space left on device")
}

// A transcript that could not be written used to fail silently; the session
// was simply not resumable afterwards. (PR #44.)
func TestRecordFailureIsReportedOnce(t *testing.T) {
	rec := &failingRecorder{}
	var notices []string
	_, err := New(&summaryProvider{stop: "end_turn", text: "hi"}, tools.NewRegistry()).Run(context.Background(),
		Options{Prompt: "hello", Model: "claude-opus-5-5", Recorder: rec}, func(e Event) {
			if e.Type == EventNotice {
				notices = append(notices, e.Content)
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	if rec.calls < 2 {
		t.Fatalf("recorder called %d times; the test needs more than one write", rec.calls)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "no space left on device") || !strings.Contains(notices[0], "resumable") {
		t.Errorf("notices = %q, want exactly one naming the error", notices)
	}
}
