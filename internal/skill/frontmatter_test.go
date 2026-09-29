package skill

import (
	"path/filepath"
	"strings"
	"testing"
)

// Real SKILL.md files write descriptions as prose, and "Triggers include: x"
// is not valid YAML. Strict parsing turned those skills away.
func TestParseDescriptionWithColon(t *testing.T) {
	src := "---\nname: decks\ndescription: Use for slides. Triggers include: 'deck', \"pptx\"\n  and presentations.\ntools: [Read, Write]\n---\nBody."
	s, err := parse([]byte(src), "/skills/decks/SKILL.md")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Name != "decks" || s.Description != `Use for slides. Triggers include: 'deck', "pptx" and presentations.` {
		t.Errorf("name %q description %q", s.Name, s.Description)
	}
	if len(s.Tools) != 2 || s.Tools[0] != "Read" || s.Tools[1] != "Write" {
		t.Errorf("tools = %v", s.Tools)
	}
	if s.Body != "Body." {
		t.Errorf("body = %q", s.Body)
	}
}

// Frontmatter that no reading can make sense of is still an error.
func TestParseUnreadableFrontmatterStillFails(t *testing.T) {
	if _, err := parse([]byte("---\n: : :\n  - [\n---\nBody"), "/skills/x.md"); err == nil {
		t.Error("want an error for frontmatter with no known keys")
	}
}

// A byte-order mark hid the opening fence, so the skill lost its description.
func TestParseByteOrderMark(t *testing.T) {
	s, err := parse([]byte("\ufeff---\ndescription: Review the diff\n---\nBody"), "/skills/review.md")
	if err != nil || s.Description != "Review the diff" || s.Body != "Body" {
		t.Errorf("got %+v, %v; want the frontmatter read", s, err)
	}
}

func TestRenderSkillDir(t *testing.T) {
	path := filepath.Join("/opt", "skills", "pdf", "SKILL.md")
	s := Skill{Body: "Run ${CLAUDE_SKILL_DIR}/scripts/fill.py or ${KLAUDIA_SKILL_DIR}/x on $ARGUMENTS", Path: path}
	got := s.Render("form.pdf")
	want := "Run /opt/skills/pdf/scripts/fill.py or /opt/skills/pdf/x on form.pdf"
	if got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
	if strings.Contains(Skill{Body: "${CLAUDE_SKILL_DIR}"}.Render(""), "/") {
		t.Error("a skill with no path should leave the placeholder alone")
	}
}
