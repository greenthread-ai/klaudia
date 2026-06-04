package memory

import (
	"bytes"
	"time"

	"gopkg.in/yaml.v3"
)

// frontmatter is the structured metadata klaudia recognises on a detail note.
// All fields are optional — a file without frontmatter parses to a
// zero-value struct and the full body, no error. The recognised subset
// matches what the pgmarkdown PRD's `frontmatter jsonb` expects to surface.
type frontmatter struct {
	Tags         []string  `yaml:"tags"`
	Created      time.Time `yaml:"created"`
	Updated      time.Time `yaml:"updated"`
	Status       string    `yaml:"status"`
	Supersedes   string    `yaml:"supersedes"`
	SupersededBy string    `yaml:"superseded_by"`
}

// fmFence is the literal YAML-frontmatter delimiter. Hugo / Jekyll /
// Obsidian / pgmarkdown all use the same marker.
var fmFence = []byte("---\n")

// parseFrontmatter splits a markdown file into its frontmatter and body.
// Files without a leading "---\n" block return a zero-value frontmatter
// and content unchanged (body == content). That's not an error — the
// common case today is exactly this shape.
//
// A leading fence with no closing fence is treated as malformed: we keep
// content as the body and zero-value frontmatter, so a typo can't break
// the walk. The audit established that detail notes are agent-written and
// must keep working even when the frontmatter is partial.
func parseFrontmatter(content []byte) (frontmatter, []byte) {
	if !bytes.HasPrefix(content, fmFence) {
		return frontmatter{}, content
	}
	rest := content[len(fmFence):]
	end := bytes.Index(rest, fmFence)
	if end < 0 {
		// Unclosed fence: treat as no frontmatter rather than fail.
		return frontmatter{}, content
	}
	var meta frontmatter
	if err := yaml.Unmarshal(rest[:end], &meta); err != nil {
		// Unparseable YAML: same defensive call as the unclosed-fence path.
		// The note still recalls fine via Title / mtime; only Tags / Status
		// degrade to empty.
		return frontmatter{}, content
	}
	body := rest[end+len(fmFence):]
	// Trim a leading newline that often follows the closing fence so callers
	// don't need to do it themselves.
	body = bytes.TrimPrefix(body, []byte("\n"))
	return meta, body
}
