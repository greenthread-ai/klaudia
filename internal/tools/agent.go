package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/schema"
)

// Spawner runs a sub-agent of the given type with a prompt and returns its
// final textual result. Implemented by the agent package to avoid an import
// cycle (tools must not import agent).
type Spawner interface {
	// progress, when non-nil, is called with short display lines as the child
	// works, so the frontend can show what it is doing instead of a bare spinner.
	Spawn(ctx context.Context, subagentType, prompt string, progress func(line string)) (string, error)
}

// AgentTypeInfo is the model-facing summary of a sub-agent type, used to build
// the Agent tool's description.
type AgentTypeInfo struct {
	Name        string
	Description string
}

// AgentInput is the Agent tool's input.
type AgentInput struct {
	Description  string `json:"description" jsonschema:"description=A short (3-5 word) description of the task"`
	Prompt       string `json:"prompt" jsonschema:"description=The task for the sub-agent to perform autonomously"`
	SubagentType string `json:"subagent_type" jsonschema:"description=The type of sub-agent to use"`
	// RunInBackground starts the sub-agent and returns at once; its result
	// arrives later as a message.
	RunInBackground bool `json:"run_in_background,omitempty" jsonschema:"description=Run the sub-agent in the background and return immediately; its result arrives as a message when it finishes. Use for independent work you do not need before your next step"`
}

// Agent launches a sub-agent (its own agentic loop with a filtered toolset)
// and returns its final result, mirroring the JS Agent/Task tool.
type Agent struct {
	schema  *schema.Schema
	spawner Spawner
	types   []AgentTypeInfo
	valid   map[string]bool
}

// NewAgent constructs the Agent tool. types lists the selectable sub-agent
// types (for the description and validation); spawner runs them.
func NewAgent(spawner Spawner, types []AgentTypeInfo) (*Agent, error) {
	s, err := schema.For[AgentInput]()
	if err != nil {
		return nil, fmt.Errorf("agent: build schema: %w", err)
	}
	valid := make(map[string]bool, len(types))
	for _, t := range types {
		valid[t.Name] = true
	}
	return &Agent{schema: s, spawner: spawner, types: types, valid: valid}, nil
}

func (a *Agent) Name() string { return "Agent" }

// HasType reports whether name is a known sub-agent type. Used by the loop to
// steer a model that calls a sub-agent type as if it were a top-level tool.
func (a *Agent) HasType(name string) bool { return a.valid[name] }

func (a *Agent) Description(context.Context) (string, error) {
	var b strings.Builder
	b.WriteString("Launch a new sub-agent to handle a complex, multi-step task autonomously. ")
	b.WriteString("Choose subagent_type based on the work:\n")
	for _, t := range a.types {
		fmt.Fprintf(&b, "- %s: %s\n", t.Name, t.Description)
	}
	b.WriteString("The sub-agent runs to completion and returns a single final message; it cannot " +
		"ask follow-up questions, so give it a complete, self-contained prompt. ")
	b.WriteString("Set run_in_background for independent work you do not need before your next step — " +
		"several can run at once — then carry on: each result arrives as a message when it is done, " +
		"so do not wait for it or poll.")
	return b.String(), nil
}

func (a *Agent) InputSchema() json.RawMessage { return a.schema.Raw }

func (a *Agent) ValidateInput(raw json.RawMessage) error {
	if err := a.schema.Validate(raw); err != nil {
		return err
	}
	var in AgentInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if !a.valid[in.SubagentType] {
		return fmt.Errorf("unknown subagent_type %q", in.SubagentType)
	}
	return nil
}

// PermissionRequest: spawning an agent is gated by the sub-agent's own tool
// permissions, so the Agent tool itself is allow-always.
func (a *Agent) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (a *Agent) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (a *Agent) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in AgentInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if in.RunInBackground && tctx.Background != nil {
		label := in.SubagentType
		if d := strings.TrimSpace(in.Description); d != "" {
			label += ": " + d
		}
		id := tctx.Background.Start(label, func(ctx context.Context, progress func(string)) (string, error) {
			return a.spawner.Spawn(ctx, in.SubagentType, in.Prompt, progress)
		})
		return []Result{{Content: fmt.Sprintf("Started sub-agent %s (%s) in the background. Its result "+
			"will arrive as a message when it finishes — carry on with other work meanwhile; do not "+
			"wait for it or poll.", id, label)}}, nil
	}
	note := ""
	if in.RunInBackground {
		// Said rather than silently ignored, so the model does not go on
		// waiting for a message that will never come.
		note = "(Background runs are not available here, so this sub-agent ran in the foreground.)\n\n"
	}
	result, err := a.spawner.Spawn(ctx, in.SubagentType, in.Prompt, tctx.Progress)
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Sub-agent failed: %v", err), IsError: true}}, nil
	}
	return []Result{{Content: note + result}}, nil
}
