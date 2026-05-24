package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestDefaultRegistryLoadsAllTools(t *testing.T) {
	reg, err := DefaultRegistry(nil)
	if err != nil {
		t.Fatalf("DefaultRegistry: %v", err)
	}
	want := []string{"Read", "Write", "Edit", "Glob", "Grep", "Bash", "TodoWrite", "NotebookEdit"}
	for _, name := range want {
		tool, ok := reg.Lookup(name)
		if !ok {
			t.Errorf("tool %q not registered", name)
			continue
		}
		// Every tool must advertise a non-empty description and a valid
		// object input schema.
		desc, err := tool.Description(context.Background())
		if err != nil || desc == "" {
			t.Errorf("%s: description err=%v empty=%v", name, err, desc == "")
		}
		var m map[string]any
		if err := json.Unmarshal(tool.InputSchema(), &m); err != nil {
			t.Errorf("%s: input schema invalid: %v", name, err)
		}
		if m["type"] != "object" {
			t.Errorf("%s: schema type = %v, want object", name, m["type"])
		}
	}
	if got := len(reg.Names()); got != len(want) {
		t.Errorf("registry has %d tools, want %d", got, len(want))
	}
}
