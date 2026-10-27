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
