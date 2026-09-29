package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/schema"
)

// --- BashOutput tool ---

type BashOutputInput struct {
	BashID string `json:"bash_id" jsonschema:"description=The background job's id (e.g. bash_1) or name (e.g. dev)"`
	Filter string `json:"filter,omitempty" jsonschema:"description=Optional regex; only matching output lines are returned"`
}

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
		"since the last read, plus whether the job is still running. A long read keeps its " +
		"start and its most recent output and names the job's log file for the rest.", nil
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
	return nil
}

func (t *BashOutput) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (t *BashOutput) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx) // reading output of a shell we already started
}

func (t *BashOutput) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in BashOutputInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if t.shells == nil {
		return []Result{{Content: "background jobs are not available", IsError: true}}, nil
	}
	out, ok := t.shells.Read(in.BashID)
	if !ok {
		return []Result{{Content: t.shells.unknownJobMsg(in.BashID), IsError: true}}, nil
	}
	body := out.Output
	if in.Filter != "" {
		body = filterLines(body, in.Filter)
	}
	model, full := clampJobRead(body, out)
	var status string
	if out.Running {
		status = fmt.Sprintf("\n[job %s running]", out.Name)
	} else {
		status = fmt.Sprintf("\n[job %s exited, code %d]", out.Name, out.ExitCode)
	}
	if full != "" {
		full += status
	}
	return []Result{{Content: model + status, Full: full}}, nil
}

// A background job's read is capped at the same total as Bash output, but split
// the other way: what the job printed last is what the model is asking about
// (the error after a reload, the request that just failed), so the tail gets
// two thirds and the head keeps only the start of what came since the last read.
const (
	jobReadHeadBytes = 10000
	jobReadTailBytes = bashMaxOutput - jobReadHeadBytes
)

// clampJobRead trims one BashOutput read to the cap. It returns the text for
// the model and, when something was cut, the untrimmed text for local display.
//
// Without it, a read returned everything written since the previous one, so a
// chatty dev server left alone for a few turns could flood the context in one
// call. The cut is never a loss: the notice names the job's own log file and
// the byte range this read covered, so the model can Read or grep the middle.
// Only a memory-only log (no file could be opened) is spilled to a new file.
func clampJobRead(body string, out ShellOutput) (model, full string) {
	if body == "" {
		return "[no new output]", ""
	}
	clamped, elided := clampSplit(body, jobReadHeadBytes, jobReadTailBytes)
	if elided == 0 {
		return body, ""
	}
	switch {
	case out.LogPath != "":
		clamped += fmt.Sprintf("\n[full log: %s — this read is bytes %d–%d of it]", out.LogPath, out.From, out.To)
	default:
		if path, ok := spillOutput(body); ok {
			clamped += "\n" + spillMarker + path + "]"
		}
	}
	return clamped, body
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
