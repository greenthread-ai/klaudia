package skill

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/subagent"
)

// onDisk drops the bundled skills, for tests about what the directories
// contribute.
func onDisk(skills []Skill) []Skill {
	var out []Skill
	for _, sk := range skills {
		if !sk.Bundled {
			out = append(out, sk)
		}
	}
	return out
}

// TestBundledSkillsAllParse guards the embedded files: Bundled skips a file
// that does not parse rather than warn every user about it, so a broken one
// would otherwise vanish without a trace.
func TestBundledSkillsAllParse(t *testing.T) {
	entries, err := fs.ReadDir(bundledFS, "bundled")
	if err != nil {
		t.Fatal(err)
	}
	got := Bundled()
	if len(got) != len(entries) {
		t.Fatalf("Bundled() = %d skills from %d files; one failed to parse", len(got), len(entries))
	}
	for _, sk := range got {
		if !sk.Bundled || !strings.HasPrefix(sk.Path, bundledPathPrefix) {
			t.Errorf("%s: not marked bundled (Bundled=%v Path=%q)", sk.Name, sk.Bundled, sk.Path)
		}
		// The name is what /<name> and the Skill tool use, so it must match the
		// file a maintainer edits.
		if want := strings.TrimSuffix(strings.TrimPrefix(sk.Path, bundledPathPrefix), ".md"); sk.Name != want {
			t.Errorf("skill %q lives in %q", sk.Name, sk.Path)
		}
		// The description is all the model sees until it invokes the skill.
		if sk.Description == "" {
			t.Errorf("%s: no description", sk.Name)
		}
		// Arguments are how the user says what to review or build.
		if !strings.Contains(sk.Body, "$ARGUMENTS") {
			t.Errorf("%s: body has no $ARGUMENTS", sk.Name)
		}
	}
}

func TestBundledSkillsShipped(t *testing.T) {
	byName := map[string]Skill{}
	for _, sk := range Bundled() {
		byName[sk.Name] = sk
	}
	for _, name := range []string{"code-review", "review-pr", "feature-dev"} {
		if _, ok := byName[name]; !ok {
			t.Errorf("bundled skill %q missing", name)
		}
	}
}

// The prompts send work to sub-agents by type name. A name Klaudia does not
// define — one carried over from another agent's plugin, say — would make every
// such Agent call fail validation.
func TestBundledSkillsNameRealSubagents(t *testing.T) {
	ref := regexp.MustCompile("`([A-Za-z-]+)` sub-agent|`subagent_type` set to\\s+`([A-Za-z-]+)`")
	seen := 0
	for _, sk := range Bundled() {
		for _, m := range ref.FindAllStringSubmatch(sk.Body, -1) {
			name := m[1] + m[2]
			seen++
			if _, ok := subagent.Lookup(name); !ok {
				t.Errorf("%s sends work to sub-agent type %q, which is not built in", sk.Name, name)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no sub-agent references found; the pattern no longer matches the prompts")
	}
}

func TestLoadIncludesBundledAtLowestPrecedence(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	// Nothing on disk: the bundled skills alone.
	got := Load(cwd, nil)
	if len(got) != len(Bundled()) || len(got) == 0 {
		t.Fatalf("Load with no skill dirs = %d skills, want the %d bundled", len(got), len(Bundled()))
	}

	// A project skill of the same name replaces the bundled one.
	write(t, filepath.Join(cwd, ".klaudia", "skills", "code-review.md"),
		"---\ndescription: our review\n---\nproject review")
	got = Load(cwd, nil)
	var found bool
	for _, sk := range got {
		if sk.Name != "code-review" {
			continue
		}
		found = true
		if sk.Bundled || sk.Body != "project review" {
			t.Errorf("project skill should replace the bundled one, got %+v", sk)
		}
	}
	if !found {
		t.Fatal("code-review missing after override")
	}
	if len(got) != len(Bundled()) {
		t.Errorf("override should replace, not add: %d skills", len(got))
	}
}
