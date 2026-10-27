package agent

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/greenthread-ai/klaudia/pkg/plan"
)

// Event is a wire format (EventSchemaVersion). This pins its JSON names: a
// change here that is not purely additive must bump EventSchemaVersion and say
// so in the CHANGELOG — a stream-json peer reads these names, not Go fields.
func TestEventWireShape(t *testing.T) {
	ev := Event{
		Type: EventToolResult, Text: "t", ToolName: "n", ToolUseID: "id", Input: map[string]any{"k": 1},
		Content: "c", IsError: true, HostBlocked: true, FullContent: "f", Data: map[string]any{"line": 1},
		InputDelta: 1, OutputDelta: 1, TurnDelta: 1,
		Plan: &plan.Snapshot{Items: []plan.Item{{Content: "x", Status: plan.StatusPending, ActiveForm: "y"}}, Approved: true, Path: "p", Rev: 1},
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	want := []string{"content", "data", "full_content", "host_blocked", "input", "input_delta", "is_error",
		"output_delta", "plan", "text", "tool_name", "tool_use_id", "turn_delta", "type"}
	if got := keys(m); !reflect.DeepEqual(got, want) {
		t.Errorf("Event JSON fields = %v, want %v — renaming or removing one breaks peers; bump EventSchemaVersion", got, want)
	}
	p := m["plan"].(map[string]any)
	if got, want := keys(p), []string{"approved", "items", "path", "rev"}; !reflect.DeepEqual(got, want) {
		t.Errorf("plan fields = %v, want %v", got, want)
	}
	if got, want := keys(p["items"].([]any)[0].(map[string]any)), []string{"active_form", "content", "status"}; !reflect.DeepEqual(got, want) {
		t.Errorf("plan item fields = %v, want %v", got, want)
	}
	if EventSchemaVersion != 1 {
		t.Errorf("EventSchemaVersion moved to %d: update this test's expectations alongside it", EventSchemaVersion)
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
