package session

import (
	"encoding/json"
	"strings"
	"testing"
)

// A rewind cuts the resumed conversation where it cut the live one, and what
// was said after the rewind follows the cut.
func TestResumeAppliesARewind(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	cwd := t.TempDir()
	tr, err := NewTranscript(Meta{SessionID: "s1", CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	msg := func(role, text string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"role": role, "content": []map[string]string{{"type": "text", "text": text}}})
		return b
	}
	tr.Record("user", msg("user", "first"))
	tr.Record("assistant", msg("assistant", "one"))
	tr.Record("user", msg("user", "second — rewound away"))
	tr.Record("assistant", msg("assistant", "two"))
	tr.RecordRewind(2)
	tr.Record("user", msg("user", "second, rewritten"))

	rp, err := ReadForResume(Path(cwd, "s1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rp.After) != 3 {
		t.Fatalf("resumed %d entries, want 3", len(rp.After))
	}
	if got := string(rp.After[2].Message); !json.Valid(rp.After[2].Message) || !strings.Contains(got, "rewritten") {
		t.Errorf("last = %s", got)
	}
	for _, e := range rp.After {
		if strings.Contains(string(e.Message), "rewound away") {
			t.Error("a rewound message came back on resume")
		}
	}
}

// "Summarise up to here" resumes as the summary, then the kept messages and
// whatever followed, with nothing from before the point.
func TestResumeAppliesAPartialCompaction(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	cwd := t.TempDir()
	tr, err := NewTranscript(Meta{SessionID: "s1", CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	msg := func(role, text string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"role": role, "content": []map[string]string{{"type": "text", "text": text}}})
		return b
	}
	tr.Record("user", msg("user", "first — summarised away"))
	tr.Record("assistant", msg("assistant", "one — summarised away"))
	tr.Record("user", msg("user", "second"))
	tr.Record("assistant", msg("assistant", "two"))
	tr.RecordPartialCompaction("they talked about the first thing", 2)
	tr.Record("user", msg("user", "third"))

	rp, err := ReadForResume(Path(cwd, "s1"))
	if err != nil {
		t.Fatal(err)
	}
	if rp.Summary != "they talked about the first thing" {
		t.Errorf("summary = %q", rp.Summary)
	}
	if len(rp.After) != 3 {
		t.Fatalf("resumed %d entries, want 3 (two kept, one after)", len(rp.After))
	}
	for _, e := range rp.After {
		if strings.Contains(string(e.Message), "summarised away") {
			t.Error("a summarised message came back on resume")
		}
	}
	if !strings.Contains(string(rp.After[0].Message), "second") {
		t.Errorf("first kept = %s", rp.After[0].Message)
	}
}

// A rewind after a partial compaction counts the summary message, as the live
// conversation does.
func TestRewindAfterPartialCompaction(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	cwd := t.TempDir()
	tr, _ := NewTranscript(Meta{SessionID: "s1", CWD: cwd})
	msg := func(role, text string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"role": role, "content": []map[string]string{{"type": "text", "text": text}}})
		return b
	}
	tr.Record("user", msg("user", "a"))
	tr.Record("assistant", msg("assistant", "b"))
	tr.Record("user", msg("user", "c"))
	tr.Record("assistant", msg("assistant", "d"))
	tr.RecordPartialCompaction("sum", 2) // live: [summary, c, d]
	tr.RecordRewind(2)                   // live: [summary, c]
	rp, _ := ReadForResume(Path(cwd, "s1"))
	if len(rp.After) != 1 || !strings.Contains(string(rp.After[0].Message), `"c"`) {
		t.Fatalf("after = %d entries", len(rp.After))
	}
}
