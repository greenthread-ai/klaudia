package streamjson

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/pkg/agent"
)

// A peer learns the event schema version before the first event, from a
// system/init line like Claude Code's — and the host's own fields ride along.
func TestRunOpensWithInit(t *testing.T) {
	var out bytes.Buffer
	d := NewDriver(&out)
	d.Init = map[string]any{"session_id": "s1", "model": "m", "type": "not allowed to override"}
	run := func(context.Context, agent.Turn) (agent.Result, error) { return agent.Result{}, nil }
	if err := d.Run(context.Background(), strings.NewReader(""), nil, run); err != nil {
		t.Fatal(err)
	}
	first := strings.SplitN(out.String(), "\n", 2)[0]
	var m map[string]any
	if err := json.Unmarshal([]byte(first), &m); err != nil {
		t.Fatalf("first line is not JSON: %q", first)
	}
	if m["type"] != "system" || m["subtype"] != "init" || m["schema_version"] != float64(agent.EventSchemaVersion) {
		t.Errorf("first line = %v, want system/init with schema_version %d", m, agent.EventSchemaVersion)
	}
	if m["session_id"] != "s1" || m["model"] != "m" {
		t.Errorf("host fields missing: %v", m)
	}
}
