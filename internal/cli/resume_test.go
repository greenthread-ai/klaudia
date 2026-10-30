package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/session"
)

// --resume seeded from the summary file alone and dropped every entry after
// the compaction. On the restart that found this, 572 entries — 80 minutes of
// work — were gone, and the resumed session could not remember doing any of
// it. Resume must be the summary plus everything after it.
func TestResumeKeepsWhatCameAfterTheSummary(t *testing.T) {
	type step struct {
		say     string // a user line, or an assistant line if prefixed "a:"
		compact string // a compaction boundary with this summary instead
		legacy  string // a summary *file* only (pre-boundary binary)
	}
	tests := []struct {
		name  string
		steps []step
		ok    bool
		// want is the text of each resumed message in order; the first is
		// matched as "contains", since the seed wraps the summary.
		want []string
	}{
		{
			name: "everything after a boundary survives",
			steps: []step{
				{say: "before"}, {say: "a:old answer"},
				{compact: "SUMMARY-1"},
				{say: "a:after one"}, {say: "after two"},
			},
			ok:   true,
			want: []string{"SUMMARY-1", "after one", "after two"},
		},
		{
			name: "the last boundary wins",
			steps: []step{
				{say: "x"}, {compact: "SUMMARY-1"}, {say: "a:middle"},
				{compact: "SUMMARY-2"}, {say: "a:latest"},
			},
			ok:   true,
			want: []string{"SUMMARY-2", "latest"},
		},
		{
			name: "a legacy transcript is cut at the summary file's stamp",
			steps: []step{
				{say: "before"}, {say: "a:old answer"},
				{legacy: "LEGACY"},
				{say: "a:after"}, {say: "later"},
			},
			ok:   true,
			want: []string{"LEGACY", "after", "later"},
		},
		{
			name:  "a session never compacted is a full replay",
			steps: []step{{say: "hi"}, {say: "a:hello"}},
			ok:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
			cwd := t.TempDir()
			const id = "11111111-2222-3333-4444-555555555555"
			tr, err := session.NewTranscript(session.Meta{SessionID: id, CWD: cwd, Version: "test"})
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range tc.steps {
				switch {
				case s.compact != "":
					if err := tr.RecordCompaction(s.compact); err != nil {
						t.Fatal(err)
					}
				case s.legacy != "":
					if err := session.WriteSummary(cwd, id, s.legacy, ""); err != nil {
						t.Fatal(err)
					}
				default:
					role, text := "user", s.say
					if strings.HasPrefix(text, "a:") {
						role, text = "assistant", strings.TrimPrefix(text, "a:")
					}
					msg, _ := json.Marshal(map[string]any{"role": role, "content": []map[string]any{{"type": "text", "text": text}}})
					if err := tr.Record(role, msg); err != nil {
						t.Fatal(err)
					}
				}
				// Timestamps are millisecond-resolution; the legacy cut is
				// strictly-after, so neighbours must not share one.
				time.Sleep(2 * time.Millisecond)
			}
			_ = tr.Close()

			msgs, ok, err := resumeFromSummary(cwd, id)
			if err != nil {
				t.Fatal(err)
			}
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			got := texts(msgs)
			if len(got) != len(tc.want) {
				t.Fatalf("resumed %d messages %q, want %d %q", len(got), got, len(tc.want), tc.want)
			}
			if !strings.Contains(got[0], tc.want[0]) || !strings.Contains(got[0], "[End of summary") {
				t.Errorf("seed = %q, want the summary %q with its closing line", got[0], tc.want[0])
			}
			for i := 1; i < len(got); i++ {
				if got[i] != tc.want[i] {
					t.Errorf("message %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// Every existing reader goes through session.Read, so a boundary line must be
// invisible to it — or --resume --full, ACP's session/load and the session
// list would all choke on a message-less entry.
func TestBoundaryIsInvisibleToTheFullReader(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	cwd := t.TempDir()
	const id = "11111111-2222-3333-4444-666666666666"
	tr, err := session.NewTranscript(session.Meta{SessionID: id, CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	_ = tr.Record("user", json.RawMessage(`{"role":"user","content":"a"}`))
	_ = tr.RecordCompaction("S")
	_ = tr.Record("user", json.RawMessage(`{"role":"user","content":"b"}`))
	_ = tr.Close()

	raw, _ := os.ReadFile(tr.Path())
	if !strings.Contains(string(raw), `"subtype":"compact_boundary"`) {
		t.Fatalf("no boundary line written:\n%s", raw)
	}
	entries, err := session.Read(tr.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("Read returned %d entries, want the 2 messages and no boundary", len(entries))
	}
}

// A thinking-drop line is diagnostic only. Neither the full reader nor resume
// may see it — and in particular it must not end a resume's "after the
// boundary" run, or a drop recorded after a compaction would cut it short.
func TestThinkingDropLineIsInvisibleToReaders(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	cwd := t.TempDir()
	const id = "11111111-2222-3333-4444-777777777777"
	tr, err := session.NewTranscript(session.Meta{SessionID: id, CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	_ = tr.Record("user", json.RawMessage(`{"role":"user","content":"a"}`))
	_ = tr.RecordCompaction("S")
	_ = tr.Record("user", json.RawMessage(`{"role":"user","content":"b"}`))
	_ = tr.RecordThinkingDropped("3 earlier thinking block(s) dropped")
	_ = tr.Record("user", json.RawMessage(`{"role":"user","content":"c"}`))
	_ = tr.Close()

	raw, _ := os.ReadFile(tr.Path())
	if !strings.Contains(string(raw), `"subtype":"thinking_dropped"`) {
		t.Fatalf("no drop line written:\n%s", raw)
	}
	entries, err := session.Read(tr.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("Read returned %d entries, want the 3 messages", len(entries))
	}
	msgs, ok, err := resumeFromSummary(cwd, id)
	if err != nil || !ok {
		t.Fatalf("resume: ok=%v err=%v", ok, err)
	}
	if got := texts(msgs); len(got) != 3 || got[1] != "b" || got[2] != "c" {
		t.Errorf("resumed %q, want the summary then b, c", got)
	}
}

func texts(msgs []anthropic.BetaMessageParam) []string {
	var out []string
	for _, m := range msgs {
		var b strings.Builder
		for _, c := range m.Content {
			if c.OfText != nil {
				b.WriteString(c.OfText.Text)
			}
		}
		out = append(out, b.String())
	}
	return out
}
