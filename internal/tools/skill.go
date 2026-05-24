package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/schema"
)

// SkillInfo is one skill the model can invoke through the Skill tool. It mirrors
// skill.Skill but keeps the tools package free of a skill-package import; the
// CLI builds these from skill.Load.
type SkillInfo struct {
	Name        string
	Description string
	// Render returns the skill body with $ARGUMENTS substituted by the supplied
	// arguments. Supplied by the loader so this package stays decoupled.
	Render func(arguments string) string
}

// SkillInput is the Skill tool's input.
type SkillInput struct {
	Name      string `json:"name" jsonschema:"description=The skill to run (one of the listed names)"`
	Arguments string `json:"arguments,omitempty" jsonschema:"description=Free-form arguments substituted into the skill's $ARGUMENTS"`
}

// Skill lets the model invoke a named, user-defined skill: the rendered skill
// body is returned as a tool_result (injected instructions the model then
// follows). Skills are discovered from ~/.claude/skills and <cwd>/.klaudia/skills.
type Skill struct {
	schema *schema.Schema
	byName map[string]SkillInfo
	names  []string // sorted, for the description
}

// NewSkill builds the Skill tool over the available skills. Returns (nil, nil)
// when there are no skills, so the caller can skip registering it.
func NewSkill(skills []SkillInfo) (*Skill, error) {
	if len(skills) == 0 {
		return nil, nil
	}
	s, err := schema.For[SkillInput]()
	if err != nil {
		return nil, fmt.Errorf("skill: build schema: %w", err)
	}
	byName := make(map[string]SkillInfo, len(skills))
	names := make([]string, 0, len(skills))
	for _, sk := range skills {
		byName[sk.Name] = sk
		names = append(names, sk.Name)
	}
	sort.Strings(names)
	return &Skill{schema: s, byName: byName, names: names}, nil
}

func (t *Skill) Name() string { return "Skill" }

func (t *Skill) Description(context.Context) (string, error) {
	var b strings.Builder
	b.WriteString("Invoke a user-defined skill: a reusable, named set of instructions. ")
	b.WriteString("Call this when a request matches one of the available skills. Available skills:\n")
	for _, n := range t.names {
		fmt.Fprintf(&b, "- %s: %s\n", n, t.byName[n].Description)
	}
	b.WriteString("The skill's instructions are returned as the tool result; follow them. " +
		"Pass any extra detail the skill needs via \"arguments\".")
	return b.String(), nil
}

func (t *Skill) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *Skill) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in SkillInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if _, ok := t.byName[in.Name]; !ok {
		return fmt.Errorf("no such skill %q (available: %s)", in.Name, strings.Join(t.names, ", "))
	}
	return nil
}

// PermissionRequest: invoking a skill only injects instructions; the tools the
// skill then drives are individually permission-checked when called.
func (t *Skill) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (t *Skill) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (t *Skill) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in SkillInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	sk, ok := t.byName[in.Name]
	if !ok {
		return []Result{{Content: fmt.Sprintf("No such skill %q.", in.Name), IsError: true}}, nil
	}
	return []Result{{Content: sk.Render(in.Arguments)}}, nil
}
