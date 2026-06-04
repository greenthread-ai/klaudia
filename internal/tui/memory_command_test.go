package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/internal/memory"
)

func TestHandleMemoryNoArgsRendersIndex(t *testing.T) {
	m := newTestModel()
	dir := t.TempDir()
	m.sess.Memory = memory.New(dir)
	_ = m.sess.Memory.Add("a session bullet")

	out := m.handleMemoryCommand(nil)
	if !strings.Contains(out, "a session bullet") {
		t.Errorf("no-args should render the index; got %q", out)
	}
}

func TestHandleMemoryNoArgsEmptyShowsAuditHint(t *testing.T) {
	m := newTestModel()
	m.sess.Memory = memory.New(t.TempDir())
	out := m.handleMemoryCommand(nil)
	// The empty-state message should now mention the new recall surface
	// (recent / stale) so users discover them without /help.
	if !strings.Contains(out, "/memory") {
		t.Errorf("empty-state hint should reference /memory; got %q", out)
	}
}

func TestHandleMemoryAddRoundTrips(t *testing.T) {
	m := newTestModel()
	m.sess.Memory = memory.New(t.TempDir())

	out := m.handleMemoryCommand([]string{"add", "first note"})
	if !strings.Contains(out, "Saved to memory") {
		t.Errorf("add should confirm save; got %q", out)
	}
	idx, _ := m.sess.Memory.Index()
	if !strings.Contains(idx, "first note") {
		t.Errorf("Index after add missing content: %q", idx)
	}
}

func TestHandleMemoryAddOnDisabledStoreShowsFriendlyMessage(t *testing.T) {
	m := newTestModel() // newTestModel sets Memory = Disabled()
	out := m.handleMemoryCommand([]string{"add", "ignored"})
	if !strings.Contains(out, "not available") {
		t.Errorf("disabled-store add should say unavailable; got %q", out)
	}
}

func TestHandleMemoryRecentAndStale(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	mustWrite := func(name string, at time.Time) {
		p := filepath.Join(memDir, name)
		if err := os.WriteFile(p, []byte("# "+strings.TrimSuffix(name, ".md")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("fresh.md", now.Add(-1*time.Hour))
	mustWrite("old.md", now.Add(-45*24*time.Hour))

	m := newTestModel()
	m.sess.Memory = memory.New(dir)

	out := m.handleMemoryCommand([]string{"recent"})
	if !strings.Contains(out, "fresh") {
		t.Errorf("/memory recent should list fresh; got %q", out)
	}

	out = m.handleMemoryCommand([]string{"stale"})
	if !strings.Contains(out, "old") {
		t.Errorf("/memory stale should list old; got %q", out)
	}
	if strings.Contains(out, "fresh") {
		t.Errorf("/memory stale shouldn't list fresh; got %q", out)
	}
}

func TestHandleMemoryRecentCustomDuration(t *testing.T) {
	m := newTestModel()
	m.sess.Memory = memory.New(t.TempDir())
	// Empty store, but the duration parse should still succeed and yield
	// the empty-result message rather than a parse error.
	out := m.handleMemoryCommand([]string{"recent", "24h"})
	if !strings.Contains(out, "within") {
		t.Errorf("expected the empty-recent message; got %q", out)
	}

	out = m.handleMemoryCommand([]string{"recent", "garbage"})
	if !strings.Contains(out, "invalid duration") && !strings.Contains(out, "/memory recent:") {
		t.Errorf("expected duration parse error; got %q", out)
	}
}

func TestHandleMemoryTagPromoteSupersede(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "lesson.md"),
		[]byte("---\ntags: [decision]\n---\n\n# Use pg\n\nThe body of the lesson.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newTestModel()
	m.sess.Memory = memory.New(dir)

	// tag
	out := m.handleMemoryCommand([]string{"tag", "decision"})
	if !strings.Contains(out, "lesson") {
		t.Errorf("/memory tag decision should find lesson; got %q", out)
	}

	// promote
	out = m.handleMemoryCommand([]string{"promote", "lesson"})
	if !strings.Contains(out, "Promoted lesson") {
		t.Errorf("promote message wrong: %q", out)
	}

	// supersede needs both notes to exist
	if err := os.WriteFile(filepath.Join(memDir, "older.md"), []byte("# Older\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "newer.md"), []byte("# Newer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = m.handleMemoryCommand([]string{"supersede", "older", "newer"})
	if !strings.Contains(out, "Marked older as superseded by newer") {
		t.Errorf("supersede message wrong: %q", out)
	}
}

func TestHandleMemoryRejectsUnknownSubcommand(t *testing.T) {
	m := newTestModel()
	m.sess.Memory = memory.New(t.TempDir())
	out := m.handleMemoryCommand([]string{"frobnicate"})
	if !strings.Contains(out, "unknown subcommand") {
		t.Errorf("unknown subcommand should say so; got %q", out)
	}
}

func TestParseUserDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"7d", 7 * 24 * time.Hour},
		{"24h", 24 * time.Hour},
		{"1h30m", time.Hour + 30*time.Minute},
	}
	for _, tc := range cases {
		got, err := parseUserDuration(tc.in)
		if err != nil {
			t.Errorf("parseUserDuration(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseUserDuration(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if _, err := parseUserDuration("nope"); err == nil {
		t.Error("expected garbage to fail")
	}
}
