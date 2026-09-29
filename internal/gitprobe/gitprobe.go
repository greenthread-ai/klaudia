// Package gitprobe runs the read-only git commands Klaudia issues on its own —
// the status and branch lookups at startup and in the prompt, and /diff — so
// that the repository being looked at cannot run a program through them.
//
// A repository's .git/config is not part of what a clone transfers, but it is
// part of what an archive, a vendored checkout or a nested repository carries,
// and git runs what it names: core.fsmonitor on every status, diff.external
// and textconv drivers on every diff. Those commands used to run as a side
// effect of opening Klaudia in such a folder. Upstream Claude Code fixed the
// same class in 2.1.265.
//
// Not covered: clean/smudge filters, which have no single switch to turn off.
// Commands the user asks for (/commit, the goal loop's branch and commit) run
// plain git on purpose — hooks are part of what the user asked for there.
package gitprobe

import "os/exec"

// guard is prepended to every probe. core.fsmonitor=false stops the fsmonitor
// hook; core.hooksPath=/dev/null keeps any hook from running; diff.external=
// clears an external diff driver; --no-optional-locks keeps a status from
// writing to the index of a repository Klaudia is only looking at.
var guard = []string{
	"-c", "core.fsmonitor=false",
	"-c", "core.hooksPath=/dev/null",
	"-c", "diff.external=",
	"--no-optional-locks",
}

// Command returns an exec.Cmd for `git <args>` in dir with the guard applied.
// For a diff it also passes --no-ext-diff and --no-textconv, which cover the
// per-path drivers that .gitattributes selects.
func Command(dir string, args ...string) *exec.Cmd {
	full := append(append([]string{}, guard...), args...)
	if len(args) > 0 && args[0] == "diff" {
		full = append(append(append([]string{}, guard...), "diff", "--no-ext-diff", "--no-textconv"), args[1:]...)
	}
	c := exec.Command("git", full...)
	c.Dir = dir
	return c
}
