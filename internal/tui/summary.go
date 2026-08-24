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
	Gaps     []string      // what was NOT verified, stated rather than implied
	HasStats bool          // git could be asked
}

// verifyCheck is one command that verifies something, and how it went.
type verifyCheck struct {
	Label  string // "tests", "typecheck"
	Detail string // "83 / 83 passing", "2 failing"
	OK     bool
	// Scoped is true when the command named a subset — `go test ./internal/tui`
	// rather than `go test ./...`. A targeted run passing is evidence about
	// that subset and nothing else, and presenting it as "tests pass" is the
	// single most common way a completion message overstates itself.
	Scoped bool
	// Target is the subset that was run, for the not-verified note.
	Target string
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
			label := c.Label
			if c.Scoped && c.Target != "" {
				label = c.Label + " (" + c.Target + ")"
				if len(label) > width {
					width = len(label)
				}
			}
			fmt.Fprintf(&b, "%s %-*s  %s\n", mark, width, label, c.Detail)
		}
	}

	// What was not checked. §15 asks for this explicitly, and it is the half
	// that keeps the other half honest: "auth tests 83/83" reads as proof the
	// change is good until you see "the full suite was not run" beside it.
	if len(s.Gaps) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Not verified\n")
		for _, g := range s.Gaps {
			b.WriteString("– " + g + "\n")
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
	s.Gaps = verificationGaps(s)
	return s
}

// verificationGaps says what was not checked.
//
// Only things that are actually knowable. "The full suite was not run" is a
// fact when a targeted run happened; "the integration tests would fail" is a
// guess, and a summary that guesses is worse than one that stays quiet.
func verificationGaps(s turnSummary) []string {
	var gaps []string

	// Files changed and nothing was run. The most valuable case, and the one a
	// completion message is most tempted to gloss over.
	if len(s.Files) > 0 && len(s.Checks) == 0 {
		return []string{"nothing was run — these changes are unverified"}
	}

	seenFull := map[string]bool{}
	for _, c := range s.Checks {
		if !c.Scoped {
			seenFull[c.Label] = true
		}
	}
	for _, c := range s.Checks {
		if c.Scoped && !seenFull[c.Label] {
			gaps = append(gaps, "the full "+c.Label+" — only "+c.Target+" was run")
			seenFull[c.Label] = true // one note per kind
		}
	}
	return gaps
}

// verificationChecks picks the commands that verify something out of the turn's
// tool results.
//
// Only commands whose job is to check. A passing `go build` is not evidence the
// behaviour is right, and presenting every successful command as a tick would
// turn the block into exactly the narration it replaces.
func verificationChecks(results []toolResult) []verifyCheck {
	var out []verifyCheck
	at := map[string]int{}
	for _, r := range results {
		if r.tool != "Bash" {
			continue
		}
		label, ok := verifyLabel(r.command)
		if !ok {
			continue
		}
		target, scoped := verifyScope(r.command)
		c := verifyCheck{
			Label:  label,
			Detail: verifyDetail(r.content, !r.isError),
			OK:     !r.isError,
			Scoped: scoped,
			Target: target,
		}
		i, exists := at[label]
		if !exists {
			at[label] = len(out)
			out = append(out, c)
			continue
		}
		// The same kind run twice is one line. Which one it shows matters: a
		// failure outranks a pass (a later green run does not unbreak the red
		// one), and among equals the broader run wins — otherwise running the
		// targeted suite first and the full one after would still be reported
		// as "only the subset was run".
		prev := out[i]
		switch {
		case prev.OK && !c.OK:
			out[i] = c
		case prev.OK == c.OK && prev.Scoped && !c.Scoped:
			out[i] = c
		}
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

// everythingOperands are the ways of saying "all of it".
var everythingOperands = map[string]bool{
	"./...": true, ".": true, "...": true, "all": true, "./": true,
}

// notTargets are words that appear before the thing being tested: wrappers,
// runner names, and subcommands. Without them `npx vitest run test/auth` reads
// "vitest" as the target.
var notTargets = map[string]bool{
	// wrappers and package managers
	"npx": true, "pnpm": true, "yarn": true, "npm": true, "bunx": true, "bun": true,
	"poetry": true, "uv": true, "uvx": true, "pipenv": true, "bundle": true,
	"go": true, "cargo": true, "python": true, "python3": true, "node": true,
	"mvn": true, "gradle": true, "./gradlew": true, "make": true, "just": true,
	// runners and checkers
	"vitest": true, "jest": true, "pytest": true, "rspec": true, "eslint": true,
	"tsc": true, "mypy": true, "pyright": true, "ruff": true, "golangci-lint": true,
	"shellcheck": true, "phpunit": true,
	// subcommands
	"test": true, "run": true, "vet": true, "check": true, "lint": true,
	"typecheck": true, "exec": true, "watch": true, "run-script": true,
}

// flagsTakingValues are options whose next word is a value, not a target.
// `go test -run TestX ./pkg` would otherwise report TestX as the subset.
var flagsTakingValues = map[string]bool{
	"-run": true, "-bench": true, "-timeout": true, "-tags": true, "-count": true,
	"-parallel": true, "-cpu": true, "-coverprofile": true, "-o": true,
	"-k": true, "-m": true, "-n": true, "--grep": true, "-t": true,
	"--testNamePattern": true, "--reporter": true, "--config": true, "-c": true,
	"--max-warnings": true, "--ext": true, "-p": true, "--project": true,
}

// verifyScope reports whether a check named a subset, and which.
//
// A run with no operands is the whole thing: `go test`, `npm test`, `pytest`
// all mean everything by default. An operand that is not a flag, not a flag's
// value, and not one of the "all" spellings narrows it.
func verifyScope(command string) (target string, scoped bool) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "", false
	}
	for i := 1; i < len(fields); i++ {
		f := fields[i]
		if strings.HasPrefix(f, "-") {
			if flagsTakingValues[f] {
				i++ // skip its value
			}
			continue
		}
		low := strings.ToLower(f)
		if i := strings.LastIndexByte(low, '/'); i >= 0 && notTargets[low[i+1:]] && !strings.Contains(low, ".") {
			continue // /usr/local/bin/pytest and friends
		}
		if notTargets[low] {
			continue
		}
		if everythingOperands[low] {
			return "", false
		}
		return f, true
	}
	return "", false
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
