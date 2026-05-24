package skill

import (
	"os"
	"path/filepath"
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

	write(t, filepath.Join(home, ".claude", "skills", "review.md"), "---\nname: review\ndescription: home version\n---\nhome body")
	write(t, filepath.Join(home, ".claude", "skills", "deploy.md"), "---\nname: deploy\ndescription: deploy\n---\ndeploy")
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
