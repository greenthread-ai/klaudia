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

func TestIterations(t *testing.T) {
	tests := []struct{ in, want int }{
		{0, DefaultIterations},
		{-3, DefaultIterations},
		{5, 5},
		{MaxIterations, MaxIterations},
		{MaxIterations + 100, MaxIterations},
	}
	for _, tc := range tests {
		if got := Iterations(tc.in); got != tc.want {
			t.Errorf("Iterations(%d) = %d, want %d", tc.in, got, tc.want)
		}
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
		{"no goal-goal doubling", "# Goal\n\n## Objective\n\nGoal: auto-resume project session\n", "klaudia/goal-auto-resume-project-session"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := BranchName(tc.spec); got != tc.want {
				t.Errorf("BranchName = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMergeHint(t *testing.T) {
	withBase := MergeHint("klaudia/goal-x", "main")
	if !contains(withBase, "git diff main...klaudia/goal-x") || !contains(withBase, "git merge klaudia/goal-x") {
		t.Errorf("hint with base = %q", withBase)
	}
	// Unknown base (empty, or same as the branch) → generic guidance.
	for _, base := range []string{"", "klaudia/goal-x"} {
		g := MergeHint("klaudia/goal-x", base)
		if !contains(g, "merge into your main branch") || contains(g, "git switch") {
			t.Errorf("hint with base %q = %q", base, g)
		}
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
	w := WrapUpPrompt(p)
	if !contains(w, p) || !contains(w, "Do NOT make code changes") {
		t.Error("wrap-up prompt should reference the spec path and forbid new work")
	}
}

func TestCountUnchecked(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "PRD.md")
	tests := []struct {
		name string
		body string
		want int
	}{
		{"no checkboxes", "# Goal\n\nDo the thing.\n", 0},
		{"all checked", "- [x] one\n- [X] two\n", 0},
		{"mixed", "- [x] done\n- [ ] todo\n- [ ] another\n", 2},
		{"asterisk variant counts", "* [ ] alt list\n- [ ] dash list\n", 2},
		{"indented variants count", "  - [ ] nested item\n", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			os.WriteFile(spec, []byte(tc.body), 0o644)
			n, err := CountUnchecked(spec)
			if err != nil {
				t.Fatal(err)
			}
			if n != tc.want {
				t.Errorf("CountUnchecked = %d, want %d (body:\n%s)", n, tc.want, tc.body)
			}
		})
	}
}

func TestRequiresStubFix(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "PRD.md")

	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "no progress section → no fix needed",
			body: "# Goal\n\n## Acceptance criteria\n- [ ] one\n- [ ] two\n",
			want: nil,
		},
		{
			name: "all body phases tracked → no fix",
			body: "# Goal\n## Phase 1 — Setup\nfoo\n## Phase 2 — Build\nbar\n\n## Progress\n- [ ] Phase 1 — Setup\n- [ ] Phase 2 — Build\n",
			want: nil,
		},
		{
			name: "body has phases tracker doesn't (huedoku failure mode)",
			body: "# Goal\n## Phase 0 — Reset\n\n## Phase 1 — Start\n\n## Phase 4 — Validation\n\n### Phase 10 — Polish\nfoo\n\n## Progress\n- [x] Phase 0 — Reset\n- [ ] Phase 1 — Start\n",
			want: []string{"Phase 4 — Validation", "Phase 10 — Polish"},
		},
		{
			name: "tracked phases match regardless of title drift",
			body: "## Phase 3 — Long original title\nbody\n## Progress\n- [x] Phase 3\n",
			want: nil,
		},
		{
			name: "bold-style phase headings are recognised",
			body: "**Phase 5**: audio\n\n## Progress\n- [ ] Phase 4\n",
			want: []string{"Phase 5"},
		},
		{
			name: "alternative tracker heading (Status) is recognised",
			body: "## Phase 1\n## Phase 2\n\n## Status\n- [ ] Phase 1\n",
			want: []string{"Phase 2"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			os.WriteFile(spec, []byte(tc.body), 0o644)
			got, err := RequiresStubFix(spec)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("missing = %v, want %v\nspec:\n%s", got, tc.want, tc.body)
			}
			for i, w := range tc.want {
				if got[i] != w {
					t.Errorf("missing[%d] = %q, want %q", i, got[i], w)
				}
			}
		})
	}
}

func TestStubFixPromptListsMissing(t *testing.T) {
	p := StubFixPrompt("/p/PRD.md", []string{"Phase 5 — Audio", "Phase 7 — Settings"})
	for _, want := range []string{"/p/PRD.md", "Phase 5 — Audio", "Phase 7 — Settings", "## Progress", "Do NOT start any other work"} {
		if !contains(p, want) {
			t.Errorf("stub-fix prompt missing %q:\n%s", want, p)
		}
	}
}

func TestVerificationPromptHasReReadAndToken(t *testing.T) {
	p := VerificationPrompt("/p/PRD.md")
	for _, want := range []string{"/p/PRD.md", "Re-read", "from disk", CompleteToken, "the tracker is wrong"} {
		if !contains(p, want) {
			t.Errorf("verification prompt missing %q:\n%s", want, p)
		}
	}
}

func TestFacilitatorPromptRequiresProgressStubs(t *testing.T) {
	// New contract: the goal-setter must produce one stub per body phase up
	// front, so the mechanical CountUnchecked gate is reliable later.
	p := FacilitatorPrompt("/p/PRD.md", "")
	if !contains(p, "## Progress") || !contains(p, "premature termination") {
		t.Errorf("facilitator prompt should require ## Progress stubs:\n%s", p)
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
