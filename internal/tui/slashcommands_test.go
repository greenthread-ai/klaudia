package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestIsBuiltinCommand(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{"a listed command", "doctor", true},
		{"with the leading slash", "/doctor", true},
		// /trust, /undo and fifteen others were live commands missing from the
		// hand-kept list this replaced, so a skill named after one of them was
		// shadowed without a warning.
		{"a command the old list had never learned", "trust", true},
		{"another one", "undo", true},
		{"an alias handled only by the switch", "?", true},
		{"the other alias", "exit", true},
		// /allow and /deny went with the per-command rule model. The old list
		// still reserved them, so a skill could not be called "allow".
		{"a command that was removed", "allow", false},
		{"its partner", "deny", false},
		{"an ordinary skill name", "review", false},
		{"the empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBuiltinCommand(tt.cmd); got != tt.want {
				t.Errorf("IsBuiltinCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

// TestEverySlashCaseIsClaimed is the drift guard. handleSlash is the authority
// on what a slash command does, and IsBuiltinCommand derives its answer from
// commandList; a case added to the switch without a table entry would make the
// command work while a same-named skill was shadowed in silence. Rather than
// trusting the two to stay in step, the switch is read.
func TestEverySlashCaseIsClaimed(t *testing.T) {
	cases := slashCasesFromSource(t)
	if len(cases) < 30 {
		t.Fatalf("found only %d cases in handleSlash — the parse is wrong, not the code", len(cases))
	}
	for _, c := range cases {
		if !IsBuiltinCommand(c) {
			t.Errorf("handleSlash accepts %s but IsBuiltinCommand does not: add it to commandList (or to commandAliases if it is a second spelling)", c)
		}
	}
}

// slashCasesFromSource returns the case literals of handleSlash's switch.
func slashCasesFromSource(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "commands.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing commands.go: %v", err)
	}
	var fn *ast.FuncDecl
	for _, d := range file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Name.Name == "handleSlash" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("no handleSlash in commands.go")
	}
	var out []string
	ast.Inspect(fn, func(n ast.Node) bool {
		cc, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, e := range cc.List {
			lit, ok := e.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			s, err := strconv.Unquote(lit.Value)
			// Only the command switch is of interest; handleSlash contains no
			// other string switch today, and a non-"/…" literal is skipped
			// rather than failing, so one added later does not break this.
			if err != nil || !strings.HasPrefix(s, "/") {
				continue
			}
			out = append(out, s)
		}
		return true
	})
	return out
}
