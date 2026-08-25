package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/schema"
)

// RequestHostChange exists to make one approval cover a whole operation.
//
// The obvious alternative — classify each command as it is attempted and prompt
// on the first one that touches the host — cannot do that. It only ever sees one
// command at a time, so installing nginx asks at the package manager, again at
// the config write, and again at the restart. That is the prompt fatigue the
// whole trust model is meant to remove, and it is why the flow is inverted:
// the model says what it intends to do to the machine, the user agrees to the
// operation, and the commands that carry it out proceed without interruption.
//
// It also changes what the user is asked. "Allow Bash(sudo apt-get install -y
// nginx)?" is a question about a command line. "Install nginx and configure it
// as a development proxy, because the task asks for the app to run behind a
// local proxy" is a question about the thing they actually care about.

// HostChangeInput is what the model declares.
type HostChangeInput struct {
	Summary  string   `json:"summary" jsonschema:"description=One sentence describing the whole operation in the user's terms - e.g. 'Install nginx and configure it as a development proxy'. Not a command line."`
	Reason   string   `json:"reason" jsonschema:"description=Why the task needs this. The user is deciding whether the change is worth it, so say what it buys them."`
	Paths    []string `json:"paths,omitempty" jsonschema:"description=Files and directories outside the project you intend to create or modify. Exact paths - no globs or wildcards."`
	Services []string `json:"services,omitempty" jsonschema:"description=System services you intend to start, stop, restart, enable or disable."`
	Packages []string `json:"packages,omitempty" jsonschema:"description=Packages you intend to install or remove, by name."`
}

// HostChangeRequest is the declared change, passed to the frontend.
type HostChangeRequest struct {
	Summary  string
	Reason   string
	Paths    []string
	Services []string
	Packages []string
}

// HostChangeOutcome is the frontend's answer.
type HostChangeOutcome struct {
	Approved bool
	// Message, when set, replaces the default text sent back to the model —
	// a frontend can explain a refusal in the user's own words.
	Message string
}

// HostApprover puts a declared host change to the user and records the grant it
// buys. Implemented in the agent package, which owns the grant ledger.
type HostApprover interface {
	RequestHostChange(ctx context.Context, req HostChangeRequest) (HostChangeOutcome, error)
}

// RequestHostChange is the tool.
type RequestHostChange struct {
	schema *schema.Schema
}

// NewRequestHostChange constructs the tool.
func NewRequestHostChange() (*RequestHostChange, error) {
	s, err := schema.For[HostChangeInput]()
	if err != nil {
		return nil, fmt.Errorf("requesthostchange: build schema: %w", err)
	}
	return &RequestHostChange{schema: s}, nil
}

func (r *RequestHostChange) Name() string { return "RequestHostChange" }

func (r *RequestHostChange) Description(context.Context) (string, error) {
	return "Ask the user's agreement before changing the machine Klaudia is running on.\n\n" +
		"Work inside the project is autonomous — editing, building, testing, running dev servers, " +
		"git, and anything on a remote host the task calls for. You do not need this tool for any " +
		"of that. Use it only for changes to this machine itself: writing outside the project " +
		"(/etc, /usr, /opt, /Library, shell rc files), installing or removing packages, " +
		"controlling system services, users, firewall, mounts or kernel parameters.\n\n" +
		"Declare the whole operation once, before you start it, at the level the user cares " +
		"about — not one command at a time. Approving it covers every step inside the scope you " +
		"describe, so list the paths, services and packages the operation will touch. " +
		"If you later need something outside that scope, call this again for the extra part; the " +
		"user will be told it was not in what they approved.\n\n" +
		"If the user declines, do not look for another way to make the change. " +
		"Carry on with the rest of the task and say what you could not do.", nil
}

func (r *RequestHostChange) InputSchema() json.RawMessage { return r.schema.Raw }

func (r *RequestHostChange) ValidateInput(raw json.RawMessage) error {
	if err := r.schema.Validate(raw); err != nil {
		return err
	}
	var in HostChangeInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Summary) == "" {
		return fmt.Errorf("summary is required: say what the operation is, in the user's terms")
	}
	if strings.TrimSpace(in.Reason) == "" {
		return fmt.Errorf("reason is required: the user is deciding whether this is worth it")
	}
	if len(in.Paths) == 0 && len(in.Services) == 0 && len(in.Packages) == 0 {
		return fmt.Errorf("declare at least one path, service or package — " +
			"an approval with no scope would cover nothing and you would be asked again on the first command")
	}
	for _, p := range in.Paths {
		if strings.ContainsAny(p, "*?[") {
			return fmt.Errorf("%q is a pattern: declare exact paths. "+
				"Approving one file inside a directory already covers the rest of that directory", p)
		}
	}
	return nil
}

// PermissionRequest: asking is harmless. The change itself is gated separately,
// which is the whole point — this tool cannot be used to perform anything.
func (r *RequestHostChange) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (r *RequestHostChange) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (r *RequestHostChange) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in HostChangeInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if tctx.HostChange == nil {
		// No one to ask. Say so plainly rather than pretending to have asked:
		// a model told "approved" here would go on to be refused by the gate on
		// every command, which is a confusing way to learn there is no user.
		return []Result{{
			Content: "There is no one to ask — this session has no interactive approver, " +
				"so changes to this machine cannot be authorised. Continue with the rest of the task " +
				"without them and say what you could not do.",
			IsError: true,
		}}, nil
	}

	out, err := tctx.HostChange.RequestHostChange(ctx, HostChangeRequest{
		// Short display strings, same double-escaping risk as a question.
		Summary:  unescapeDisplayText(strings.TrimSpace(in.Summary)),
		Reason:   unescapeDisplayText(strings.TrimSpace(in.Reason)),
		Paths:    in.Paths,
		Services: in.Services,
		Packages: in.Packages,
	})
	if err != nil {
		return []Result{{Content: "Could not ask the user: " + err.Error(), IsError: true}}, nil
	}
	if out.Message != "" {
		return []Result{{Content: out.Message, IsError: !out.Approved}}, nil
	}
	if !out.Approved {
		return []Result{{Content: "The user declined this change to their machine. " +
			"Do not retry it or look for another way to make it. Continue with the rest of the task " +
			"without it, and say what you could not do."}}, nil
	}
	return []Result{{Content: "Approved: " + strings.TrimSpace(in.Summary) + ". " +
		"Go ahead with the steps inside that scope — you will not be asked again for them. " +
		"Anything outside it will still stop."}}, nil
}
