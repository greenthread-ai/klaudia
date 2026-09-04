package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/greenthread-ai/klaudia/internal/native/bashparser"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/sandbox"
	"github.com/greenthread-ai/klaudia/internal/schema"
)

// bashDefaultTimeout is applied when the model doesn't specify one.
const bashDefaultTimeout = 2 * time.Minute

// BashInput is the Bash tool's input.
type BashInput struct {
	Command         string `json:"command" jsonschema:"description=The shell command to execute"`
	Description     string `json:"description,omitempty" jsonschema:"description=A short description of what the command does"`
	Timeout         int    `json:"timeout,omitempty" jsonschema:"description=Timeout in milliseconds (default 120000)"`
	RunInBackground bool   `json:"run_in_background,omitempty" jsonschema:"description=Run detached and return a shell id immediately; read its output with BashOutput and stop it with KillShell"`
}

// Bash executes shell commands via a sandbox.Executor. When run_in_background is
// set, it launches a managed job tracked by the (optional) JobStore.
type Bash struct {
	schema   *schema.Schema
	executor sandbox.Executor
	shells   *JobStore
}

// NewBash constructs the Bash tool with the given executor. The optional
// JobStore backs run_in_background (omit it to disable background jobs).
func NewBash(executor sandbox.Executor, shells ...*JobStore) (*Bash, error) {
	s, err := schema.For[BashInput]()
	if err != nil {
		return nil, fmt.Errorf("bash: build schema: %w", err)
	}
	b := &Bash{schema: s, executor: executor}
	if len(shells) > 0 {
		b.shells = shells[0]
	}
	return b, nil
}

func (b *Bash) Name() string { return "Bash" }

func (b *Bash) Description(context.Context) (string, error) {
	return "Executes a shell command and returns its combined output. Commands run via bash. " +
		"Provide an optional timeout in milliseconds (default 120000, max 600000). " +
		"Prefer the Read/Glob/Grep tools over cat/find/grep where possible.", nil
}

func (b *Bash) InputSchema() json.RawMessage { return b.schema.Raw }

func (b *Bash) ValidateInput(raw json.RawMessage) error {
	if err := b.schema.Validate(raw); err != nil {
		return err
	}
	var in BashInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Command) == "" {
		return fmt.Errorf("command must not be empty")
	}
	return nil
}

// PermissionRequest derives a rule specifier from the command via the bash
// parser (e.g. "git status"), falling back to the raw command on parse error.
func (b *Bash) PermissionRequest(raw json.RawMessage) permission.PermissionRequest {
	var in BashInput
	_ = json.Unmarshal(raw, &in)
	spec := in.Command
	if a, err := bashparser.Parse(in.Command); err == nil {
		if p := a.Prefix(); p != "" {
			spec = p
		}
	}
	return permission.PermissionRequest{Specifier: spec}
}

// CheckPermissions: Bash is a command-executing (exec-class) tool.
func (b *Bash) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return execClassDecision(pctx)
}

