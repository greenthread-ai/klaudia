package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// §9's other half: results should dominate narration.
//
// A turn used to end with "✓ done in 1m12s" and nothing else. Everything the
// user actually wanted to know — what changed, whether the tests passed, how
// much code moved — was scattered through the tool lines above, in the order
// Klaudia happened to do things rather than the order a person reads in.
//
// This is the summary block. It says what changed and what was verified,
// because those are different claims and conflating them is how "I fixed it"
// comes to mean "it compiles". What it does not do is guess: a turn with no
// edits and no test runs prints nothing rather than an empty ceremony.

// turnSummary is what a finished turn produced.
type turnSummary struct {
	Files    []string      // repo-relative paths Klaudia changed
	Added    int           // lines added, from git
	Removed  int           // lines removed
	Checks   []verifyCheck // commands that constitute verification
	HasStats bool          // git could be asked
}

// verifyCheck is one command that verifies something, and how it went.
type verifyCheck struct {
	Label  string // "auth tests", "typecheck"
	Detail string // "83 / 83 passing", "exit 1"
	OK     bool
}

// Empty reports whether there is nothing worth printing.
func (s turnSummary) Empty() bool { return len(s.Files) == 0 && len(s.Checks) == 0 }

// Render lays the summary out with the results first and the accounting last.
func (s turnSummary) Render() string {
	if s.Empty() {
		return ""
	}
	var b strings.Builder

	if len(s.Checks) > 0 {
		width := 0
		for _, c := range s.Checks {
			if len(c.Label) > width {
				width = len(c.Label)
			}
		}
		for _, c := range s.Checks {
			mark := "✓"
			if !c.OK {
				mark = "✗"
			}
			fmt.Fprintf(&b, "%s %-*s  %s\n", mark, width, c.Label, c.Detail)
		}
	}

	if len(s.Files) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		noun := "files"
		if len(s.Files) == 1 {
			noun = "file"
		}
		fmt.Fprintf(&b, "%d %s changed", len(s.Files), noun)
		if s.HasStats && (s.Added > 0 || s.Removed > 0) {
			fmt.Fprintf(&b, "  +%d -%d", s.Added, s.Removed)
		}
		b.WriteString("\n")
		for _, f := range s.Files {
			b.WriteString("  " + f + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// buildSummary assembles the block from what the session already tracked.
func buildSummary(touched map[string]bool, results []toolResult, gitStat func() (int, int, bool)) turnSummary {
	var s turnSummary
	for f := range touched {
		s.Files = append(s.Files, f)
	}
	sort.Strings(s.Files)
	if len(s.Files) > 0 && gitStat != nil {
		s.Added, s.Removed, s.HasStats = gitStat()
	}
	s.Checks = verificationChecks(results)
	return s
}

// verificationChecks picks the commands that verify something out of the turn's
// tool results.
//
// Only commands whose job is to check. A passing `go build` is not evidence the
// behaviour is right, and presenting every successful command as a tick would
// turn the block into exactly the narration it replaces.
func verificationChecks(results []toolResult) []verifyCheck {
	var out []verifyCheck
	seen := map[string]bool{}
	for _, r := range results {
		if r.tool != "Bash" {
			continue
		}
		label, ok := verifyLabel(r.command)
		if !ok || seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, verifyCheck{
			Label:  label,
			Detail: verifyDetail(r.content, !r.isError),
			OK:     !r.isError,
		})
	}
	return out
}

// verifyKinds map a command shape to what it verifies. Ordered longest-first at
// match time so "go vet" is not read as "go".
var verifyKinds = []struct{ match, label string }{
	{"go test", "tests"},
	{"npm test", "tests"},
	{"npm run test", "tests"},
	{"pnpm test", "tests"},
	{"yarn test", "tests"},
	{"pytest", "tests"},
	{"cargo test", "tests"},
	{"mvn test", "tests"},
	{"gradle test", "tests"},
	{"make test", "tests"},
	{"jest", "tests"},
	{"vitest", "tests"},
	{"rspec", "tests"},
	{"go vet", "vet"},
	{"tsc", "typecheck"},
	{"mypy", "typecheck"},
	{"pyright", "typecheck"},
	{"npm run typecheck", "typecheck"},
	{"eslint", "lint"},
	{"ruff", "lint"},
	{"golangci-lint", "lint"},
	{"npm run lint", "lint"},
	{"make lint", "lint"},
	{"shellcheck", "lint"},
}

func verifyLabel(command string) (string, bool) {
	c := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if c == "" {
		return "", false
	}
	best := ""
	bestLen := 0
	for _, k := range verifyKinds {
		if strings.Contains(c, k.match) && len(k.match) > bestLen {
			best, bestLen = k.label, len(k.match)
		}
	}
	return best, best != ""
}

// testCountRe pulls a pass/total out of the usual runner formats.
var testCountRe = []*regexp.Regexp{
	regexp.MustCompile(`(?i)Tests:\s+(\d+)\s+passed,\s+(\d+)\s+total`), // jest / vitest
	regexp.MustCompile(`(?i)(\d+)\s+passed[,;]\s+(\d+)\s+(?:total|tests)`),
	regexp.MustCompile(`(?i)(\d+)\s+examples?,\s+(\d+)\s+failures?`), // rspec
	regexp.MustCompile(`(?i)(\d+)\s+passed\b`),
}

// verifyDetail summarises a check's output in a few words.
//
// The counts come from the runner's own summary line where there is one. Where
// there is not, "passed" or the exit code is the honest answer — inventing a
// number would be worse than not having one.
func verifyDetail(output string, ok bool) string {
	if !ok {
		if n := countFailures(output); n > 0 {
			return fmt.Sprintf("%d failing", n)
		}
		if code := exitCode(output); code != "" {
			return "failed (exit " + code + ")"
		}
		return "failed"
	}
	for _, re := range testCountRe {
		if m := re.FindStringSubmatch(output); m != nil {
			if len(m) == 3 {
				pass, _ := strconv.Atoi(m[1])
				total, _ := strconv.Atoi(m[2])
				// rspec reports failures, not totals; a 0 there means all passed.
				if strings.Contains(strings.ToLower(re.String()), "failures") {
					return fmt.Sprintf("%d / %d passing", pass-total, pass)
				}
				return fmt.Sprintf("%d / %d passing", pass, total)
			}
			return m[1] + " passing"
		}
	}
	return "passed"
}

var failCountRe = regexp.MustCompile(`(?i)(\d+)\s+(?:failed|failing|failures?)`)
var exitCodeRe = regexp.MustCompile(`\[exit code (\d+)\]`)

func countFailures(output string) int {
	if m := failCountRe.FindStringSubmatch(output); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	// Go's runner prints one --- FAIL: line per failing test.
	if n := strings.Count(output, "--- FAIL:"); n > 0 {
		return n
	}
	return 0
}

func exitCode(output string) string {
	if m := exitCodeRe.FindStringSubmatch(output); m != nil {
		return m[1]
	}
	return ""
}

// numstat parses `git diff --numstat` output into totals.
func numstat(out string) (added, removed int) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// A binary file reports "-" for both counts.
		if a, err := strconv.Atoi(fields[0]); err == nil {
			added += a
		}
		if r, err := strconv.Atoi(fields[1]); err == nil {
			removed += r
		}
	}
	return added, removed
}

// turnSummaryBlock renders the completion block for the turn that just ended.
func (m *Model) turnSummaryBlock() string {
	s := buildSummary(m.turnTouched, m.results.since(m.turnResultsFrom), m.gitNumstat)
	if s.Empty() {
		return ""
	}
	body := s.Render()
	// Changed and verified are different claims. The hint names the way to see
	// the change itself, and deliberately does not offer an undo that does not
	// exist yet.
	return "\n" + bannerStyle.Render(body) + "\n" + hintStyle.Render("  /diff to review · /commit to keep")
}

// gitNumstat totals the working tree's line changes.
func (m *Model) gitNumstat() (added, removed int, ok bool) {
	if m.sess == nil || m.sess.CWD == "" {
		return 0, 0, false
	}
	out, err := gitOutput(m.sess.CWD, "diff", "--numstat", "HEAD")
	if err != nil {
		// Not a repo, or no HEAD yet. The file list is still useful.
		return 0, 0, false
	}
	a, r := numstat(out)
	return a, r, true
}
