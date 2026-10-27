package cli

import (
	"encoding/json"
	"testing"

	"github.com/greenthread-ai/klaudia/pkg/agent"
)

type plainRecorder struct{}

func (plainRecorder) Record(string, json.RawMessage) error { return nil }

type markingRecorder struct{ got []string }

func (m *markingRecorder) Record(string, json.RawMessage) error { return nil }
func (m *markingRecorder) RecordCompaction(s string) error {
	m.got = append(m.got, s)
	return nil
}

// The headless path wraps the transcript in a multiRecorder. Without
// forwarding, autocompact's CompactionRecorder assertion fails on the wrapper
// and a headless session never records a boundary — so its resume drops
// everything after the summary, the bug the boundary exists to fix.
func TestMultiRecorderForwardsTheBoundary(t *testing.T) {
	tr := &markingRecorder{}
	var r agent.Recorder = multiRecorder{plainRecorder{}, nil, tr}
	cr, ok := r.(agent.CompactionRecorder)
	if !ok {
		t.Fatal("multiRecorder does not implement CompactionRecorder")
	}
	if err := cr.RecordCompaction("S"); err != nil {
		t.Fatal(err)
	}
	if len(tr.got) != 1 || tr.got[0] != "S" {
		t.Errorf("wrapped transcript got %q, want [S]", tr.got)
	}
}
