// Package goal supports PRD-anchored autonomous iteration (the "/goal run"
// Ralph loop): locating/templating a goal spec, framing goal-setting and
// iteration turns, and detecting completion. The helpers are pure so both the
// TUI loop and the headless `klaudia --loop` share them.
package goal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CompleteToken is the exact marker an iteration emits when the goal is fully
// satisfied and verified. Matches the Ralph "completion promise" pattern.
const CompleteToken = "<goal-complete/>"

// DefaultIterations is used when `/goal run` is given no count; MaxIterations is
// the hard cap, so a runaway loop can't burn unbounded tokens/time.
const (
	DefaultIterations = 10
	MaxIterations     = 50
)

// SpecPath returns the goal-spec path for cwd and whether it already exists.
// Preference order: ./PRD.md, then ./.klaudia/GOAL.md. When neither exists it
// returns the default create path (.klaudia/GOAL.md) with found=false.
func SpecPath(cwd string) (path string, found bool) {
	candidates := []string{
		filepath.Join(cwd, "PRD.md"),
		filepath.Join(cwd, ".klaudia", "GOAL.md"),
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, true
		}
	}
	return candidates[1], false
}

// Read returns the spec contents and its path. If no spec exists yet, text is ""
// and err is nil (the default create path is still returned).
func Read(cwd string) (text, path string, err error) {
	path, found := SpecPath(cwd)
	if !found {
		return "", path, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", path, err
	}
	return string(data), path, nil
}

// Template returns a starter goal spec. objective seeds the Objective section
// (a placeholder is used when empty).
func Template(objective string) string {
	if strings.TrimSpace(objective) == "" {
		objective = "Describe what you want built or achieved, in a sentence or two."
	}
	return "# Goal\n\n" +
		"## Objective\n\n" + strings.TrimSpace(objective) + "\n\n" +
		"## Acceptance criteria\n\n" +
		"- [ ] First concrete, verifiable outcome\n" +
		"- [ ] Second outcome\n\n" +
		"## Verify\n\n" +
		"```\nCGO_ENABLED=0 go build ./... && go test ./internal/...\n```\n"
}

// FacilitatorPrompt frames a goal-setting turn: the model interviews the user
// and writes/refines the spec at specPath, but does not implement the goal.
// existing is the current spec contents ("" if none yet).
func FacilitatorPrompt(specPath, existing string) string {
	var b strings.Builder
	b.WriteString("You are helping the user define a clear goal specification, saved at ")
	b.WriteString(specPath)
	b.WriteString(".\n\n")
	if strings.TrimSpace(existing) == "" {
		b.WriteString("There is no spec yet.\n\n")
	} else {
		b.WriteString("The current spec is:\n\n")
		b.WriteString(existing)
		b.WriteString("\n\nRefine it based on the conversation.\n\n")
	}
	b.WriteString("Briefly interview the user to understand the objective, then use the Write tool to " +
		"save " + specPath + " as a concise Markdown spec with: a one-paragraph Objective; an " +
		"\"Acceptance criteria\" checklist of concrete, verifiable \"- [ ]\" items; and a \"Verify\" " +
		"section with a shell command that proves success (build/tests). Do NOT implement the goal " +
		"yet — only define and write the spec. Keep it tight.")
	return b.String()
}

// IterationPrompt is the fixed prompt fed on every loop iteration. It is
// intentionally constant: progress accumulates in files and git history, not in
// the conversation context (the Ralph principle).
func IterationPrompt(specPath string) string {
	return "You are autonomously iterating toward the goal defined in " + specPath + ".\n" +
		"Re-read " + specPath + " now to recall the objective, acceptance criteria, and the " +
		"\"## Progress\" notes from earlier iterations. Then inspect the current state of the " +
		"repository (git status, git log, and run the build and tests). " +
		"Choose the SINGLE most valuable next step toward satisfying the acceptance criteria, " +
		"implement it, verify it (build + tests, or the spec's Verify command), and commit your work " +
		"with a clear message.\n\n" +
		"Before ending the turn, update " + specPath + " and commit it: tick the checklist items you " +
		"verified, and maintain a short \"## Progress\" section recording what is done, what remains, " +
		"and the single best next step — so the next run (or a person) can resume from the spec alone.\n\n" +
		"If and only if EVERY acceptance criterion is satisfied and verification passes, reply with " +
		"exactly " + CompleteToken + " on its own line and make no further changes. Otherwise, end " +
		"your turn after committing this step."
}

