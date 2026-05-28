// Package goal supports PRD-anchored autonomous iteration (the "/goal run"
// Ralph loop): locating/templating a goal spec, framing goal-setting and
// iteration turns, and detecting completion. The helpers are pure so both the
// TUI loop and the headless `klaudia --loop` share them.
package goal

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
		"section with a shell command that proves success (build/tests).\n\n" +
		"If the work is broken into phases / milestones / deliverables, end the spec with a " +
		"`## Progress` section containing one `- [ ] Phase N — <title>` line for every phase " +
		"described in the body — no gaps. The goal loop uses these checkboxes as the mechanical " +
		"signal for \"done\"; phases described in the body but missing from `## Progress` cause " +
		"premature termination.\n\n" +
		"Do NOT implement the goal yet — only define and write the spec. Keep it tight.")
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

// CountUnchecked returns how many "- [ ]" / "* [ ]" lines remain in the spec.
// Used as the cheap mechanical gate before honouring <goal-complete/>: if the
// model claims completion while unchecked items remain in the spec it can see,
// the claim is rejected and the loop continues. Catches "model forgot one it
// could see" but NOT "Progress tracker is missing rows for phases described
// in the body" — RequiresStubFix handles that.
func CountUnchecked(specPath string) (int, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "- [ ]") || strings.HasPrefix(t, "* [ ]") {
			n++
		}
	}
	return n, nil
}

// phaseInHeadingPattern matches a markdown heading line whose title starts
// with "Phase N" — e.g. `## Phase 3` or `### Phase 3 — Static board rendering`.
var phaseInHeadingPattern = regexp.MustCompile(`(?m)^\s*#+\s+(Phase\s+\d+(?:\s*[—:\-]\s*[^\n]+?)?)\s*$`)

// phaseInBoldPattern matches a line that opens with a bold-emphasis "Phase N"
// label, regardless of what follows — `**Phase 5**: audio`, `**Phase 5 — Audio**`.
// We only need the label captured; trailing text on the line is ignored.
var phaseInBoldPattern = regexp.MustCompile(`(?m)^\s*\*\*(Phase\s+\d+(?:\s*[—:\-]\s*[^*\n]+)?)\*\*`)

// phaseInTrackerPattern matches a checkbox tracker entry — `- [ ]` or `- [x]`
// (and `*` variant) — that references a phase. Captures the "Phase N" prefix
// for matching against body mentions.
var phaseInTrackerPattern = regexp.MustCompile(`(?mi)^\s*[-*]\s*\[\s*[ xX]?\s*\]\s*(?:\*\*)?(Phase\s+\d+)`)

// progressHeadingPattern finds a section heading the spec uses as its progress
// tracker — common names accepted so users aren't forced into a single label.
// Anchors at the start of a markdown heading line.
var progressHeadingPattern = regexp.MustCompile(`(?im)^\s*#{1,4}\s+(progress|status|deliverables|tracker|done)\b`)

// phaseNumberPattern extracts the integer suffix of "Phase N" for normalised
// keying when comparing body mentions to tracker entries.
var phaseNumberPattern = regexp.MustCompile(`(?i)^Phase\s+(\d+)`)

// RequiresStubFix scans the spec and returns the names of phases mentioned in
// the body that are NOT present in the `## Progress` (or equivalently-named)
// tracker section. Only fires when such a section is present — specs that use
// the Acceptance-criteria checklist as their only tracker pass straight through.
// Returns missing phase labels in body order (e.g. ["Phase 5 — Audio"]) so a
// prompt can show the user exactly what's missing.
func RequiresStubFix(specPath string) (missing []string, err error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, err
	}
	text := string(data)
	loc := progressHeadingPattern.FindStringIndex(text)
	if loc == nil {
		// No tracker section, no convention to enforce.
		return nil, nil
	}
	body, tracker := text[:loc[0]], text[loc[0]:]

	tracked := map[string]bool{}
	for _, m := range phaseInTrackerPattern.FindAllStringSubmatch(tracker, -1) {
		tracked[normalizePhaseKey(m[1])] = true
	}

	// Collect phase mentions in body order across both styles, by walking the
	// body line-by-line so order is preserved for the user-facing missing list.
	seen := map[string]bool{}
	consider := func(label string) {
		label = strings.TrimSpace(label)
		key := normalizePhaseKey(label)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		if !tracked[key] {
			missing = append(missing, label)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if m := phaseInHeadingPattern.FindStringSubmatch(line); m != nil {
			consider(m[1])
			continue
		}
		if m := phaseInBoldPattern.FindStringSubmatch(line); m != nil {
			consider(m[1])
		}
	}
	return missing, nil
}

// normalizePhaseKey reduces a phase label to its canonical "phase N" form so
// "## Phase 3 — Title" and "- [ ] Phase 3" compare equal.
func normalizePhaseKey(s string) string {
	if m := phaseNumberPattern.FindStringSubmatch(s); m != nil {
		return "phase " + m[1]
	}
	return ""
}

// StubFixPrompt is the one-shot prompt for the iteration that runs when the
// pre-loop scan finds phases in the body that the Progress tracker doesn't
// list. Fixing the spec first means the mechanical CountUnchecked gate is then
// trustworthy for the rest of the run.
func StubFixPrompt(specPath string, missing []string) string {
	list := strings.Join(missing, ", ")
	return "Before iterating on the goal, the Progress tracker in " + specPath + " is incomplete. " +
		"The spec body describes the following phases that are NOT yet listed as `- [ ]` items in the " +
		"`## Progress` section: " + list + ".\n\n" +
		"Open " + specPath + ", append a `- [ ] Phase N — <title>` line for each of those phases " +
		"under `## Progress` (in spec order, with the same titles used in the body), and commit only " +
		"the spec update. Do NOT start any other work — the next iteration handles that."
}

// VerificationPrompt is the final-review prompt that fires once when the loop
// receives a <goal-complete/> claim and the mechanical CountUnchecked gate
// passes. Its job is the holistic re-read: cross-reference the spec body
// against the Progress tracker, against git history, and against the build/
// tests — and either confirm or list remaining work. Belt-and-suspenders on
// top of CountUnchecked: catches "tracker says done but body has phases the
// tracker never had rows for" (the case RequiresStubFix is supposed to prevent
// up-front, but this is the safety net).
func VerificationPrompt(specPath string) string {
	return "You said the goal is complete. Before that's accepted, do a final independent review:\n\n" +
		"1. Re-read " + specPath + " from disk in full — do not rely on the conversation context. " +
		"List every phase, milestone, or acceptance criterion the spec body describes (not just the " +
		"Progress tracker).\n" +
		"2. For each, state explicitly whether it has been demonstrably done in code/tests/git " +
		"(cite a commit, a file, or a passing test).\n" +
		"3. If the `## Progress` tracker is missing entries for items the spec body describes, the " +
		"tracker is wrong — update it before claiming completion.\n" +
		"4. Run the spec's Verify command (or the build + tests) and confirm it passes.\n\n" +
		"If everything in the body is demonstrably done AND verification passes, reply with exactly " +
		CompleteToken + " on its own line. Otherwise, list what remains, do NOT claim completion, " +
		"and end the turn so the loop can continue."
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
