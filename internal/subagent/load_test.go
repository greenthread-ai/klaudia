package subagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAgent(t *testing.T, dir, name, body string) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	writeAgent(t, filepath.Join(home, ".claude", "agents"), "reviewer.md",
		"---\nname: reviewer\ndescription: user-level reviewer\ntools: Read, Grep\nmodel: sonnet\n---\nReview it.")
	// The project's definition of the same name wins.
	writeAgent(t, filepath.Join(cwd, ".klaudia", "agents"), "reviewer.md",
		"---\nname: reviewer\ndescription: project reviewer\n---\nReview it the project's way.")
	// Written the way Claude Code's own plugin agents are: prose with colons
	// and unindented example lines after the description.
	writeAgent(t, filepath.Join(cwd, ".claude", "agents"), "hunter.md",
		"---\nname: silent-failure-hunter\ndescription: Use this agent when reviewing error handling. Examples:\nContext: a PR adds retries.\nuser: \"check it\"\nmodel: inherit\ncolor: yellow\n---\nFind swallowed errors.")
	writeAgent(t, filepath.Join(cwd, ".claude", "agents"), "broken.md", "no frontmatter here")

	var warns []string
	types := Load(cwd, func(m string) { warns = append(warns, m) })

	rev, ok := Find(types, "Reviewer")
	if !ok || rev.Description != "project reviewer" || len(rev.Tools) != 1 || rev.Tools[0] != "*" {
		t.Errorf("reviewer = %+v, want the project definition (all tools)", rev)
	}
	h, ok := Find(types, "silent_failure_hunter")
	if !ok || !strings.Contains(h.Description, "Context: a PR adds retries.") || h.Model != "" || h.SystemPrompt != "Find swallowed errors." {
		t.Errorf("hunter = %+v", h)
	}
	if _, ok := Find(types, "explore"); !ok {
		t.Error("built-ins should still be there, found case-insensitively")
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "broken.md") {
		t.Errorf("warns = %v, want one for broken.md", warns)
	}
}

// A user definition with a built-in's name replaces it.
func TestLoadOverridesBuiltin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	writeAgent(t, filepath.Join(cwd, ".klaudia", "agents"), "explore.md",
		"---\nname: Explore\ndescription: our explorer\ntools: [Read]\n---\nExplore carefully.")
	types := Load(cwd, nil)
	e, _ := Find(types, "Explore")
	if e.Description != "our explorer" || len(e.Tools) != 1 {
		t.Errorf("Explore = %+v, want the override", e)
	}
	n := 0
	for _, ty := range types {
		if strings.EqualFold(ty.Name, "explore") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d Explore types, want 1", n)
	}
}
