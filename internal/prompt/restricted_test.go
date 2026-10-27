package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --restricted leaves ~/.claude/CLAUDE.md out of the instructions.
func TestRestrictedSkipsClaudeMD(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "CLAUDE.md"), []byte("PERSONAL-RULE"), 0o644)
	if instr, _ := loadProjectInstructions(t.TempDir()); !strings.Contains(instr, "PERSONAL-RULE") {
		t.Fatal("precondition: ~/.claude/CLAUDE.md is read normally")
	}
	t.Setenv("KLAUDIA_RESTRICTED", "1")
	if instr, _ := loadProjectInstructions(t.TempDir()); strings.Contains(instr, "PERSONAL-RULE") {
		t.Error("a restricted run read ~/.claude/CLAUDE.md")
	}
}
