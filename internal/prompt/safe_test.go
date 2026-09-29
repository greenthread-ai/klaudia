package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeSystemLeavesOutTheProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "CLAUDE.md"), []byte("USER RULE"), 0o644)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("PROJECT RULE"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".klaudia"), 0o755)
	os.WriteFile(filepath.Join(dir, ".klaudia", "MEMORY.md"), []byte("PROJECT MEMORY"), 0o644)

	full := System(dir, "")
	safe := SafeSystem(dir, "")
	if !strings.Contains(full, "PROJECT RULE") || !strings.Contains(full, "PROJECT MEMORY") {
		t.Fatal("the normal prompt should carry the project's instructions and memory")
	}
	if strings.Contains(safe, "PROJECT RULE") || strings.Contains(safe, "PROJECT MEMORY") {
		t.Error("safe mode loaded project content")
	}
	if !strings.Contains(safe, "USER RULE") || !strings.Contains(safe, "safe mode") {
		t.Error("safe mode should keep the user's own CLAUDE.md and say it is in safe mode")
	}
}
