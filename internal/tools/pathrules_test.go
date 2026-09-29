package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/permission"
)

// pathTool is a tool that builds a permission request from its input.
type pathTool interface {
	permission.IntrinsicChecker
	PermissionRequest(json.RawMessage) permission.PermissionRequest
}

func checkPath(t *testing.T, tool pathTool, allow, deny []string, input map[string]string) permission.Behavior {
	t.Helper()
	a, err := permission.ParseRules(allow)
	if err != nil {
		t.Fatal(err)
	}
	d, err := permission.ParseRules(deny)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(input)
	pctx := permission.Context{Mode: permission.StaticMode(permission.ModeDefault), Allow: a, Deny: d}
	return permission.Check(pctx, tool, tool.PermissionRequest(raw)).Behavior
}

func mustTool[T any](t *testing.T, mk func() (T, error)) T {
	t.Helper()
	v, err := mk()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// File rules used to match nothing useful: Read, Glob and Grep sent no
// specifier, so a Read deny could never apply, and Edit/Write compared the
// path as written against the rule as a string, so `~`, `**` and relative
// patterns did not work.
func TestPathRules(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ssh := filepath.Join(home, ".ssh")
	os.MkdirAll(ssh, 0o700)
	os.WriteFile(filepath.Join(ssh, "id_rsa"), []byte("key"), 0o600)
	proj := t.TempDir()
	t.Chdir(proj)
	os.MkdirAll(filepath.Join(proj, "src", "pkg"), 0o755)
	os.Symlink(ssh, filepath.Join(proj, "keys"))

	read := mustTool(t, NewRead)
	glob := mustTool(t, NewGlob)
	grep := mustTool(t, NewGrep)
	edit := mustTool(t, NewEdit)
	write := mustTool(t, NewWrite)
	denySSH := []string{"Read(~/.ssh/**)"}

	for _, tc := range []struct {
		name  string
		tool  pathTool
		input map[string]string
	}{
		{"read the key", read, map[string]string{"file_path": filepath.Join(ssh, "id_rsa")}},
		{"read through a symlink", read, map[string]string{"file_path": "keys/id_rsa"}},
		{"grep the directory", grep, map[string]string{"pattern": "BEGIN", "path": ssh}},
		{"glob an absolute pattern", glob, map[string]string{"pattern": ssh + "/*"}},
	} {
		if got := checkPath(t, tc.tool, nil, denySSH, tc.input); got != permission.Deny {
			t.Errorf("%s: %q, want deny", tc.name, got)
		}
	}
	if got := checkPath(t, read, nil, denySSH, map[string]string{"file_path": "main.go"}); got != permission.Allow {
		t.Errorf("read a project file: %q, want allow", got)
	}

	// A relative pattern is relative to the project; ** crosses directories;
	// an Edit deny covers Write too.
	denySrc := []string{"Edit(src/**)"}
	if got := checkPath(t, write, nil, denySrc, map[string]string{"file_path": "src/pkg/a.go", "content": ""}); got != permission.Deny {
		t.Errorf("write src/pkg/a.go under Edit(src/**) deny: %q, want deny", got)
	}
	if got := checkPath(t, edit, nil, denySrc, map[string]string{"file_path": filepath.Join(proj, "docs", "x.md")}); got != permission.Ask {
		t.Errorf("edit docs/x.md: %q, want ask", got)
	}

	// Allow rules match the same way, but are not widened to other tools.
	allowSrc := []string{"Edit(src/**)"}
	if got := checkPath(t, edit, allowSrc, nil, map[string]string{"file_path": filepath.Join(proj, "src", "pkg", "a.go")}); got != permission.Allow {
		t.Errorf("edit under Edit(src/**) allow: %q, want allow", got)
	}
	if got := checkPath(t, write, allowSrc, nil, map[string]string{"file_path": "src/pkg/a.go"}); got != permission.Ask {
		t.Errorf("write under an Edit allow: %q, want ask (allow rules are not widened)", got)
	}
	// Rules written for the old string matching keep working.
	if got := checkPath(t, edit, []string{"Edit(src/*)"}, nil, map[string]string{"file_path": "src/pkg/a.go"}); got != permission.Allow {
		t.Errorf("legacy prefix rule: %q, want allow", got)
	}
}
