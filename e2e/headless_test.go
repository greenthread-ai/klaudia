package e2e

import (
	"os"
	"strings"
	"testing"
)

// The client-side tools round-trip through the real binary: tool_use in the
// SSE stream, dispatch, execution on disk, tool_result back to the model, and
// a well-formed result envelope at the end. Ported from smoke.sh, where it
// depended on haiku following a four-step instruction.
func TestHeadlessToolRoundTrip(t *testing.T) {
	m := NewFakeModel(t,
		Use("Write", map[string]any{"file_path": "a.txt", "content": "apple"}),
		Use("Read", map[string]any{"file_path": "a.txt"}),
		Use("Glob", map[string]any{"pattern": "*.txt"}),
		Use("Grep", map[string]any{"pattern": "apple", "output_mode": "files_with_matches"}),
		Say("done."),
	)
	e := NewEnv(t, m)
	r := e.Headless("use the tools", "--dangerously-skip-permissions")

	if r.ExitCode != 0 {
		t.Fatalf("exit %d, want 0\n%s", r.ExitCode, r.dump())
	}
	if got, err := os.ReadFile(e.Path("a.txt")); err != nil || string(got) != "apple" {
		t.Fatalf("a.txt = %q, %v; want apple", got, err)
	}

	events := r.Events()
	if got, want := strings.Join(toolUses(events), ","), "Write,Read,Glob,Grep"; got != want {
		t.Errorf("tool uses = %s, want %s", got, want)
	}
	results := toolResults(events)
	if len(results) != 4 {
		t.Fatalf("got %d tool results, want 4\n%s", len(results), r.dump())
	}
	for i, res := range results {
		if res["is_error"] == true {
			t.Errorf("tool result %d is an error: %s", i, resultText(res))
		}
	}
	if !strings.Contains(resultText(results[1]), "apple") {
		t.Errorf("Read result lacks the file's content: %q", resultText(results[1]))
	}
	for i, name := range []string{"Glob", "Grep"} {
		if !strings.Contains(resultText(results[2+i]), "a.txt") {
			t.Errorf("%s result lacks a.txt: %q", name, resultText(results[2+i]))
		}
	}

	res := r.ResultEvent()
	if res == nil {
		t.Fatalf("no result envelope\n%s", r.dump())
	}
	if res["is_error"] != false || res["subtype"] != "success" || res["result"] != "done." {
		t.Errorf("result envelope = %v", res)
	}

	// The model saw each tool's result before it was asked for the next call.
	reqs := m.Requests()
	if len(reqs) != 5 {
		t.Fatalf("model called %d times, want 5", len(reqs))
	}
	if !strings.Contains(reqs[2].Raw(), "apple") {
		t.Errorf("the Read result never reached the model")
	}
}

// Text-only answers go straight to the result, in text mode as well as JSON.
func TestHeadlessTextOutput(t *testing.T) {
	m := NewFakeModel(t, Say("hello from the fake model"))
	e := NewEnv(t, m)
	r := e.Run("-p", "say hi")
	if r.ExitCode != 0 {
		t.Fatalf("exit %d\n%s", r.ExitCode, r.dump())
	}
	if strings.TrimSpace(r.Stdout) != "hello from the fake model" {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

// --model reaches the API request unchanged.
func TestHeadlessModelFlagReachesRequest(t *testing.T) {
	m := NewFakeModel(t, Say("ok"))
	e := NewEnv(t, m)
	e.Headless("hi", "--model", "claude-e2e-model")
	reqs := m.Requests()
	if len(reqs) == 0 || reqs[0].Model != "claude-e2e-model" {
		t.Fatalf("requests = %+v, want model claude-e2e-model", reqs)
	}
}

// An unknown tool name from the model is answered with an error result the
// model can recover from, not a crash.
func TestHeadlessUnknownToolRecovers(t *testing.T) {
	m := NewFakeModel(t, Use("NoSuchTool", map[string]any{}), Say("recovered"))
	e := NewEnv(t, m)
	r := e.Headless("x", "--dangerously-skip-permissions")
	if r.ExitCode != 0 {
		t.Fatalf("exit %d\n%s", r.ExitCode, r.dump())
	}
	results := toolResults(r.Events())
	if len(results) != 1 || results[0]["is_error"] != true {
		t.Fatalf("want one error tool_result, got %v", results)
	}
	if res := r.ResultEvent(); res == nil || res["result"] != "recovered" {
		t.Errorf("result = %v", res)
	}
}