// WrapUpPrompt is run once when the loop stops without completing (iteration cap
// or stall). It asks the model to record an honest end-of-run summary in the
// spec — no new work — so the next run or a person can resume cleanly.
func WrapUpPrompt(specPath string) string {
	return "The goal loop is stopping before the goal is complete. Do NOT make code changes or " +
		"start new work. Review " + specPath + " and the recent git history, then update " + specPath +
		" so it reflects reality: tick the acceptance criteria that are genuinely done, and write a " +
		"clear \"## Progress\" section summarising what was accomplished this run, what remains, any " +
		"blockers, and the single best next step to resume. Commit only that spec update."
}

// IsComplete reports whether an iteration's final text signals completion.
func IsComplete(text string) bool {
	return strings.Contains(text, CompleteToken)
}

// Iterations clamps a requested loop count: <=0 means "use the default", and
// anything above the cap is clamped to MaxIterations.
func Iterations(requested int) int {
	switch {
	case requested <= 0:
		return DefaultIterations
	case requested > MaxIterations:
		return MaxIterations
	default:
		return requested
	}
}

// BranchName derives the loop's working branch from the spec's first heading
// (its objective), as klaudia/goal-<slug>. Falls back to "klaudia/goal". A
// leading "goal" in the slug is dropped so an objective like "Goal: …" doesn't
// produce "klaudia/goal-goal-…".
func BranchName(spec string) string {
	slug := strings.TrimPrefix(slugify(firstHeading(spec)), "goal-")
	if slug == "" || slug == "goal" {
		return "klaudia/goal"
	}
	return "klaudia/goal-" + slug
}

// MergeHint tells the user where the loop's work landed and how to review/merge
// it. base is the branch the loop started from ("" or == branch when unknown).
func MergeHint(branch, base string) string {
	if base == "" || base == branch {
		return fmt.Sprintf("Work is on branch %s — review it (git log / git diff) and merge into your main branch when ready.", branch)
	}
	return fmt.Sprintf("Work is on branch %s — review with `git diff %s...%s`, then merge with `git switch %s && git merge %s`.", branch, base, branch, base, branch)
}

// structuralHeadings are the template's section titles; they make poor branch
// slugs, so firstHeading skips them in favour of the actual objective text.
var structuralHeadings = map[string]bool{
	"goal": true, "objective": true, "acceptance criteria": true, "verify": true,
}

// firstHeading returns the first meaningful line of the spec — the objective
// text — skipping structural section titles, checklist markers, and code
// fences. Leading '#'s are stripped. Returns "" if nothing suitable is found.
func firstHeading(spec string) string {
	for _, line := range strings.Split(spec, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		isHeading := strings.HasPrefix(t, "#")
		t = strings.TrimSpace(strings.TrimLeft(t, "#"))
		switch {
		case t == "":
			continue
		case isHeading && structuralHeadings[strings.ToLower(t)]:
			continue // a section title like "## Objective"
		case strings.HasPrefix(t, "- ["), strings.HasPrefix(t, "```"):
			continue // checklist item or code fence
		}
		return t
	}
	return ""
}

// slugify lowercases s and reduces it to a short, branch-safe a-z0-9 slug with
// single hyphens between words (max ~32 chars).
func slugify(s string) string {
	var b strings.Builder
	lastHyphen := true // avoid leading hyphen
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
		if b.Len() >= 32 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}
