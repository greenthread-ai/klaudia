package acp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/plan"
)

func TestTodoWriteBecomesAPlanNotAToolCall(t *testing.T) {
	// The whole point of the tool is the checklist. Reported as a tool call the
	// user gets a JSON array in a collapsed widget instead. And the checklist
	// comes from the plan event — the stored list — not from the call's input,
	// which arrives before the tool has validated anything: a TodoWrite the
	// tool rejected used to show the client a plan that never existed.
	snap := plan.Snapshot{Items: []plan.Item{
		{Content: "write the test", Status: plan.StatusInProgress},
		{Content: "run it", Status: plan.StatusPending},
		{Content: "read the spec", Status: plan.StatusCompleted},
	}, Rev: 1}
	tests := []struct {
		name   string
		events []agent.Event
		want   []string
	}{
		{
			name: "a stored list is posted from the plan event",
			events: []agent.Event{
				{Type: "tool_use", ToolName: "TodoWrite", ToolUseID: "tu_1",
					Input: map[string]any{"todos": []any{map[string]any{"content": "write the test", "status": "in_progress"}}}},
				{Type: "tool_result", ToolName: "TodoWrite", ToolUseID: "tu_1", Content: "3 todos"},
				{Type: "plan", Plan: &snap},
			},
			want: []string{`{"sessionUpdate":"plan","entries":[
				{"content":"write the test","priority":"medium","status":"in_progress"},
				{"content":"run it","priority":"medium","status":"pending"},
				{"content":"read the spec","priority":"medium","status":"completed"}
			]}`},
		},
		{
			name: "a rejected call posts nothing",
			// No plan event follows: the store did not change.
			events: []agent.Event{
				{Type: "tool_use", ToolName: "TodoWrite", ToolUseID: "tu_2",
					Input: map[string]any{"todos": []any{map[string]any{"content": "x", "status": "pending"}, map[string]any{"content": "y", "status": "in_progress"}, map[string]any{"content": "z", "status": "in_progress"}}}},
				{Type: "tool_result", ToolName: "TodoWrite", ToolUseID: "tu_2", Content: "only one item may be in_progress", IsError: true},
			},
		},
		{
			name:   "a cleared plan is an empty plan, not silence",
			events: []agent.Event{{Type: "plan", Plan: &plan.Snapshot{Items: []plan.Item{}, Rev: 2}}},
			want:   []string{`{"sessionUpdate":"plan","entries":[]}`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var r recorder
			tr := newTranslator(&r, "sess_1")
			for _, ev := range tc.events {
				tr.event(ev)
			}
			// The suppressed call's result must not be sent either: a
			// tool_call_update for an id the client never received as a
			// tool_call is a patch to nothing, which clients log as an error.
			if len(r.updates) != len(tc.want) {
				t.Fatalf("got %d updates, want %d: %v", len(r.updates), len(tc.want), r.updates)
			}
			for i, w := range tc.want {
				if !jsonEqual(t, r.updates[i], w) {
					t.Errorf("update %d = %v\nwant %s", i, r.updates[i], w)
				}
			}
		})
	}
}