func (b *Bash) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in BashInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}

	// Refuse a command that needs a terminal before launching it. Without this
	// the child sits waiting for a keypress that cannot arrive and the turn
	// stalls until the timeout, reporting nothing useful about why.
	if reason, blocked := sandbox.TTYRequired(in.Command); blocked {
		return []Result{{Content: reason, IsError: true}}, nil
	}

	// A service the model backgrounds with `&` itself skips the whole job
	// system: no name, no managed log, no crash detection, no restart. Observed
	// in the agent-loop torture test — the model shell-backgrounded a dev
	// server eleven times and then managed the processes by hand with pkill and
	// `kill -9 $(lsof -ti:PORT)`, which is exactly the work managed jobs exist
	// to remove.
	//
	// The timeout nudge cannot catch this: a self-backgrounded command returns
	// immediately, so there is no timeout. It has to be caught on the way in.
	if !in.RunInBackground {
		if reason, blocked := selfBackgrounded(in.Command); blocked {
			return []Result{{Content: reason, IsError: true}}, nil
		}
	}

	// Background: launch detached, return a shell id immediately.
	if in.RunInBackground {
		if b.shells == nil {
			return []Result{{Content: "background execution is not available", IsError: true}}, nil
		}
		res, err := b.shells.Start(b.executor, sandbox.Request{Command: in.Command, WorkingDir: tctx.WorkingDir})
		if err != nil {
			return []Result{{Content: fmt.Sprintf("Failed to start background command: %v", err), IsError: true}}, nil
		}
		j := res.Job
		if res.Duplicate {
			// Two dev servers fighting over one port is a confusing failure, and
			// the loser usually looks like broken code. Hand back the one that
			// is already up instead.
			return []Result{{Content: fmt.Sprintf(
				"That command is already running as job %s (%s), started %s ago. "+
					"Reusing it rather than starting a second copy — read its output with "+
					"BashOutput(bash_id=%q), or restart it with RestartJob(job=%q).",
				j.Name, j.ID, fmtShortDuration(time.Since(j.Started)), j.Name, j.Name)}}, nil
		}
		return []Result{{Content: fmt.Sprintf(
			"Started job %s (%s). Read its output with BashOutput(bash_id=%q), restart it with "+
				"RestartJob(job=%q), stop it with KillShell(shell_id=%q).",
			j.Name, j.ID, j.Name, j.Name, j.Name)}}, nil
	}

	timeout := bashDefaultTimeout
	if in.Timeout > 0 {
		timeout = min(time.Duration(in.Timeout)*time.Millisecond, 10*time.Minute)
	}

	resp, err := b.executor.Run(ctx, sandbox.Request{
		Command:    in.Command,
		WorkingDir: tctx.WorkingDir,
		Timeout:    timeout,
	})
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Failed to run command: %v", err), IsError: true}}, nil
	}

	model, full := formatBashOutput(resp, in.Command)
	return []Result{{Content: model, Full: full, IsError: resp.ExitCode != 0}}, nil
}

// selfBackgrounded reports whether a command detaches a long-running service
// with the shell rather than asking for a job, and what to do instead.
//
// Deliberately narrow: it fires only when the command *both* backgrounds
// something *and* looks like a service. `sleep 1 &` in a test script is nobody's
// business; `go run ./cmd/api &` is a dev server that should have a name, a log
// and a way to be restarted.
func selfBackgrounded(command string) (reason string, blocked bool) {
	if !looksLongRunning(command) || !hasBackgroundOperator(command) {
		return "", false
	}
	return "This backgrounds a long-running process with the shell, which leaves it untracked: " +
		"no name, no log Klaudia can page, no notice when it dies, and no way to restart it — " +
		"you would have to hunt it down with ps and kill.\n\n" +
		"Start it as a managed job instead: run the server on its own with Bash's " +
		"run_in_background parameter, then run your checks as a separate command. " +
		"Read its output with BashOutput, restart it with RestartJob, stop it with KillShell, " +
		"and check Jobs first in case it is already up.\n\n" +
		"If this really is short-lived, just run it in the foreground.", true
}

// looksLongRunning is looksLikeService plus the shapes that are only ever
// backgrounded because they do not return.
//
// Broader than the timeout nudge on purpose. There, a false positive adds an
// unhelpful sentence to a failure; here it costs one retry, and the guidance
// says exactly what to do — so erring toward catching `go run ./cmd/api &` is
// worth the occasional `go run ./cmd/oneshot &` being told to pick a lane.
func looksLongRunning(command string) bool {
	if looksLikeService(command) {
		return true
	}
	c := strings.ToLower(command)
	for _, h := range []string{
		"go run ", "cargo run", "dotnet run", "bootrun",
		"rails s", "flask ", "php -s", "caddy run", "nginx",
	} {
		if strings.Contains(c, h) {
			return true
		}
	}
	return false
}

