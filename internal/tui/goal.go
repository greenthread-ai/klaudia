package tui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/goal"
)

// toggleGoalSetting flips goal-setting mode. Turning it on loads any existing
// spec (PRD.md / .klaudia/GOAL.md) or invites the user to describe the goal so
// the model can draft one on the next turn; turning it off readies the loop.
func (m *Model) toggleGoalSetting() (tea.Model, tea.Cmd) {
	if m.goalSetting {
		m.goalSetting = false
		m.appendLine(bannerStyle.Render("Goal-setting finished. /goal run [N] to start the loop."))
		return m, nil
	}
	m.goalSetting = true
	cwd := ""
	if m.sess != nil {
		cwd = m.sess.CWD
	}
	text, specPath, _ := goal.Read(cwd)
	if strings.TrimSpace(text) != "" {
		m.appendLine(bannerStyle.Render(fmt.Sprintf(
			"Goal-setting on. Loaded spec from %s:\n  %s\nRefine it in chat, /goal to finish, then /goal run to start.",
			specPath, oneline(text, 100))))
	} else {
		m.appendLine(bannerStyle.Render(fmt.Sprintf(
			"Goal-setting on — no spec yet. Tell me what you're building and I'll draft %s. /goal to finish.",
			specPath)))
	}
	return m, nil
}

// startGoalLoop kicks off the "/goal run [N]" Ralph loop: requires a spec, moves
// onto a dedicated branch when possible, then runs up to N interruptible
// iterations (each re-invoking the agent with goal.IterationPrompt; subsequent
// iterations are started by the doneMsg hook). Reached only from an idle
// KeyMsg, so — like the normal turn-start — it must NOT arm waitForEvent (the
// single reader is already outstanding).
func (m *Model) startGoalLoop(args []string) (tea.Model, tea.Cmd) {
	cwd := ""
	if m.sess != nil {
		cwd = m.sess.CWD
	}
	specText, specPath, _ := goal.Read(cwd)
	if strings.TrimSpace(specText) == "" {
		m.appendLine(errStyle.Render("No goal spec found. Run /goal first to create one (PRD.md or .klaudia/GOAL.md)."))
		return m, nil
	}

	n := goal.DefaultIterations
	if len(args) > 0 {
		v, err := strconv.Atoi(args[0])
		if err != nil || v <= 0 {
			m.appendLine(errStyle.Render("usage: /goal run [N]  (N = max iterations, a positive integer)"))
			return m, nil
		}
		n = v
	}
	if n > goal.MaxIterations {
		n = goal.MaxIterations
		m.appendLine(toolStyle.Render(fmt.Sprintf("  capped at %d iterations.", goal.MaxIterations)))
	}

	// Move onto a dedicated branch so iterations stay isolated and revertible.
	// Reuse the branch if it already exists (resume prior work) rather than
	// resetting it, so a second /goal run continues from earlier commits.
	m.loopBranch, m.loopBaseBranch = "", ""
	if cwd != "" {
		branch := goal.BranchName(specText)
		// Capture the branch we're starting from (the merge target) before we
		// switch, unless we're already sitting on the goal branch (a resume).
		if base, err := gitOutput(cwd, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			if b := strings.TrimSpace(base); b != branch {
				m.loopBaseBranch = b
			}
		}
		args := []string{"checkout", "-b", branch}
		if _, err := gitOutput(cwd, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
			args = []string{"checkout", branch}
		}
		if out, err := gitOutput(cwd, args...); err != nil {
			m.appendLine(toolStyle.Render("  (not branching: " + strings.TrimSpace(out) + ")"))
		} else {
			m.loopBranch = branch
			m.appendLine(toolStyle.Render("  ↳ on branch " + branch))
		}
	}

	m.goalSetting = false
	m.loopTotal, m.loopRemaining, m.loopSpecPath = n, n, specPath
	m.loopStubFixing, m.loopVerifying = false, false
	m.appendLine(bannerStyle.Render(fmt.Sprintf("Goal loop: up to %d iterations against %s. Esc or /goal stop to halt.", n, specPath)))

	prompt := m.prepareFirstLoopTurn(specPath, n)
	m.setState(stateRunning)
	return m, m.startTurn(prompt)
}

// prepareFirstLoopTurn selects the first turn's prompt for /goal run: either
// a stub-fix turn (when the Progress tracker doesn't list every phase the body
// describes — fixing that up-front makes the mechanical CountUnchecked gate
// trustworthy for the rest of the run) or the normal iteration prompt. Side
// effects (loopStubFixing, scrollback) are confined here so the caller stays
// linear and tests can drive this without standing up a full Tea runtime.
func (m *Model) prepareFirstLoopTurn(specPath string, n int) string {
	if missing, err := goal.RequiresStubFix(specPath); err == nil && len(missing) > 0 {
		m.loopStubFixing = true
		m.appendLine(toolStyle.Render(fmt.Sprintf("  ⚠ Progress tracker missing stubs for: %s — repairing first.", strings.Join(missing, ", "))))
		m.appendLine(toolStyle.Render(fmt.Sprintf("  ↻ iteration 1/%d (stub fix)", n)))
		return goal.StubFixPrompt(specPath, missing)
	}
	m.appendLine(toolStyle.Render(fmt.Sprintf("  ↻ iteration 1/%d", n)))
	return goal.IterationPrompt(specPath)
}

