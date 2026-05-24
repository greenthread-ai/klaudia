package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/schema"
)

// Spawner runs a sub-agent of the given type with a prompt and returns its
// final textual result. Implemented by the agent package to avoid an import
// cycle (tools must not import agent).
type Spawner interface {
	Spawn(ctx context.Context, subagentType, prompt string) (string, error)
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

func (a *Agent) Description(context.Context) (string, error) {
	var b strings.Builder
	b.WriteString("Launch a new sub-agent to handle a complex, multi-step task autonomously. ")
	b.WriteString("Choose subagent_type based on the work:\n")
	for _, t := range a.types {
		fmt.Fprintf(&b, "- %s: %s\n", t.Name, t.Description)
	}
	b.WriteString("The sub-agent runs to completion and returns a single final message; it cannot " +
		"ask follow-up questions, so give it a complete, self-contained prompt.")
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

func (a *Agent) Execute(ctx context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in AgentInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	result, err := a.spawner.Spawn(ctx, in.SubagentType, in.Prompt)
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Sub-agent failed: %v", err), IsError: true}}, nil
	}
	return []Result{{Content: result}}, nil
}