// hasBackgroundOperator finds a shell `&` that detaches, ignoring `&&`, `>&`
// and `2>&1`.
func hasBackgroundOperator(command string) bool {
	if strings.Contains(command, "nohup ") || strings.Contains(command, "setsid ") {
		return true
	}
	inSingle, inDouble := false, false
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '&' && !inSingle && !inDouble:
			if i+1 < len(command) && command[i+1] == '&' {
				i++ // logical AND
				continue
			}
			if i > 0 && (command[i-1] == '&' || command[i-1] == '>' || command[i-1] == '<') {
				continue // the tail of &&, or a >& / <& redirection
			}
			// A digit before it is a fd redirection like 2>&1, already covered
			// by the '>' case above. Anything else detaches.
			return true
		}
	}
	return false
}

// serviceHints are the shapes of a command that does not intend to finish.
// Matched loosely and only used to add a suggestion to a timeout, so a false
// positive costs one unhelpful sentence rather than a wrong decision.
var serviceHints = []string{
	"run dev", "run start", "run serve", "run watch", "start:dev",
	"npm start", "yarn start", "pnpm start", "bun run dev",
	"compose up", "docker run", "serve", "http.server", "runserver",
	"tail -f", "watch ", "nodemon", "vite", "webpack-dev-server",
	"rails s", "flask run", "uvicorn", "gunicorn", "air", "reflex",
	"ng serve", "next dev", "nuxt dev", "remix dev", "astro dev",
	"make dev", "make run", "make serve", "cargo watch", "mvn spring-boot:run",
}

func looksLikeService(command string) bool {
	c := strings.ToLower(command)
	// A pipeline that ends somewhere is not a service, even if it starts with
	// one: `tail -f log | head -20` terminates.
	if strings.Contains(c, "|") {
		return false
	}
	for _, h := range serviceHints {
		if strings.Contains(c, h) {
			return true
		}
	}
	return false
}

// formatBashOutput combines stdout/stderr and annotates non-zero exit / timeout.
// It returns two strings: the model-facing text, clamped to bashMaxOutput (see
// output.go for why it keeps a tail as well as a head), and the untruncated
// text for local display. full is empty when nothing was clamped.
func formatBashOutput(resp sandbox.Response, command string) (model, full string) {
	var b strings.Builder
	b.WriteString(resp.Stdout)
	if resp.Stderr != "" {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteString("\n")
		}
		b.WriteString(resp.Stderr)
	}
	raw := b.String()
	out := raw
	if clamped, elided := clampOutput(raw); elided > 0 {
		out = clamped
		// The spill file is the *model's* escape hatch to the elided middle —
		// it can Read or grep the path. The UI doesn't need it: `full` carries
		// the same text in memory.
		if path, ok := spillOutput(raw); ok {
			out += "\n" + spillMarker + path + "]"
		}
		full = raw
	}

	// Status annotations belong on both variants — a reader of the full output
	// still needs to know the command failed.
	var status string
	if resp.TimedOut {
		status = fmt.Sprintf("\n[command timed out, exit code %d]", resp.ExitCode)
		// A bare exit 124 reads as "the command is broken". Usually it means the
		// command has no natural end, and the right move is to make it a job —
		// which keeps it running, gives it a name and a log, and lets the turn
		// continue. Saying so here is the difference between the model
		// retrying with a longer timeout and it doing the right thing.
		if looksLikeService(command) {
			status += "\n[this looks like a long-running service. Start it with Bash run_in_background " +
				"to make it a managed job: it keeps running, gets a name and a log, and you can carry on. " +
				"Check Jobs first — it may already be up.]"
		}
	} else if resp.Canceled {
		// Say plainly what happened, because the alternative is the model
		// inferring a cause. Whatever the command had already done still
		// happened, so the next move is to check the state rather than assume
		// the work failed or re-run it blindly.
		status = "\n[interrupted by the user before it finished — this was not a timeout and not a " +
			"failure of the command. Anything it had already done still took effect; check the " +
			"current state before re-running it.]"
	} else if resp.ExitCode != 0 {
		status = fmt.Sprintf("\n[exit code %d]", resp.ExitCode)
	}
	out += status
	if full != "" {
		full += status
	}

	if out == "" {
		return "[no output]", ""
	}
	return out, full
}
