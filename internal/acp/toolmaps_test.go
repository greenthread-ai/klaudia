package acp

import (
	"encoding/json"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/mcp"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

func knownToolSchemas(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	all, err := tools.BuiltinSchemas()
	if err != nil {
		t.Fatal(err)
	}
	res, err := mcp.ResourceToolSchemas()
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range res {
		all[k] = v
	}
	return all
}

// These maps are keyed by tool name and point at input fields, and nothing
// checked either half. Measured when this test was written: salientKey and
// toolKinds still carried WebSearch and WebFetch, which no tool is called
// (the server tools are web_search / web_fetch and never reach this path),
// and the retired Task* tools' entries had outlived the tools. A key must be
// a real tool, and a field must be one that tool's schema declares — a typo
// there silently drops the title or the editor location.
func TestToolMapsNameRealToolsAndFields(t *testing.T) {
	schemas := knownToolSchemas(t)
	fieldMaps := []struct {
		name string
		m    map[string]string
	}{
		{"salientKey", salientKey},
		{"locationKey", locationKey},
	}
	for _, fm := range fieldMaps {
		for tool, field := range fm.m {
			s, ok := schemas[tool]
			if !ok {
				t.Errorf("%s[%q]: no tool has that name", fm.name, tool)
				continue
			}
			if !tools.SchemaHasField(s, field) {
				t.Errorf("%s[%q] = %q: %s's schema has no such field", fm.name, tool, field, tool)
			}
		}
	}
	for tool := range toolKinds {
		if _, ok := schemas[tool]; !ok {
			t.Errorf("toolKinds[%q]: no tool has that name", tool)
		}
	}
}
