package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain gives every test in the package an empty home directory. Load reads
// ~/.claude/skills and ~/.klaudia/skills, so a test that forgot to isolate HOME
// saw whatever the developer running it had installed — and failed, or passed,
// for reasons in their home directory rather than in the code (#110). Doing it
// here covers tests not yet written; a test that needs a populated home still
// sets its own with t.Setenv.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "skill-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func TestLoadSkipsClaudeSyncedStoreQuietly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	// The layout Claude Code writes when it syncs skills from claude.ai.
	synced := filepath.Join(home, ".claude", "skills", "synced", "bucket-id")
	write(t, filepath.Join(synced, "manifest.json"), `{"skills":[]}`)
	write(t, filepath.Join(synced, "pdf", "SKILL.md"), "---\nname: pdf\ndescription: synced\n---\nbody")
	// An ordinary installed skill beside it still loads.
	write(t, filepath.Join(home, ".claude", "skills", "review", "SKILL.md"), "---\ndescription: installed\n---\nbody")

	var warnings []string
	got := Load(t.TempDir(), func(m string) { warnings = append(warnings, m) })
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none for the synced store", warnings)
	}
	if len(got) != 1 || got[0].Name != "review" {
		t.Errorf("got %+v, want only the installed skill (synced skills are not loaded)", got)
	}
}

func TestLoadSyncedElsewhereStillWarns(t *testing.T) {
	cwd := t.TempDir()
	// Only ~/.claude/skills/synced is Claude Code's; a project directory of
	// that name is a skill missing its SKILL.md like any other.
	write(t, filepath.Join(cwd, ".claude", "skills", "synced", "notes.md"), "notes")

	var warnings []string
	Load(cwd, func(m string) { warnings = append(warnings, m) })
	if len(warnings) != 1 || !strings.Contains(warnings[0], "no SKILL.md") {
		t.Fatalf("warnings = %q, want one about a missing SKILL.md", warnings)
	}
}

func TestLoadSkillNamedSyncedStillLoads(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// A real skill that happens to be called "synced" is not the store.
	write(t, filepath.Join(home, ".claude", "skills", "synced", "SKILL.md"), "---\ndescription: d\n---\nbody")

	got := Load(t.TempDir(), nil)
	if len(got) != 1 || got[0].Name != "synced" {
		t.Fatalf("got %+v, want the skill named synced", got)
	}
}
