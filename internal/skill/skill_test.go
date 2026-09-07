package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseFrontmatterAndBody(t *testing.T) {
	sk, err := parse([]byte("---\nname: review\ndescription: Review the diff\ntype: prompt\ntools: [Bash, Read]\n---\nReview this: $ARGUMENTS\n"), "review.md")
	if err != nil {
		t.Fatal(err)
	}
	if sk.Name != "review" || sk.Description != "Review the diff" || sk.Type != TypePrompt {
		t.Errorf("meta = %+v", sk)
	}
	if len(sk.Tools) != 2 || sk.Tools[0] != "Bash" {
		t.Errorf("tools = %v", sk.Tools)
	}
	if sk.Body != "Review this: $ARGUMENTS" {
		t.Errorf("body = %q", sk.Body)
	}
}

func TestParseDefaults(t *testing.T) {
	// No frontmatter: name from filename, type defaults to prompt.
	sk, err := parse([]byte("just a body"), "/x/quickfix.md")
	if err != nil {
		t.Fatal(err)
	}
	if sk.Name != "quickfix" || sk.Type != TypePrompt || sk.Body != "just a body" {
		t.Errorf("got %+v", sk)
	}
}

func TestParseRejectsBadType(t *testing.T) {
	if _, err := parse([]byte("---\nname: x\ntype: bogus\n---\nbody"), "x.md"); err == nil {
		t.Error("expected error for invalid type")
	}
}

func TestParseUnterminatedFrontmatter(t *testing.T) {
	if _, err := parse([]byte("---\nname: x\nno closing fence"), "x.md"); err == nil {
		t.Error("expected error for unterminated frontmatter")
	}
}

func TestRenderArguments(t *testing.T) {
	sk := Skill{Body: "Do $ARGUMENTS now"}
	if got := sk.Render("the thing"); got != "Do the thing now" {
		t.Errorf("render = %q", got)
	}
	// No placeholder + args → appended.
	sk2 := Skill{Body: "Standing instructions."}
	if got := sk2.Render("extra"); got != "Standing instructions.\n\nextra" {
		t.Errorf("append = %q", got)
	}
	// No placeholder + no args → unchanged.
	if got := sk2.Render(""); got != "Standing instructions." {
		t.Errorf("unchanged = %q", got)
	}
}

func TestLoadProjectOverlaysHome(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	write(t, filepath.Join(home, ".klaudia", "skills", "review.md"), "---\nname: review\ndescription: home version\n---\nhome body")
	write(t, filepath.Join(home, ".klaudia", "skills", "deploy.md"), "---\nname: deploy\ndescription: deploy\n---\ndeploy")
	// Project overrides review and adds plan.
	write(t, filepath.Join(cwd, ".klaudia", "skills", "review.md"), "---\nname: review\ndescription: project version\n---\nproject body")
	write(t, filepath.Join(cwd, ".klaudia", "skills", "plan.md"), "---\nname: plan\ndescription: plan\n---\nplan")
	// Malformed file is skipped (not fatal).
	write(t, filepath.Join(cwd, ".klaudia", "skills", "bad.md"), "---\ntype: nonsense\n---\nx")

	var warnings []string
	got := Load(cwd, func(s string) { warnings = append(warnings, s) })

	byName := map[string]Skill{}
	for _, sk := range got {
		byName[sk.Name] = sk
	}
	if byName["review"].Description != "project version" || byName["review"].Body != "project body" {
		t.Errorf("project should win: %+v", byName["review"])
	}
	if _, ok := byName["deploy"]; !ok {
		t.Error("home-only skill deploy missing")
	}
	if _, ok := byName["plan"]; !ok {
		t.Error("project-only skill plan missing")
	}
	if _, ok := byName["bad"]; ok {
		t.Error("malformed skill should have been skipped")
	}
	if len(warnings) == 0 {
		t.Error("expected a warning for the malformed skill")
	}
}

func TestLoadDirSkillLayout(t *testing.T) {
	dir := t.TempDir()
	skills := filepath.Join(dir, ".klaudia", "skills")
	mustMkdir(t, filepath.Join(skills, "deploy"))
	write(t, filepath.Join(skills, "deploy", "SKILL.md"), `---
description: Ship it
---
Deploy the service. $ARGUMENTS`)
	// Supporting files beside the definition are the reason this layout exists.
	write(t, filepath.Join(skills, "deploy", "checklist.md"), "not a skill")

	got := Load(dir, func(string) {})
	if len(got) != 1 {
		t.Fatalf("got %d skills, want 1: %+v", len(got), got)
	}
	// The directory names the skill: "SKILL" would be useless.
	if got[0].Name != "deploy" {
		t.Errorf("name = %q, want %q", got[0].Name, "deploy")
	}
	if got[0].Description != "Ship it" {
		t.Errorf("description = %q", got[0].Description)
	}
	if !strings.Contains(got[0].Body, "Deploy the service") {
		t.Errorf("body = %q", got[0].Body)
	}
}

func TestLoadDirWarnsOnSkillDirWithoutDefinition(t *testing.T) {
	dir := t.TempDir()
	skills := filepath.Join(dir, ".klaudia", "skills")
	mustMkdir(t, filepath.Join(skills, "halfdone"))
	write(t, filepath.Join(skills, "halfdone", "notes.md"), "just notes")

	var warnings []string
	got := Load(dir, func(m string) { warnings = append(warnings, m) })
	if len(got) != 0 {
		t.Fatalf("got %d skills, want 0", len(got))
	}
	// Silence here is what sent two sessions hunting the filesystem.
	if len(warnings) != 1 || !strings.Contains(warnings[0], "no SKILL.md") {
		t.Fatalf("warnings = %q, want one about a missing SKILL.md", warnings)
	}
}

func TestLoadDirFrontmatterNameStillWins(t *testing.T) {
	dir := t.TempDir()
	skills := filepath.Join(dir, ".klaudia", "skills")
	mustMkdir(t, filepath.Join(skills, "folder-name"))
	write(t, filepath.Join(skills, "folder-name", "SKILL.md"), `---
name: explicit
description: d
---
body`)

	got := Load(dir, func(string) {})
	if len(got) != 1 || got[0].Name != "explicit" {
		t.Fatalf("frontmatter name should win, got %+v", got)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReadsClaudeDirectories(t *testing.T) {
	dir := t.TempDir()
	// What a skills installer leaves behind for Claude Code.
	write(t, filepath.Join(dir, ".claude", "skills", "frontend-design", "SKILL.md"), `---
name: frontend-design
description: installed
---
from .claude`)
	// And Klaudia's own directory, which must win on a name collision.
	write(t, filepath.Join(dir, ".klaudia", "skills", "frontend-design.md"), `---
name: frontend-design
description: overridden
---
from .klaudia`)
	write(t, filepath.Join(dir, ".claude", "skills", "solo.md"), `---
name: solo
description: only in .claude
---
body`)

	got := Load(dir, func(string) {})
	if len(got) != 2 {
		t.Fatalf("got %d skills, want 2: %+v", len(got), got)
	}
	byName := map[string]Skill{}
	for _, sk := range got {
		byName[sk.Name] = sk
	}
	if d := byName["frontend-design"].Description; d != "overridden" {
		t.Errorf(".klaudia should win on collision, got %q", d)
	}
	if _, ok := byName["solo"]; !ok {
		t.Error("a skill only in .claude/skills should load")
	}
}
