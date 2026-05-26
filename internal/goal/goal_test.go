package goal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSpecPathPrecedence(t *testing.T) {
	t.Run("none exists returns default create path", func(t *testing.T) {
		dir := t.TempDir()
		path, found := SpecPath(dir)
		if found {
			t.Fatal("found should be false when no spec exists")
		}
		if want := filepath.Join(dir, ".klaudia", "GOAL.md"); path != want {
			t.Errorf("default path = %q, want %q", path, want)
		}
	})

	t.Run("PRD.md wins over .klaudia/GOAL.md", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, ".klaudia"), 0o755)
		os.WriteFile(filepath.Join(dir, ".klaudia", "GOAL.md"), []byte("# Goal\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "PRD.md"), []byte("# PRD\n"), 0o644)

		path, found := SpecPath(dir)
		if !found || path != filepath.Join(dir, "PRD.md") {
			t.Errorf("got (%q, %v), want PRD.md found", path, found)
		}
	})

	t.Run("falls back to .klaudia/GOAL.md", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, ".klaudia"), 0o755)
		os.WriteFile(filepath.Join(dir, ".klaudia", "GOAL.md"), []byte("# Goal\n"), 0o644)

		path, found := SpecPath(dir)
		if !found || path != filepath.Join(dir, ".klaudia", "GOAL.md") {
			t.Errorf("got (%q, %v), want .klaudia/GOAL.md found", path, found)
		}
	})
}

func TestRead(t *testing.T) {
	dir := t.TempDir()
	if text, _, err := Read(dir); err != nil || text != "" {
		t.Fatalf("Read with no spec = (%q, %v), want empty", text, err)
	}
	os.WriteFile(filepath.Join(dir, "PRD.md"), []byte("hello"), 0o644)
	text, path, err := Read(dir)
	if err != nil || text != "hello" || path != filepath.Join(dir, "PRD.md") {
		t.Fatalf("Read = (%q, %q, %v)", text, path, err)
	}
}

func TestTemplateHasSections(t *testing.T) {
	out := Template("build a thing")
	for _, want := range []string{"## Objective", "build a thing", "## Acceptance criteria", "- [ ]", "## Verify"} {
		if !contains(out, want) {
			t.Errorf("template missing %q:\n%s", want, out)
		}
	}
	// Empty objective gets a placeholder, not a blank section.
	if contains(Template("  "), "\n\n\n") {
		t.Error("empty objective produced a blank Objective section")
	}
}

func TestIsComplete(t *testing.T) {
	if !IsComplete("all done\n<goal-complete/>\n") {
		t.Error("should detect the completion token")
	}
	if IsComplete("still working on it") {
		t.Error("false positive on ordinary text")
	}
}

func TestBranchName(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want string
	}{
		{"from objective text under headers", "# Goal\n\n## Objective\n\nAdd a /version subcommand\n", "klaudia/goal-add-a-version-subcommand"},
		{"skips bare Goal title", "# Goal\n", "klaudia/goal"},
		{"empty spec", "", "klaudia/goal"},
		{"slug strips punctuation", "# Fix the LSP: diagnostics!\n", "klaudia/goal-fix-the-lsp-diagnostics"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := BranchName(tc.spec); got != tc.want {
				t.Errorf("BranchName = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPromptsReferenceSpecPath(t *testing.T) {
	const p = "/repo/.klaudia/GOAL.md"
	if !contains(FacilitatorPrompt(p, ""), p) || !contains(FacilitatorPrompt(p, ""), "no spec yet") {
		t.Error("facilitator prompt should mention the path and the no-spec case")
	}
	if !contains(FacilitatorPrompt(p, "EXISTING"), "EXISTING") {
		t.Error("facilitator prompt should include existing spec contents")
	}
	it := IterationPrompt(p)
	if !contains(it, p) || !contains(it, CompleteToken) {
		t.Error("iteration prompt should reference the spec path and completion token")
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
