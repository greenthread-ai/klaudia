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

// bashMaxOutput caps combined output length to protect the context window.
const bashMaxOutput = 30000

// BashInput is the Bash tool's input.
type BashInput struct {
	Command         string `json:"command" jsonschema:"description=The shell command to execute"`
	Description     string `json:"description,omitempty" jsonschema:"description=A short description of what the command does"`
	Timeout         int    `json:"timeout,omitempty" jsonschema:"description=Timeout in milliseconds (default 120000)"`
	RunInBackground bool   `json:"run_in_background,omitempty" jsonschema:"description=Run detached and return a shell id immediately; read its output with BashOutput and stop it with KillShell"`
}

// Bash executes shell commands via a sandbox.Executor. When run_in_background is
// set, it launches a detached shell tracked by the (optional) ShellStore.
type Bash struct {
	schema   *schema.Schema
	executor sandbox.Executor
	shells   *ShellStore
}

// NewBash constructs the Bash tool with the given executor. The optional
// ShellStore backs run_in_background (omit it to disable background shells).
func NewBash(executor sandbox.Executor, shells ...*ShellStore) (*Bash, error) {
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

	// Background: launch detached, return a shell id immediately.
	if in.RunInBackground {
		if b.shells == nil {
			return []Result{{Content: "background execution is not available", IsError: true}}, nil
		}
		id, err := b.shells.Start(b.executor, sandbox.Request{Command: in.Command, WorkingDir: tctx.WorkingDir})
		if err != nil {
			return []Result{{Content: fmt.Sprintf("Failed to start background command: %v", err), IsError: true}}, nil
		}
		return []Result{{Content: fmt.Sprintf("Started background shell %s. Read its output with BashOutput(bash_id=%q) and stop it with KillShell(shell_id=%q).", id, id, id)}}, nil
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

	return []Result{{Content: formatBashOutput(resp), IsError: resp.ExitCode != 0}}, nil
}

// formatBashOutput combines stdout/stderr and annotates non-zero exit / timeout,
// truncating to bashMaxOutput.
func formatBashOutput(resp sandbox.Response) string {
	var b strings.Builder
	b.WriteString(resp.Stdout)
	if resp.Stderr != "" {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteString("\n")
		}
		b.WriteString(resp.Stderr)
	}
	out := b.String()
	if len(out) > bashMaxOutput {
		out = out[:bashMaxOutput] + "\n... [output truncated]"
	}
	if resp.TimedOut {
		out += fmt.Sprintf("\n[command timed out, exit code %d]", resp.ExitCode)
	} else if resp.ExitCode != 0 {
		out += fmt.Sprintf("\n[exit code %d]", resp.ExitCode)
	}
	if out == "" {
		return "[no output]"
	}
	return out
}
