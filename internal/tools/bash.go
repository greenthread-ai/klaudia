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
