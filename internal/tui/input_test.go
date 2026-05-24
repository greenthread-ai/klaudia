package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
)

func newTestModel() *Model {
	in := textinput.New()
	return &Model{input: in, sess: &Session{}, histPos: 0}
}

func TestHistoryNavigation(t *testing.T) {
	m := newTestModel()
	m.pushHistory("first")
	m.pushHistory("second")

	// Stash a fresh draft, then walk up to older entries.
	m.input.SetValue("draft")
	m.navigateHistory(true) // → "second"
	if m.input.Value() != "second" {
		t.Fatalf("up once = %q, want second", m.input.Value())
	}
	m.navigateHistory(true) // → "first"
	if m.input.Value() != "first" {
		t.Fatalf("up twice = %q, want first", m.input.Value())
	}
	m.navigateHistory(true) // clamp at oldest
	if m.input.Value() != "first" {
		t.Fatalf("up clamped = %q, want first", m.input.Value())
	}
	m.navigateHistory(false) // → "second"
	m.navigateHistory(false) // → draft restored
	if m.input.Value() != "draft" {
		t.Fatalf("down to draft = %q, want draft", m.input.Value())
	}
}

func TestPushHistoryDedupesConsecutive(t *testing.T) {
	m := newTestModel()
	m.pushHistory("a")
	m.pushHistory("a")
	m.pushHistory("b")
	if len(m.inputHistory) != 2 {
		t.Errorf("history = %v, want [a b]", m.inputHistory)
	}
}

func TestCommonPrefix(t *testing.T) {
	if got := commonPrefix([]string{"internal/api", "internal/agent"}); got != "internal/ag" && got != "internal/a" {
		// longest shared prefix of the two is "internal/a"
		if got != "internal/a" {
			t.Errorf("commonPrefix = %q", got)
		}
	}
	if got := commonPrefix([]string{"abc"}); got != "abc" {
		t.Errorf("single = %q", got)
	}
	if got := commonPrefix([]string{"abc", "xyz"}); got != "" {
		t.Errorf("disjoint = %q, want empty", got)
	}
}

func TestFmtDuration(t *testing.T) {
	cases := map[time.Duration]string{
		500 * time.Millisecond:          "500ms",
		12300 * time.Millisecond:        "12.3s",
		(2*time.Minute + 5*time.Second): "2m05s",
	}
	for d, want := range cases {
		if got := fmtDuration(d); got != want {
			t.Errorf("fmtDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestSlashSuggestions(t *testing.T) {
	m := newTestModel()
	m.sess.Skills = []SkillCommand{{Name: "review"}}

	m.input.SetValue("/co")
	got := m.slashSuggestions()
	// /compact, /config, /context, /commit all start with /co
	if len(got) < 3 {
		t.Errorf("/co suggestions = %v, want several", got)
	}
	for _, g := range got {
		if !strings.HasPrefix(g, "/co") {
			t.Errorf("suggestion %q does not match /co", g)
		}
	}

	// A skill is suggested.
	m.input.SetValue("/rev")
	if got := m.slashSuggestions(); len(got) != 1 || got[0] != "/review" {
		t.Errorf("/rev = %v, want [/review]", got)
	}

	// No suggestions once a space is typed (args started).
	m.input.SetValue("/model gpt")
	if got := m.slashSuggestions(); got != nil {
		t.Errorf("with args = %v, want nil", got)
	}
}

func TestCompleteSlashCommonPrefix(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("/co")
	m.completeSlash() // multiple matches → common prefix (at least "/co")
	if !strings.HasPrefix(m.input.Value(), "/co") {
		t.Errorf("completeSlash = %q", m.input.Value())
	}

	m.input.SetValue("/doc")
	m.completeSlash() // unique → "/doctor "
	if m.input.Value() != "/doctor " {
		t.Errorf("unique completeSlash = %q, want '/doctor '", m.input.Value())
	}
}

func TestMatchPaths(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"alpha.go", "alfred.txt", "beta.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := matchPaths(dir, "al")
	if len(got) != 2 {
		t.Fatalf("matchPaths(al) = %v, want alpha.go + alfred.txt", got)
	}
	if matchPaths(dir, "zzz") != nil {
		t.Errorf("no-match should be nil")
	}
}
