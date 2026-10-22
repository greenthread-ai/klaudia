package session

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// newTestTranscript returns a transcript writing under a temp config dir.
func newTestTranscript(t *testing.T) *Transcript {
	t.Helper()
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	tr, err := NewTranscript(Meta{SessionID: "sess-sizing", CWD: "/work/proj", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr
}

// rawLines returns the transcript file's lines as decoded key sets plus the
// raw text, so a test can assert on key *absence* as well as on values.
func rawLines(t *testing.T, path string) []map[string]json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]json.RawMessage
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		m := map[string]json.RawMessage{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// TestTranscriptSizingIsAssistantOnly pins the divergence documented on Entry:
// the sizing fields describe what produced the content, so they belong on
// assistant lines and must not be repeated on user lines.
func TestTranscriptSizingIsAssistantOnly(t *testing.T) {
	sizingKeys := []string{"model", "maxTokens", "maxTokensSource", "contextWindow", "contextWindowSource"}

	tests := []struct {
		name     string
		role     string
		wantKeys bool
	}{
		{name: "assistant entry carries the resolved sizing", role: "assistant", wantKeys: true},
		{name: "user entry omits the resolved sizing", role: "user", wantKeys: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTestTranscript(t)
			tr.SetSizing("claude-opus-5-5-20260401", 128000, "provider", 200000, "provider")
			if err := tr.Record(tc.role, json.RawMessage(`{"role":"`+tc.role+`","content":"x"}`)); err != nil {
				t.Fatal(err)
			}
			lines := rawLines(t, tr.Path())
			if len(lines) != 1 {
				t.Fatalf("got %d lines, want 1", len(lines))
			}
			for _, k := range sizingKeys {
				_, ok := lines[0][k]
				if ok != tc.wantKeys {
					t.Errorf("key %q present = %v, want %v", k, ok, tc.wantKeys)
				}
			}
			if !tc.wantKeys {
				return
			}
			entries, err := Read(tr.Path())
			if err != nil {
				t.Fatal(err)
			}
			got := entries[0]
			want := Entry{
				Model: "claude-opus-5-5-20260401", MaxTokens: 128000, MaxTokensSource: "provider",
				ContextWindow: 200000, ContextWindowSource: "provider",
			}
			if got.Model != want.Model || got.MaxTokens != want.MaxTokens ||
				got.MaxTokensSource != want.MaxTokensSource ||
				got.ContextWindow != want.ContextWindow ||
				got.ContextWindowSource != want.ContextWindowSource {
				t.Errorf("sizing = %+v, want %+v", got, want)
			}
		})
	}
}

// TestTranscriptWithoutSizingIsUnchanged is the compatibility guarantee: a
// transcript whose sizing was never set writes the same bytes it wrote before
// these fields existed, so older sessions and --resume are unaffected.
func TestTranscriptWithoutSizingIsUnchanged(t *testing.T) {
	tr := newTestTranscript(t)
	if err := tr.Record("assistant", json.RawMessage(`{"role":"assistant","content":"x"}`)); err != nil {
		t.Fatal(err)
	}
	for k := range rawLines(t, tr.Path())[0] {
		switch k {
		case "model", "maxTokens", "maxTokensSource", "contextWindow", "contextWindowSource":
			t.Errorf("unset sizing still emitted key %q", k)
		}
	}
}

// TestTranscriptSizingTracksModelSwitch covers why the fields are per-entry
// rather than a session header: /model changes the answer mid-session, and only
// the entries after the switch may carry the new figures.
func TestTranscriptSizingTracksModelSwitch(t *testing.T) {
	tr := newTestTranscript(t)
	tr.SetSizing("claude-sonnet-4-5-20250929", 64000, "model default", 200000, "model default")
	if err := tr.Record("assistant", json.RawMessage(`{"role":"assistant","content":"first"}`)); err != nil {
		t.Fatal(err)
	}
	tr.SetSizing("claude-opus-5-5-20260401", 128000, "provider", 200000, "provider")
	if err := tr.Record("assistant", json.RawMessage(`{"role":"assistant","content":"second"}`)); err != nil {
		t.Fatal(err)
	}

	entries, err := Read(tr.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Model != "claude-sonnet-4-5-20250929" || entries[0].MaxTokens != 64000 {
		t.Errorf("first entry = %q/%d, want the pre-switch model", entries[0].Model, entries[0].MaxTokens)
	}
	if entries[1].Model != "claude-opus-5-5-20260401" || entries[1].MaxTokens != 128000 {
		t.Errorf("second entry = %q/%d, want the post-switch model", entries[1].Model, entries[1].MaxTokens)
	}
	if entries[0].MaxTokensSource == entries[1].MaxTokensSource {
		t.Errorf("both entries report source %q; the switch changed it", entries[0].MaxTokensSource)
	}
}