func TestPlanEntries(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		input any
		want  []planEntry
		ok    bool
	}{
		{
			name: "an unknown status becomes pending",
			// Anything but the three ACP statuses would be rejected by a
			// client, and the model does invent them.
			tool:  "TodoWrite",
			input: map[string]any{"todos": []any{map[string]any{"content": "x", "status": "blocked"}}},
			want:  []planEntry{{Content: "x", Priority: planPriorityMedium, Status: planPending}},
			ok:    true,
		},
		{
			name:  "a blank entry is dropped",
			tool:  "TodoWrite",
			input: map[string]any{"todos": []any{map[string]any{"content": "  ", "status": "pending"}}},
			want:  []planEntry{},
			ok:    true,
		},
		{
			name: "an empty list is still a plan",
			// How the model clears one. Reporting false here would leak the
			// call back out as a raw tool call.
			tool:  "TodoWrite",
			input: map[string]any{"todos": []any{}},
			want:  []planEntry{},
			ok:    true,
		},
		{
			name:  "another tool is not a plan",
			tool:  "Read",
			input: map[string]any{"file_path": "/a/b.go"},
			ok:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := planEntries(tc.tool, tc.input)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d entries, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("entry %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestDiffContent(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "have.go")
	if err := os.WriteFile(existing, []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "new.go")
	empty := filepath.Join(dir, "empty.go")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		tool  string
		input map[string]any
		want  string
	}{
		{
			name: "Edit sends its own strings as the diff",
			// Not a reconstructed whole file: this is the text the permission
			// prompt asks the user to approve, and recomputing the replacement
			// here could show them a different change from the one that runs.
			tool: "Edit",
			input: map[string]any{
				"file_path": "/a/b.go", "old_string": "a := 1", "new_string": "a := 2",
			},
			want: `[{"type":"diff","path":"/a/b.go","oldText":"a := 1","newText":"a := 2"}]`,
		},
		{
			name:  "Write reads the file it is about to overwrite",
			tool:  "Write",
			input: map[string]any{"file_path": existing, "content": "package new\n"},
			want:  `[{"type":"diff","path":"` + existing + `","oldText":"package old\n","newText":"package new\n"}]`,
		},
		{
			name: "Write of a new file has no oldText at all",
			// Absent, not empty: the schema's oldText is "None for new files",
			// and a client renders that as a creation. An empty string would
			// claim the file exists and is empty — which the next case is.
			tool:  "Write",
			input: map[string]any{"file_path": missing, "content": "package new\n"},
			want:  `[{"type":"diff","path":"` + missing + `","newText":"package new\n"}]`,
		},
		{
			name: "an existing but empty file still sends an oldText",
			// The pointer earns its keep here: omitempty on a *string tests the
			// pointer, so "" survives while nil is dropped.
			tool:  "Write",
			input: map[string]any{"file_path": empty, "content": "package new\n"},
			want:  `[{"type":"diff","path":"` + empty + `","oldText":"","newText":"package new\n"}]`,
		},
		{
			name: "truncating a file to nothing is still a diff",
			// The reason the input is re-decoded rather than flattened to
			// strings: an absent "content" and an empty one are different
			// edits, and a string map cannot tell them apart.
			tool:  "Write",
			input: map[string]any{"file_path": existing, "content": ""},
			want:  `[{"type":"diff","path":"` + existing + `","oldText":"package old\n","newText":""}]`,
		},
		{
			name:  "a half-specified Edit has no diff",
			tool:  "Edit",
			input: map[string]any{"file_path": "/a/b.go", "old_string": "a := 1"},
			want:  `null`,
		},
		{
			name:  "a tool that changes no file has no diff",
			tool:  "Read",
			input: map[string]any{"file_path": existing},
			want:  `null`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var r recorder
			tr := newTranslator(&r, "sess_1")
			got := tr.diffContent(tc.tool, tc.input)
			if !jsonEqual(t, got, tc.want) {
				b := mustJSON(t, got)
				t.Errorf("got  %s\nwant %s", b, tc.want)
			}
		})
	}
}

func TestDiffReachesTheToolCall(t *testing.T) {
	// diffContent is only useful if the tool_call carries it: the client needs
	// the diff at approval time, not after the edit has happened.
	var r recorder
	tr := newTranslator(&r, "sess_1")
	tr.readFile = func(string) ([]byte, error) { return []byte("before\n"), nil }
	tr.event(agent.Event{
		Type: "tool_use", ToolName: "Write", ToolUseID: "tu_1",
		Input: map[string]any{"file_path": "/a/b.go", "content": "after\n"},
	})
	if len(r.updates) != 1 {
		t.Fatalf("got %d updates, want 1", len(r.updates))
	}
	content, _ := r.updates[0]["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("tool_call content = %v, want one diff", r.updates[0]["content"])
	}
	entry, _ := content[0].(map[string]any)
	if entry["type"] != "diff" || entry["oldText"] != "before\n" || entry["newText"] != "after\n" {
		t.Errorf("diff = %+v", entry)
	}
}
