package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// Guards the deliberate web-tool configuration: the current GA versions, in
// direct mode. If someone bumps the version or flips allowed_callers (enabling
// dynamic filtering, which changes the result shape and gates on model
// capability), this fails so the change is a conscious one.
func TestWebToolParamsAreCurrentAndDirect(t *testing.T) {
	b, err := json.Marshal(webToolParams())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	for _, want := range []string{
		`"type":"web_search_20260318"`,
		`"type":"web_fetch_20260318"`,
		`"name":"web_search"`,
		`"name":"web_fetch"`,
		`"allowed_callers":["direct"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("web tool params missing %s\ngot: %s", want, got)
		}
	}
	// Exactly one search + one fetch tool, no accidental dynamic-filtering caller.
	if strings.Contains(got, "code_execution") {
		t.Errorf("web tools should not invoke code execution (dynamic filtering):\n%s", got)
	}
	if n := strings.Count(got, `"name":"web_search"`); n != 1 {
		t.Errorf("expected exactly one web_search tool, got %d", n)
	}
}
