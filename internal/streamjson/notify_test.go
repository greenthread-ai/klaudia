package streamjson

import (
	"bytes"
	"encoding/json"
	"testing"
)

// A job exit between turns reaches the peer as a system line.
func TestNotifyWritesASystemLine(t *testing.T) {
	var buf bytes.Buffer
	d := NewDriver(&buf)
	d.Notify("job_exit", map[string]any{"job_id": "bash_1", "exit_code": 0})
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("not one JSON line: %q", buf.String())
	}
	if m["type"] != "system" || m["subtype"] != "job_exit" || m["job_id"] != "bash_1" {
		t.Errorf("line = %v", m)
	}
}
