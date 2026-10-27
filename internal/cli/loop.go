package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/goal"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// loopStallLimit stops the loop after this many consecutive iterations make no
// new git commit — a sign the agent is spinning without making progress.
const loopStallLimit = 3

// loopRun bundles what the goal loop needs beyond the shared run closure. The
// closure (built in run(), like every other frontend's) carries the model,
// system prompt, tools and permission context, so this holds only what the loop
// itself reasons about: where the project is, that the mode can run unattended,
// how many iterations are allowed, and where output goes.
type loopRun struct {
	run        agent.RunFunc
	approver   agent.Approver
	cwd        string
	mode       permission.Mode
	iterations int
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
	// unattended was bypassPermissions, which turned off every check to get
	// past prompts about editing files in the project.
	if p.mode != permission.ModeAutonomous && p.mode != permission.ModeBypassPermissions {
		return fmt.Errorf("--loop runs unattended and cannot answer prompts; use --permission-mode autonomous "+
			"(host changes still need --allow-host-changes) or bypassPermissions. Current mode: %s", p.mode)
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

	notices := noticeWriter(errOut)
	emit := func(ev agent.Event) {
		notices(ev)
		_ = p.render.Event(ev)
	}
	// Each turn starts fresh (no History): it re-reads the spec and git state,
	// per the Ralph principle (bounded context over long runs).
	runTurn := func(prompt string) (agent.Result, error) {
		return p.run(ctx, agent.Turn{
			Prompt:   prompt,
			Emit:     emit,
			Approver: p.approver,
		})
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
