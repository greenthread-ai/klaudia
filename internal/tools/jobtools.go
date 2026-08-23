package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/schema"
)

// --- Jobs tool ---

// JobsInput is empty: listing takes no arguments.
type JobsInput struct{}

// Jobs answers "what's running?".
//
// The store has always known, but nothing could ask it — List() was called only
// from its own test. A model that started a dev server three turns ago had no
// way to check whether it was still up, so it would start another one.
type Jobs struct {
	schema *schema.Schema
	jobs   *JobStore
}

func NewJobs(jobs *JobStore) (*Jobs, error) {
	s, err := schema.For[JobsInput]()
	if err != nil {
		return nil, fmt.Errorf("jobs: build schema: %w", err)
	}
	return &Jobs{schema: s, jobs: jobs}, nil
}

func (t *Jobs) Name() string { return "Jobs" }

func (t *Jobs) Description(context.Context) (string, error) {
	return "List the background jobs this session started: id, name, command, whether each is " +
		"still running, the port it announced, and where it runs. Check this before starting a " +
		"long-running command — the one you need is often already up.", nil
}

func (t *Jobs) InputSchema() json.RawMessage            { return t.schema.Raw }
func (t *Jobs) ValidateInput(raw json.RawMessage) error { return nil }
func (t *Jobs) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (t *Jobs) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (t *Jobs) Execute(context.Context, Context, json.RawMessage) ([]Result, error) {
	if t.jobs == nil {
		return []Result{{Content: "background jobs are not available"}}, nil
	}
	list := t.jobs.List()
	if len(list) == 0 {
		return []Result{{Content: "No background jobs."}}, nil
	}
	var b strings.Builder
	for _, j := range list {
		state := "running"
		if !j.Running {
			state = fmt.Sprintf("exited (%d)", j.ExitCode)
		}
		fmt.Fprintf(&b, "%s  %s  %q  %s", j.ID, j.Name, j.Command, state)
		if j.Port != "" {
			fmt.Fprintf(&b, "  port %s", j.Port)
		}
		if j.Where != "local" {
			fmt.Fprintf(&b, "  on %s", j.Where)
		}
		if j.Restarts > 0 {
			fmt.Fprintf(&b, "  restarted %d×", j.Restarts)
		}
		b.WriteString("\n")
	}
	return []Result{{Content: strings.TrimRight(b.String(), "\n")}}, nil
}

// --- RestartJob tool ---

type RestartJobInput struct {
	Job string `json:"job" jsonschema:"description=The job's id (e.g. bash_1) or name (e.g. dev) to restart"`
}

// RestartJob stops a job and starts the same command again in the same slot.
//
// This is the spec's "restart the API should operate on the existing API
// process rather than launch a mystery second copy". Without it the model's
// only route is kill-then-start, which produces a new id, a new log, and a race
// against the old process for the port.
type RestartJob struct {
	schema *schema.Schema
	jobs   *JobStore
}

func NewRestartJob(jobs *JobStore) (*RestartJob, error) {
	s, err := schema.For[RestartJobInput]()
	if err != nil {
		return nil, fmt.Errorf("restartjob: build schema: %w", err)
	}
	return &RestartJob{schema: s, jobs: jobs}, nil
}

func (t *RestartJob) Name() string { return "RestartJob" }

func (t *RestartJob) Description(context.Context) (string, error) {
	return "Restart a background job by id or name: stops it (and its whole process group), waits " +
		"for it to release its port, and runs the same command again under the same id, name and " +
		"log. Use this rather than killing and starting a new one.", nil
}

func (t *RestartJob) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *RestartJob) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in RestartJobInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Job) == "" {
		return fmt.Errorf("job is required: the id or name to restart")
	}
	return nil
}

func (t *RestartJob) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (t *RestartJob) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx) // restarting something we already started
}

func (t *RestartJob) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in RestartJobInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if t.jobs == nil {
		return []Result{{Content: "background jobs are not available", IsError: true}}, nil
	}
	st, ok := t.jobs.Restart(in.Job)
	if st.ID == "" {
		return []Result{{Content: t.jobs.unknownJobMsg(in.Job), IsError: true}}, nil
	}
	if !ok {
		return []Result{{Content: fmt.Sprintf(
			"Stopped job %s but could not start it again. Read its log with BashOutput(bash_id=%q).",
			st.Name, st.Name), IsError: true}}, nil
	}
	return []Result{{Content: fmt.Sprintf(
		"Restarted job %s (%s). Read new output with BashOutput(bash_id=%q).", st.Name, st.ID, st.Name)}}, nil
}
