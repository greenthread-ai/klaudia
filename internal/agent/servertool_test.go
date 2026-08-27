package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// webSearchResponse is an assistant turn as the API sends it, with a real
// web_search result. It is what the recorder used to mangle.
func webSearchResponse(t *testing.T) anthropic.BetaMessage {
	t.Helper()
	const j = `{"id":"m","type":"message","role":"assistant","model":"c","stop_reason":"end_turn",
	  "content":[
	    {"type":"text","text":"searching"},
	    {"type":"server_tool_use","id":"srv_1","name":"web_search","input":{"query":"gitea"}},
	    {"type":"web_search_tool_result","tool_use_id":"srv_1","content":[
	      {"type":"web_search_result","title":"T","url":"https://e.com","encrypted_content":"abc"}
	    ]}
	  ]}`
	var m anthropic.BetaMessage
	if err := json.Unmarshal([]byte(j), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// The root fix: what the recorder writes must round-trip back into a param and
// re-marshal as valid JSON — a web_search result's content stays an array, and
// no SDK union field name leaks. This is the exact path resume takes.
func TestRecordedAssistantRoundTripsForResume(t *testing.T) {
	rec := &captureBytes{}
	record(rec, "assistant", webSearchResponse(t))

	stored := rec.last()
	if strings.Contains(string(stored), "OfBetaWebSearchResultBlockArray") {
		t.Fatalf("recorded JSON leaks an SDK field name:\n%s", stored)
	}

	// Resume: stored JSON -> param -> re-marshal, as the loop does every turn.
	var p anthropic.BetaMessageParam
	if err := json.Unmarshal(stored, &p); err != nil {
		t.Fatalf("resume could not read the recorded message: %v", err)
	}
	out, err := json.Marshal(sanitizeMessages([]anthropic.BetaMessageParam{p})[0])
	if err != nil {
		t.Fatal(err)
	}
	// The content array must survive as an array.
	if !strings.Contains(string(out), `"web_search_result"`) {
		t.Errorf("the search result was lost on round-trip:\n%s", out)
	}
	if strings.Contains(string(out), "web_search_tool_result_error") {
		t.Errorf("a sound result collapsed into an error block:\n%s", out)
	}
}

// The defensive fix: a transcript already corrupted by the old recorder (the
// user's on-disk case) must not 400 on resume. brokenResultToolUseID detects
// the collapsed block, and sanitize replaces it.
func TestSanitizeRepairsACorruptedWebSearchResult(t *testing.T) {
	// The shape the old recorder produced, after resume parses it into a param:
	// a web_search_tool_result whose content is an empty error — no results,
	// no error code.
	const corrupted = `{"role":"assistant","content":[
	  {"type":"server_tool_use","id":"srv_1","name":"web_search","input":{"query":"x"}},
	  {"type":"web_search_tool_result","tool_use_id":"srv_1","content":{"type":"web_search_tool_result_error"}}
	]}`
	var p anthropic.BetaMessageParam
	if err := json.Unmarshal([]byte(corrupted), &p); err != nil {
		t.Fatal(err)
	}
	if _, broken := brokenResultToolUseID(p.Content[1]); !broken {
		t.Fatal("the corrupted result was not detected as broken")
	}

	got := sanitizeMessages([]anthropic.BetaMessageParam{p})
	out, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	// The broken result and its server_tool_use are gone; a note stands in.
	if strings.Contains(string(out), "web_search_tool_result") {
		t.Errorf("the broken result survived sanitize:\n%s", out)
	}
	if strings.Contains(string(out), "server_tool_use") {
		t.Errorf("the dangling server_tool_use survived:\n%s", out)
	}
	if !strings.Contains(string(out), "could not be restored") {
		t.Errorf("no placeholder was left in its place:\n%s", out)
	}
}

// A sound result must be left completely alone — the repair is only for the
// broken shape.
func TestSanitizeLeavesSoundServerToolResults(t *testing.T) {
	p := webSearchResponse(t).ToParam()
	if hasBrokenServerToolResult([]anthropic.BetaMessageParam{p}) {
		t.Fatal("a sound result was flagged as broken")
	}
	got := sanitizeMessages([]anthropic.BetaMessageParam{p})
	if len(got) != 1 || len(got[0].Content) != len(p.Content) {
		t.Errorf("sanitize altered a sound message: %d -> %d blocks", len(p.Content), len(got[0].Content))
	}
}

// A genuine error result (real error_code) is sound and must not be repaired —
// the model should see that the search failed.
func TestGenuineErrorResultIsKept(t *testing.T) {
	const withError = `{"role":"assistant","content":[
	  {"type":"web_search_tool_result","tool_use_id":"srv_1","content":{"type":"web_search_tool_result_error","error_code":"max_uses_exceeded"}}
	]}`
	var p anthropic.BetaMessageParam
	if err := json.Unmarshal([]byte(withError), &p); err != nil {
		t.Fatal(err)
	}
	if _, broken := brokenResultToolUseID(p.Content[0]); broken {
		t.Error("a real error result was treated as broken and would be dropped")
	}
}

// captureBytes keeps the actual bytes recorded, not just the roles.
type captureBytes struct{ rows [][]byte }

func (c *captureBytes) Record(role string, msg json.RawMessage) error {
	c.rows = append(c.rows, append([]byte(nil), msg...))
	return nil
}
func (c *captureBytes) last() []byte { return c.rows[len(c.rows)-1] }
