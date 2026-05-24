package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemIncludesEnvAndSecurity(t *testing.T) {
	dir := t.TempDir()
	p := System(dir, "claude-haiku-4-5")

	for _, want := range []string{
		"You are Klaudia",
		"authorized security testing", // security clause
		"<env>",
		"Working directory: " + dir,
		"Today's date:",
		"claude-haiku-4-5",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}

func TestSystemLoadsProjectClaudeMd(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate from a real ~/.claude/CLAUDE.md
	dir := t.TempDir()
	body := "# Klaudia repo\nAlways run gofmt before committing."
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	p := System(dir, "")
	if !strings.Contains(p, "Project instructions (from CLAUDE.md)") {
		t.Error("expected CLAUDE.md section header")
	}
	if !strings.Contains(p, "Always run gofmt before committing.") {
		t.Error("expected CLAUDE.md content to be included")
	}
}

func TestSystemNoClaudeMd(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate from a real ~/.claude/CLAUDE.md
	p := System(t.TempDir(), "")
	if strings.Contains(p, "Project instructions (from CLAUDE.md)") {
		t.Error("should not emit CLAUDE.md section when none exists")
	}
}
