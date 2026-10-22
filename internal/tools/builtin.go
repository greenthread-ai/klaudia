package tools

import (
	"encoding/json"
	"fmt"
)

// BuiltinSchemas returns the input schema of every tool this package defines,
// keyed by name, including the ones DefaultRegistry adds only conditionally
// (LSP, Memory, Skill, ToolSearch, Agent).
//
// It exists so the frontends' per-tool maps (ACP's titles, kinds and
// locations; the TUI's header summaries) can be checked against tools that
// really exist. Those maps are keyed by bare strings, and nothing noticed when
// a key stopped matching: ACP still carried WebSearch and WebFetch, names no
// tool has, and the retired Task* tools' entries outlived the tools. The MCP
// resource tools live in package mcp, which imports this one; see
// mcp.ResourceToolSchemas.
func BuiltinSchemas() (map[string]json.RawMessage, error) {
	reg, err := DefaultRegistry(nil)
	if err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{}
	for _, name := range reg.Names() {
		t, _ := reg.Lookup(name)
		out[name] = t.InputSchema()
	}
	// Built with nil dependencies: only the schema is read, and every one of
	// these constructors touches its dependency at Execute, not at New.
	extra := []func() (Tool, error){
		func() (Tool, error) { return NewDiagnostics(nil) },
		func() (Tool, error) { return NewDefinition(nil) },
		func() (Tool, error) { return NewReferences(nil) },
		func() (Tool, error) { return NewMemory(nil) },
		func() (Tool, error) { return NewSkill([]SkillInfo{{Name: "x"}}) },
		func() (Tool, error) { return NewToolSearch(nil) },
		func() (Tool, error) { return NewAgent(nil, nil) },
	}
	for _, mk := range extra {
		t, err := mk()
		if err != nil {
			return nil, err
		}
		if t == nil {
			return nil, fmt.Errorf("a builtin constructor returned no tool")
		}
		out[t.Name()] = t.InputSchema()
	}
	return out, nil
}

// SchemaHasField reports whether a tool's input schema declares field as a
// top-level property.
func SchemaHasField(schema json.RawMessage, field string) bool {
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(schema, &s) != nil {
		return false
	}
	_, ok := s.Properties[field]
	return ok
}
