package skill

import (
	"embed"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// Bundled skills ship inside the binary, so a fresh install has useful
// workflows (review, feature development) without anything to install. They are
// the lowest-precedence layer: a skill of the same name in any skills directory
// replaces the bundled one, which is also how a user turns one off or rewrites
// it.
//
// The prompts are written for Klaudia's own tools and sub-agent types. They are
// modelled on the workflows of Claude Code's published plugins (code-review,
// pr-review-toolkit, feature-dev) but are original text: that repository is
// all-rights-reserved, so its prompts are not copied here.
//
//go:embed bundled/*.md
var bundledFS embed.FS

// bundledPathPrefix marks a bundled skill's Path, which names no file on disk.
const bundledPathPrefix = "bundled:"

// Bundled returns the skills compiled into the binary, sorted by name. A file
// that fails to parse is a build defect caught by the package tests, so it is
// skipped here rather than reported to every user at startup.
func Bundled() []Skill {
	entries, err := fs.ReadDir(bundledFS, "bundled")
	if err != nil {
		return nil
	}
	out := make([]Skill, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := bundledFS.ReadFile(path.Join("bundled", e.Name()))
		if err != nil {
			continue
		}
		sk, err := parseNamed(data, bundledPathPrefix+e.Name(), strings.TrimSuffix(e.Name(), ".md"))
		if err != nil {
			continue
		}
		sk.Bundled = true
		out = append(out, sk)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
