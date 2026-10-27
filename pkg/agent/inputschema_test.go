package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// A Pydantic/zod-style schema keeps its $defs, so "$ref" resolves; a built-in
// tool's schema (additionalProperties, no $defs) is sent exactly as before.
func TestInputSchemaKeepsDefinitions(t *testing.T) {
	mcp := json.RawMessage(`{"type":"object","properties":{"item":{"$ref":"#/$defs/Item"}},"required":["item"],
		"$defs":{"Item":{"type":"object","properties":{"id":{"type":"string"}}}},"title":"Args"}`)
	b, _ := json.Marshal(inputSchemaParam(mcp))
	if !strings.Contains(string(b), `"$defs"`) || !strings.Contains(string(b), `"#/$defs/Item"`) {
		t.Errorf("definitions dropped: %s", b)
	}
	if strings.Contains(string(b), `"title"`) {
		t.Errorf("non-structural keys should not be forwarded: %s", b)
	}

	builtin := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)
	b, _ = json.Marshal(inputSchemaParam(builtin))
	if got, want := string(b), `{"properties":{"path":{"type":"string"}},"required":["path"],"type":"object"}`; got != want {
		t.Errorf("built-in schema changed:\n got %s\nwant %s", got, want)
	}
}
