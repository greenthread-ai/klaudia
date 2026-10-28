package tui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/greenthread-ai/klaudia/internal/goal"
	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/plan"
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
	m.loopPlan = false
	m.loopTotal, m.loopRemaining, m.loopSpecPath = n, n, specPath
	m.refreshGoalList()
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
			if m.loopPlan {
				m.appendLine(toolStyle.Render("  progress recorded in the plan — /plan run to resume."))
			} else {
				m.appendLine(toolStyle.Render(fmt.Sprintf("  summary written to %s — /goal run to resume.", filepath.Base(m.loopSpecPath))))
			}
		}
		m.appendMergeHint()
		return ""
	}
	if err != nil {
		m.loopRemaining = 0
		m.appendLine(toolStyle.Render("  ⊘ " + m.loopNoun() + " halted."))
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

	// A plan run is also done when every step is: the steps are what the user
	// approved, and the model may finish them without saying the token.
	claimed := goal.IsComplete(res.Text) || (m.loopPlan && len(m.plan.Items) > 0 && m.plan.Remaining() == 0)
	if claimed {
		// Mechanical gate first.
		if n, cerr := m.loopUnchecked(); cerr == nil && n > 0 {
			m.appendLine(toolStyle.Render(fmt.Sprintf("  ✗ completion claim rejected: %s still unchecked in %s", countOf(n, "item"), m.loopSourceName())))
			m.loopVerifying = false
			return m.continueIteration()
		}
		// If we just ran the verification turn and it also says complete, exit.
		if m.loopVerifying {
			m.loopVerifying = false
			done := m.loopTotal - m.loopRemaining + 1
			m.loopRemaining = 0
			m.appendLine(bannerStyle.Render(fmt.Sprintf("  ✓ %s complete (verified) in %s.", m.loopKind(), countOf(done, m.loopUnit()))))
			m.appendMergeHint()
			return ""
		}
		// Mechanical passed but verification hasn't fired yet — fire it once.
		// Doesn't decrement the budget; it's an insurance check, not new work.
		m.loopVerifying = true
		m.appendLine(toolStyle.Render("  completion claimed; running final verification…"))
		if m.loopPlan {
			return goal.PlanVerificationPrompt(m.plan.Path)
		}
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
	if m.loopPlan && m.plan.Path == "" {
		// The store had nowhere to write the plan, so there is no file for the
		// next turn to read; carrying on would ask the model to continue a plan
		// it can only remember.
		m.loopRemaining = 0
		m.appendLine(errStyle.Render("  ⊘ plan run halted: the plan was not saved, so there is nothing to resume from."))
		return ""
	}
	if m.loopRemaining > 0 {
		next := m.loopTotal - m.loopRemaining + 1
		m.appendLine(toolStyle.Render(fmt.Sprintf("  ↻ %s %d/%d", m.loopUnit(), next, m.loopTotal)))
		if m.loopPlan {
			return goal.PlanIterationPrompt(m.plan.Path)
		}
		return goal.IterationPrompt(m.loopSpecPath)
	}
	m.loopWrapUp = true
	if m.loopPlan {
		m.appendLine(toolStyle.Render(fmt.Sprintf("  stopped after %s; recording progress in the plan…", countOf(m.loopTotal, "turn"))))
		return goal.PlanWrapUpPrompt(m.plan.Path)
	}
	m.appendLine(toolStyle.Render(fmt.Sprintf("  stopped after %s; summarising progress to the spec…", countOf(m.loopTotal, "iteration"))))
	return goal.WrapUpPrompt(m.loopSpecPath)
}

// appendMergeHint tells the user where the loop's work landed and how to merge
// it (only when the loop actually moved onto a branch).
func (m *Model) appendMergeHint() {
	if m.loopBranch != "" {
		m.appendLine(toolStyle.Render("  " + goal.MergeHint(m.loopBranch, m.loopBaseBranch)))
	}
}

// startPlanRun turns the session's approved plan into a run: the goal loop with
// the plan as its spec (see goal.PlanIterationPrompt). fromApproval means the
// turn already running — the one the plan was approved in — is the run's
// first turn, so its changes belong in the run's completion block.
func (m *Model) startPlanRun(n int, fromApproval bool) {
	m.loopPlan = true
	m.goalSetting = false
	m.loopBranch, m.loopBaseBranch = "", "" // a plan is approved on the user's branch
	m.loopTotal, m.loopRemaining, m.loopSpecPath = n, n, ""
	m.loopStubFixing, m.loopVerifying, m.loopWrapUp = false, false, false
	m.loopSpanOpen = fromApproval
}

// runPlan is /plan run [N]: resume the approved plan when it has steps left.
//
// A step list the model kept with TodoWrite, never approved through
// ExitPlanMode, is in the same panel under the same title; asking to run it
// approves it. It used to be refused with "No approved plan", the panel
// showing seven steps beneath the message.
func (m *Model) runPlan(args []string) (tea.Model, tea.Cmd) {
	if !m.plan.Approved && m.plan.Remaining() > 0 && m.sess.ApprovePlan != nil {
		snap, err := m.sess.ApprovePlan()
		if err != nil {
			m.appendLine(errStyle.Render("Could not save the plan: " + err.Error()))
			return m, nil
		}
		m.plan = snap
	}
	switch {
	case !m.plan.Approved:
		m.appendLine(errStyle.Render("No plan this session. /plan to start one."))
		return m, nil
	case m.plan.Path == "":
		m.appendLine(errStyle.Render("The approved plan was not saved, so there is nothing to resume from."))
		return m, nil
	case len(m.plan.Items) == 0 || m.plan.Remaining() == 0:
		m.appendLine(bannerStyle.Render("Every step of the approved plan is done."))
		return m, nil
	}
	n := goal.DefaultIterations
	if len(args) > 0 {
		v, err := strconv.Atoi(args[0])
		if err != nil || v <= 0 {
			m.appendLine(errStyle.Render("usage: /plan run [N]  (N = max turns, a positive integer)"))
			return m, nil
		}
		n = min(v, goal.MaxIterations)
	}
	m.startPlanRun(n, false)
	m.appendLine(bannerStyle.Render(fmt.Sprintf("Plan run: %s left, up to %s. Esc or /plan stop to halt.", countOf(m.plan.Remaining(), "step"), countOf(n, "turn"))))
	m.appendLine(toolStyle.Render(fmt.Sprintf("  ↻ turn 1/%d", n)))
	m.setState(stateRunning)
	return m, m.startTurn(goal.PlanIterationPrompt(m.plan.Path))
}

// stopLoop is /goal stop and /plan stop: either kind of run.
func (m *Model) stopLoop() {
	if m.loopRemaining > 0 || m.loopWrapUp || m.turnCancel != nil {
		noun := m.loopNoun()
		m.loopRemaining, m.loopWrapUp, m.loopStubFixing, m.loopVerifying = 0, false, false, false
		m.loopPlan = false
		if m.turnCancel != nil {
			m.turnCancel()
			m.turnCancel = nil
		}
		m.appendLine(toolStyle.Render("  ⊘ " + noun + " stopped."))
		return
	}
	m.appendLine(bannerStyle.Render("Nothing is running."))
}

// loopKind, loopNoun and loopUnit name the run for messages: a goal loop
// counts iterations, a plan run counts turns.
func (m *Model) loopKind() string {
	if m.loopPlan {
		return "plan"
	}
	return "goal"
}

func (m *Model) loopNoun() string {
	if m.loopPlan {
		return "plan run"
	}
	return "goal loop"
}

func (m *Model) loopUnit() string {
	if m.loopPlan {
		return "turn"
	}
	return "iteration"
}

// loopUnchecked is the mechanical gate's count: open steps of the plan, or
// unchecked boxes in the goal spec.
func (m *Model) loopUnchecked() (int, error) {
	if m.loopPlan {
		return m.plan.Remaining(), nil
	}
	return goal.CountUnchecked(m.loopSpecPath)
}

func (m *Model) loopSourceName() string {
	if m.loopPlan {
		return "the plan"
	}
	return filepath.Base(m.loopSpecPath)
}

// refreshGoalList rereads a goal run's checklist for the panel. Called when
// the run starts and after each tool result, since that is when the model
// can have ticked a box.
func (m *Model) refreshGoalList() {
	if m.loopSpecPath == "" {
		m.goalList = plan.Snapshot{}
		return
	}
	items, err := goal.Checklist(m.loopSpecPath)
	if err != nil {
		return // keep the last good list rather than blanking the panel
	}
	snap := plan.Snapshot{Items: make([]plan.Item, 0, len(items))}
	for _, it := range items {
		st := plan.StatusPending
		if it.Done {
			st = plan.StatusCompleted
		}
		snap.Items = append(snap.Items, plan.Item{Content: it.Text, Status: st})
	}
	m.goalList = snap
}

// workList is what the live panel shows: during a goal run the spec's
// checklist, otherwise the plan — each titled, and a run's count of turns or
// iterations alongside. One panel for both, so a goal run never sits under a
// stale plan.
func (m *Model) workList() (snap plan.Snapshot, title, suffix string) {
	running := m.loopRemaining > 0 || m.loopWrapUp
	if running {
		suffix = fmt.Sprintf(" · %s %d/%d", m.loopUnit(), m.loopTotal-m.loopRemaining+1, m.loopTotal)
		if m.loopWrapUp {
			suffix = " · wrapping up"
		}
	}
	if running && !m.loopPlan {
		return m.goalList, "Goal", suffix
	}
	return m.plan, "Plan", suffix
}
