package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/permission"
)

// Writing a git hook, .mcp.json or .envrc makes code run later, so it is asked
// about even in the modes that auto-accept edits. Ordinary files are not.
func TestEditPathDecisionAsksForExecGrantingFiles(t *testing.T) {
	dir := t.TempDir()
	hooks := filepath.Join(dir, ".git", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "hooks")
	if err := os.Symlink(hooks, link); err != nil {
		t.Fatal(err)
	}

	for _, mode := range []permission.Mode{permission.ModeAutonomous, permission.ModeAcceptEdits} {
		pctx := permission.Context{Mode: permission.StaticMode(mode)}
		for _, p := range []string{
			filepath.Join(hooks, "pre-commit"),
			filepath.Join(link, "pre-commit"), // through a symlinked parent
			".git/config",
			".mcp.json",
			"sub/.envrc",
			".vscode/tasks.json",
			".klaudia/config.toml",
		} {
			if got := editPathDecision(pctx, p); got.Behavior != permission.Ask {
				t.Errorf("%s: %s = %q, want ask", mode, p, got.Behavior)
			}
		}
		for _, p := range []string{"main.go", filepath.Join(dir, "README.md"), "docs/git.md", ".gitignore"} {
			if got := editPathDecision(pctx, p); got.Behavior != permission.Allow {
				t.Errorf("%s: %s = %q, want allow", mode, p, got.Behavior)
			}
		}
	}
	// Plan mode still refuses outright rather than asking.
	plan := permission.Context{Mode: permission.StaticMode(permission.ModePlan)}
	if got := editPathDecision(plan, ".mcp.json"); got.Behavior != permission.Deny {
		t.Errorf("plan: .mcp.json = %q, want deny", got.Behavior)
	}
}
