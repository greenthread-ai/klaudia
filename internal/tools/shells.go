package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/schema"
	"github.com/greenthread-ai/klaudia/pkg/tool"
)

// --- BashOutput tool ---

type BashOutputInput struct {
	BashID string `json:"bash_id" jsonschema:"description=The background job's id (e.g. bash_1) or name (e.g. dev)"`
	Filter string `json:"filter,omitempty" jsonschema:"description=Optional regex; only matching output lines are returned"`
	// WaitSeconds lets one call wait out a long job instead of polling it.
	WaitSeconds int `json:"wait_seconds,omitempty" jsonschema:"description=Wait up to this many seconds (max 600) for the job to exit or for output matching filter before returning"`
}

// maxBashOutputWait caps wait_seconds at Bash's own longest timeout.
const maxBashOutputWait = 600

type BashOutput struct {
	schema *schema.Schema
	shells *JobStore
}

func NewBashOutput(shells *JobStore) (*BashOutput, error) {
	s, err := schema.For[BashOutputInput]()
	if err != nil {
		return nil, fmt.Errorf("bashoutput: build schema: %w", err)
	}
	return &BashOutput{schema: s, shells: shells}, nil
}

func (t *BashOutput) Name() string { return "BashOutput" }

func (t *BashOutput) Description(context.Context) (string, error) {
	return "Read new output from a background job started by Bash with run_in_background. " +
		"Accepts the job's id (bash_1) or its name (dev, api). Returns only output produced " +
		"since the last read, plus whether the job is still running, how long it has run and how " +
		"long since it last printed anything. For a long job (provisioning a VM, a rollout), pass " +
		"wait_seconds (max 600) to wait for it to exit — or, with filter, for a matching line — in " +
		"one call instead of polling. You are also told when a background job exits, so you can " +
		"end your turn and be woken by it.", nil
}

func (t *BashOutput) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *BashOutput) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in BashOutputInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.BashID) == "" {
		return fmt.Errorf("bash_id is required")
	}
	if in.Filter != "" {
		if _, err := regexp.Compile(in.Filter); err != nil {
			return fmt.Errorf("invalid filter regex: %w", err)
		}
	}
	if in.WaitSeconds < 0 || in.WaitSeconds > maxBashOutputWait {
		return fmt.Errorf("wait_seconds must be between 0 and %d", maxBashOutputWait)
	}
	return nil
}

func (t *BashOutput) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (t *BashOutput) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx) // reading output of a shell we already started
}

func (t *BashOutput) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in BashOutputInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if t.shells == nil {
		return []Result{{Content: "background jobs are not available", IsError: true}}, nil
	}
	waited := ""
	if in.WaitSeconds > 0 {
		var match *regexp.Regexp
		if in.Filter != "" {
			match = regexp.MustCompile(in.Filter) // validated above
		}
		why, ok := t.shells.Wait(ctx, in.BashID, match, time.Duration(in.WaitSeconds)*time.Second)
		if !ok {
			return []Result{{Content: t.shells.unknownJobMsg(in.BashID), IsError: true}}, nil
		}
		waited = why
	}
	out, ok := t.shells.Read(in.BashID)
	if !ok {
		return []Result{{Content: t.shells.unknownJobMsg(in.BashID), IsError: true}}, nil
	}
	body := out.Output
	if in.Filter != "" {
		body = filterLines(body, in.Filter)
	}
	model, full := clampJobRead(body, out, tctx.Spill)
	status := "\n" + jobStateLine(out, time.Now())
	if waited == "timed out" {
		status += fmt.Sprintf(" — waited %ds; still going", in.WaitSeconds)
	}
	if full != "" {
		full += status
	}
	return []Result{{Content: model + status, Full: full}}, nil
}

// clampJobRead trims one BashOutput read to the per-tool budget, keeping more
// of the newest output (tool.ClampKeepingTail) than the loop's head-heavy
// backstop would: what a job printed last is what the model is asking about.
// It returns the text for the model and, when something was cut, the
// untrimmed read for local display.
//
// The cut loses nothing. The notice names the job's own log, which already
// holds the text and survives a restart; only a memory-only log is spilled to
// a new file. (From PR #65.)
func clampJobRead(body string, out ShellOutput, spill SpillStore) (model, full string) {
	if body == "" {
		return "[no new output]", ""
	}
	clamped, elided := tool.ClampKeepingTail(body)
	if elided == 0 {
		return body, ""
	}
	if out.LogPath != "" {
		clamped += "\n[the whole of this read is in the job's log: " + out.LogPath + "]"
	} else if path, ok := spill.Spill("BashOutput", body); ok {
		clamped += "\n" + tool.SpillMarker + path + "]"
	}
	return clamped, body
}

// jobStateLine is BashOutput's last line: running or exited, for how long,
// and — while running — how long since it last printed. Quiet is reported,
// not judged: a provisioning step can say nothing for twenty minutes and be
// fine, and the model is better placed than a threshold to know which it is.
func jobStateLine(out ShellOutput, now time.Time) string {
	if !out.Running {
		return fmt.Sprintf("[job %s exited, code %d]", out.Name, out.ExitCode)
	}
	s := fmt.Sprintf("[job %s running for %s", out.Name, roundAge(now.Sub(out.Started)))
	switch {
	case out.LastOutput.IsZero():
		s += ", no output yet"
	case now.Sub(out.LastOutput) >= time.Minute:
		s += ", last output " + roundAge(now.Sub(out.LastOutput)) + " ago"
	}
	return s + "]"
}

// roundAge renders a duration to the unit that matters: 42s, 7m, 3h12m.
func roundAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return d.Round(time.Second).String()
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// filterLines keeps only lines matching the (already-validated) regex.
func filterLines(s, pattern string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return s
	}
	var kept []string
	for _, ln := range strings.Split(s, "\n") {
		if re.MatchString(ln) {
			kept = append(kept, ln)
		}
	}
	return strings.Join(kept, "\n")
}

// --- KillShell tool ---

type KillShellInput struct {
	ShellID string `json:"shell_id" jsonschema:"description=The background job's id (e.g. bash_1) or name (e.g. dev) to stop"`
}

type KillShell struct {
	schema *schema.Schema
	shells *JobStore
}

func NewKillShell(shells *JobStore) (*KillShell, error) {
	s, err := schema.For[KillShellInput]()
	if err != nil {
		return nil, fmt.Errorf("killshell: build schema: %w", err)
	}
	return &KillShell{schema: s, shells: shells}, nil
}

func (t *KillShell) Name() string { return "KillShell" }

func (t *KillShell) Description(context.Context) (string, error) {
	return "Stop a background job started by Bash with run_in_background, by id or name. " +
		"Stops the whole process group, so a wrapper script's children go too.", nil
}

func (t *KillShell) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *KillShell) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in KillShellInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.ShellID) == "" {
		return fmt.Errorf("shell_id is required")
	}
	return nil
}

func (t *KillShell) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (t *KillShell) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx) // stopping a shell we already started
}

func (t *KillShell) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in KillShellInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if t.shells == nil {
		return []Result{{Content: "background jobs are not available", IsError: true}}, nil
	}
	if !t.shells.Kill(in.ShellID) {
		return []Result{{Content: t.shells.unknownJobMsg(in.ShellID), IsError: true}}, nil
	}
	return []Result{{Content: fmt.Sprintf("Stopped job %s.", in.ShellID)}}, nil
}
