package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/spf13/cobra"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/goal"
	"github.com/greenthread-ai/klaudia/internal/permission"
)

// loopStallLimit stops the loop after this many consecutive iterations make no
// new git commit — a sign the agent is spinning without making progress.
const loopStallLimit = 3

// loopRun bundles the already-built run state the goal loop reuses (set up once
// in run() like the single-shot headless path).
type loopRun struct {
	loop       *agent.Loop
	cwd        string
	mode       permission.Mode
	model      anthropic.Model
	system     string
	maxTurns   int
	iterations int
	permCtx    permission.Context
	hostGate   *agent.HostGate
	approver   agent.Approver
	deferred   map[string]bool
	recorder   agent.Recorder
	onSummary  func(string)
	render     *Renderer
}

// runGoalLoop drives the headless Ralph loop: it iterates against the goal spec,
// each iteration re-orienting from the spec + git with a fresh context (progress
// lives in files and commits, not the conversation window), until the model
// reports completion, the iteration cap is hit, or it stalls (no new commits).
func runGoalLoop(ctx context.Context, cmd *cobra.Command, p loopRun) error {
	specText, specPath, _ := goal.Read(p.cwd)
	if strings.TrimSpace(specText) == "" {
		return fmt.Errorf("no goal spec found — create PRD.md or .klaudia/GOAL.md first (run klaudia interactively and use /goal)")
	}
	// No human is present to approve tool use, so the loop can only do real work
	// in a mode that does not ask. Autonomous qualifies now that the host gate
	// is what stops a host change: before it existed, the only way to run
	// unattended was --dangerously-skip-permissions, which turned off every
	// check to get past prompts about editing files in the project.
	if p.mode != permission.ModeAutonomous && p.mode != permission.ModeBypassPermissions {
		return fmt.Errorf("--loop runs unattended and cannot answer prompts; use --permission-mode autonomous "+
			"(host changes still need --allow-host-changes) or --dangerously-skip-permissions. Current mode: %s", p.mode)
	}

	iters := goal.Iterations(p.iterations)
	errOut := cmd.ErrOrStderr()

	branch := goal.BranchName(specText)
	base := gitBranch(p.cwd) // merge target (the branch we start from)
	if base == branch {
		base = "" // resuming on the goal branch already; base unknown
	}
	onBranch := false
	if out, gerr := gitCheckoutBranch(p.cwd, branch); gerr != nil {
		fmt.Fprintf(errOut, "warning: not branching (%s)\n", strings.TrimSpace(out))
	} else {
		onBranch = true
		fmt.Fprintf(errOut, "↳ on branch %s\n", branch)
	}
	mergeHint := func() {
		if onBranch {
			fmt.Fprintln(errOut, goal.MergeHint(branch, base))
		}
	}

	emit := func(ev agent.Event) { _ = p.render.Event(ev) }
	// Each turn starts fresh (no InitialMessages): it re-reads the spec and git
	// state, per the Ralph principle (bounded context over long runs).
	runTurn := func(prompt string) (agent.Result, error) {
		return p.loop.Run(ctx, agent.Options{
			WorkingDir:    p.cwd,
			Prompt:        prompt,
			Model:         p.model,
			System:        p.system,
			MaxTurns:      p.maxTurns,
			Permission:    p.permCtx,
			Host:          p.hostGate,
			Approver:      p.approver,
			DeferredTools: p.deferred,
			Recorder:      p.recorder,
			WebTools:      true,
			OnSummary:     p.onSummary,
		}, emit)
	}

	lastCommit := gitCommit(p.cwd)
	stalls := 0
	for i := 1; i <= iters; i++ {
		fmt.Fprintf(errOut, "↻ iteration %d/%d\n", i, iters)
		res, err := runTurn(goal.IterationPrompt(specPath))
		if err != nil {
			return err
		}
		if goal.IsComplete(res.Text) {
			fmt.Fprintf(errOut, "✓ goal complete in %d iteration(s)\n", i)
			mergeHint()
			return nil
		}
		// Stall detection: a non-repo (commit == "") never stalls.
		if c := gitCommit(p.cwd); c == "" || c != lastCommit {
			stalls, lastCommit = 0, c
		} else if stalls++; stalls >= loopStallLimit {
			fmt.Fprintf(errOut, "⊘ no new commits for %d iterations — stopping (goal not complete).\n", stalls)
			break
		}
	}

	// Stopped without completing (cap or stall): one wrap-up turn records an
	// end-of-run summary in the spec so the next run (or a person) can resume.
	fmt.Fprintf(errOut, "summarising progress to %s…\n", specPath)
	if _, err := runTurn(goal.WrapUpPrompt(specPath)); err != nil {
		return err
	}
	fmt.Fprintf(errOut, "stopped; goal not yet complete. Progress recorded in %s — re-run to resume.\n", specPath)
	mergeHint()
	return nil
}

// gitRun runs a git command in dir and returns its combined output.
func gitRun(dir string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	return string(out), err
}

// gitCheckoutBranch switches to branch, reusing it if it already exists (so a
// resumed loop continues from earlier commits) or creating it otherwise.
func gitCheckoutBranch(dir, branch string) (string, error) {
	if _, err := gitRun(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return gitRun(dir, "checkout", branch)
	}
	return gitRun(dir, "checkout", "-b", branch)
}
