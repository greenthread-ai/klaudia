package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUserSkipsProjectSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".klaudia", "skills"), 0o755)
	os.WriteFile(filepath.Join(home, ".klaudia", "skills", "mine.md"), []byte("---\ndescription: mine\n---\nx"), 0o644)
	cwd := t.TempDir()
	os.MkdirAll(filepath.Join(cwd, ".klaudia", "skills"), 0o755)
	os.WriteFile(filepath.Join(cwd, ".klaudia", "skills", "theirs.md"), []byte("---\ndescription: theirs\n---\nx"), 0o644)

	if got := len(Load(cwd, nil)); got != 2 {
		t.Fatalf("Load = %d skills, want 2", got)
	}
	got := LoadUser(nil)
	if len(got) != 1 || got[0].Name != "mine" {
		t.Errorf("LoadUser = %+v, want only the user's skill", got)
	}
}