// loopIsStall reports whether a finished iteration looks like an undetected
// failure — finished without error, but the model produced literally nothing
// (no text, no turn count, no output tokens). Anthropic's session-limit and
// some other throttling cases can manifest as a successful HTTP response with
// an empty stream rather than a 429, and without this check the loop happily
// decrements its budget for nothing while the user wonders why the spinner is
// spinning. All three signals must be zero — a turn that returned text but
// no tokens (e.g. a stubby test fixture) or no text but at least one inner
// turn (refusal, tool-only) is real work and shouldn't trip this.
func loopIsStall(res agent.Result, err error) bool {
	return err == nil && res.Text == "" && res.NumTurns == 0 && res.OutputTokens == 0
}

// loopNext updates goal-loop state after a turn finished and returns the prompt
// for the next loop turn, or "" to stop. The completion path runs a two-layer
// gate against premature <goal-complete/>:
//
//  1. Mechanical: count `- [ ]` lines in the spec. If any remain, reject the
//     claim and continue iterating — catches "model forgot one it could see".
//  2. Verification: if the mechanical gate passes, fire ONE final-review turn
//     that forces a holistic re-read (body vs Progress tracker vs git/tests).
//     Only completion from THAT turn ends the loop — belt-and-suspenders for
//     "tracker is missing rows that body phases never had" (the huedoku failure
//     mode that RequiresStubFix already tries to prevent up-front).
//
// Stub-fix and verification turns don't consume an iteration each: stub-fix
// happens before normal iteration starts (counted as iteration 1 in display
// only), and verification is a single check on top.
func (m *Model) loopNext(res agent.Result, err error) string {
	if m.loopWrapUp { // the wrap-up turn just finished
		m.loopWrapUp = false
		if err == nil {
			m.appendLine(toolStyle.Render(fmt.Sprintf("  summary written to %s — /goal run to resume.", filepath.Base(m.loopSpecPath))))
		}
		m.appendMergeHint()
		return ""
	}
	if err != nil {
		m.loopRemaining = 0
		m.appendLine(toolStyle.Render("  ⊘ goal loop halted."))
		m.appendMergeHint()
		return ""
	}
	// Stall: turn finished without error but produced nothing. Most often this
	// is the Anthropic API returning a successful-looking empty stream during
	// session-limit / quota throttling — without this check the loop would
	// blow through its budget on empty turns. Halt with a clear line so the
	// user can fix the underlying cause (wait for limit reset, switch model,
	// etc.) before re-running.
	if loopIsStall(res, err) {
		m.loopRemaining = 0
		m.appendLine(errStyle.Render("  ⊘ empty response from model — likely rate-limited or session-limit-hit. Stopping the loop."))
		m.appendMergeHint()
		return ""
	}
	// Stub-fix turn just finished — fall through to normal iteration for the
	// remaining budget (decrement happens in the default branch).
	if m.loopStubFixing {
		m.loopStubFixing = false
	}

	if goal.IsComplete(res.Text) {
		// Mechanical gate first.
		if n, cerr := goal.CountUnchecked(m.loopSpecPath); cerr == nil && n > 0 {
			m.appendLine(toolStyle.Render(fmt.Sprintf("  ✗ completion claim rejected: %d unchecked item(s) remain in %s", n, filepath.Base(m.loopSpecPath))))
			m.loopVerifying = false
			return m.continueIteration()
		}
		// If we just ran the verification turn and it also says complete, exit.
		if m.loopVerifying {
			m.loopVerifying = false
			done := m.loopTotal - m.loopRemaining + 1
			m.loopRemaining = 0
			m.appendLine(bannerStyle.Render(fmt.Sprintf("  ✓ goal complete (verified) in %d iteration(s).", done)))
			m.appendMergeHint()
			return ""
		}
		// Mechanical passed but verification hasn't fired yet — fire it once.
		// Doesn't decrement the budget; it's an insurance check, not new work.
		m.loopVerifying = true
		m.appendLine(toolStyle.Render("  completion claimed; running final verification…"))
		return goal.VerificationPrompt(m.loopSpecPath)
	}

	// Non-complete turn. If we just did a verification turn, the model decided
	// more work was needed — surface that and resume normal iteration.
	if m.loopVerifying {
		m.loopVerifying = false
		m.appendLine(toolStyle.Render("  verification flagged remaining work; continuing iteration."))
	}
	return m.continueIteration()
}

// continueIteration consumes one slot from loopRemaining and returns the prompt
// for the next iteration — or kicks off the wrap-up turn when the cap is hit.
// Centralised so the rejection path and the ordinary path can't drift apart.
func (m *Model) continueIteration() string {
	m.loopRemaining--
	if m.loopRemaining > 0 {
		next := m.loopTotal - m.loopRemaining + 1
		m.appendLine(toolStyle.Render(fmt.Sprintf("  ↻ iteration %d/%d", next, m.loopTotal)))
		return goal.IterationPrompt(m.loopSpecPath)
	}
	m.loopWrapUp = true
	m.appendLine(toolStyle.Render(fmt.Sprintf("  stopped after %d iteration(s); summarising progress to the spec…", m.loopTotal)))
	return goal.WrapUpPrompt(m.loopSpecPath)
}

// appendMergeHint tells the user where the loop's work landed and how to merge
// it (only when the loop actually moved onto a branch).
func (m *Model) appendMergeHint() {
	if m.loopBranch != "" {
		m.appendLine(toolStyle.Render("  " + goal.MergeHint(m.loopBranch, m.loopBaseBranch)))
	}
}
