package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The workspace shape this was written for: a CLAUDE.md that is only
// "@AGENTS.md", above the checkout, with shared rules beside it.
func TestInstructionsWorkspaceShape(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ws := t.TempDir()
	write(t, filepath.Join(ws, "CLAUDE.md"), "# Workspace\n\n@AGENTS.md\n")
	write(t, filepath.Join(ws, "AGENTS.md"), "Record work in Vogt.")
	write(t, filepath.Join(ws, ".claude", "rules", "b.md"), "Rule B.")
	write(t, filepath.Join(ws, ".claude", "rules", "a.md"), "Rule A.")
	repo := filepath.Join(ws, "Active", "repo")
	write(t, filepath.Join(repo, "CLAUDE.md"), "Run make check.")

	got := loadProjectInstructions(repo)
	if strings.Contains(got, "@AGENTS.md") {
		t.Error("the import line reached the model as written")
	}
	order := []string{"Record work in Vogt.", "Rule A.", "Rule B.", "Run make check."}
	last := -1
	for _, want := range order {
		i := strings.Index(got, want)
		if i < 0 {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
		if i < last {
			t.Errorf("%q out of order (workspace, then its rules sorted, then the repo):\n%s", want, got)
		}
		last = i
	}
	// AGENTS.md was imported by the CLAUDE.md beside it; it is not added again.
	if strings.Count(got, "Record work in Vogt.") != 1 {
		t.Error("AGENTS.md included twice")
	}
}

// AGENTS.md stands in for a missing CLAUDE.md, and only then.
func TestInstructionsAgentsFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	write(t, filepath.Join(dir, "AGENTS.md"), "From AGENTS.")
	if got := loadProjectInstructions(dir); !strings.Contains(got, "From AGENTS.") {
		t.Errorf("AGENTS.md not used without a CLAUDE.md:\n%s", got)
	}
	write(t, filepath.Join(dir, "CLAUDE.md"), "From CLAUDE.")
	got := loadProjectInstructions(dir)
	if !strings.Contains(got, "From CLAUDE.") || strings.Contains(got, "From AGENTS.") {
		t.Errorf("with both, want CLAUDE.md only:\n%s", got)
	}
}

func TestInstructionsImports(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	write(t, filepath.Join(dir, "CLAUDE.md"), strings.Join([]string{
		"<!-- maintainer note: not for the model -->",
		"See @docs/style.md for style. Mail me@example.com.",
		"@a.md",
		"```",
		"@docs/style.md",
		"```",
	}, "\n"))
	write(t, filepath.Join(dir, "docs", "style.md"), "Style: tabs.")
	write(t, filepath.Join(dir, "a.md"), "A.\n@b.md")
	write(t, filepath.Join(dir, "b.md"), "B.\n@a.md") // a cycle

	got := loadProjectInstructions(dir)
	for _, want := range []string{"See @docs/style.md for style.", "Style: tabs.", "A.", "B.", "me@example.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "maintainer note") {
		t.Error("HTML comment reached the model")
	}
	if strings.Count(got, "Style: tabs.") != 1 || strings.Count(got, "A.") != 1 {
		t.Errorf("an import was included more than once:\n%s", got)
	}
	// Inside a code fence, @ is code.
	if !strings.Contains(got, "```\n@docs/style.md\n```") {
		t.Errorf("fenced @reference was expanded:\n%s", got)
	}
}
