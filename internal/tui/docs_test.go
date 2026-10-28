package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// docMentions finds `/name` in the docs. A slash command the docs name but the
// TUI does not have is how /cost and /permissions sat in parity.md as "done"
// long after they were removed; this keeps that class of drift from coming
// back.
var docMentions = regexp.MustCompile("`/([a-z][a-z-]*)")

// notCommands are `/word`s the docs may legitimately use: paths, and commands
// named precisely to say they are gone.
var notCommands = map[string]bool{
	"etc": true, "tmp": true, "private": true, "usr": true, "opt": true, "dev": true,
	"var": true, "home": true, "proc": true, "sys": true, "bin": true,
	"allow": true, "deny": true, "cost": true, "permissions": true, // removed; docs say so
	"word": true, // "a lone unknown /word", in the slash-routing paragraph
}

func TestDocsNameOnlyRealSlashCommands(t *testing.T) {
	root := filepath.Join("..", "..")
	files := []string{filepath.Join(root, "README.md")}
	docs, _ := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	files = append(files, docs...)
	for _, f := range files {
		if filepath.Base(f) == "roadmap.md" {
			continue // proposals name commands that do not exist yet, by design
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range docMentions.FindAllStringSubmatch(string(data), -1) {
			if name := m[1]; !notCommands[name] && !IsBuiltinCommand(name) {
				t.Errorf("%s names /%s, which is not a slash command (add it, fix the doc, or list it in notCommands)", filepath.Base(f), name)
			}
		}
	}
}
