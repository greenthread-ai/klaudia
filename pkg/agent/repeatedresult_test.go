package agent

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// paramMessage decodes a request-shaped message, the way a resumed transcript
// is rebuilt.
func paramMessage(t *testing.T, raw string) anthropic.BetaMessageParam {
	t.Helper()
	var m anthropic.BetaMessageParam
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

const repeatFetchID = "srvtoolu_repeat"

func fetchResultTurn(t *testing.T, toolUseID string) anthropic.BetaMessageParam {
	return paramMessage(t, `{"role":"assistant","content":[
		{"type":"web_fetch_tool_result","tool_use_id":"`+repeatFetchID+`","content":{
			"type":"web_fetch_result","url":"https://example.com/",
			"content":{"type":"document","source":{"type":"text","media_type":"text/plain","data":"page"}}}},
		{"type":"tool_use","id":"`+toolUseID+`","name":"Bash","input":{}}]}`)
}

func toolResultTurn(t *testing.T, toolUseID string) anthropic.BetaMessageParam {
	return paramMessage(t, `{"role":"user","content":[{"type":"tool_result","tool_use_id":"`+toolUseID+`","content":"ok"}]}`)
}

// Before historyParam, the live loop dropped each deferred fetch result as
// corrupt, so the API delivered it again on every later turn and the
// transcript kept every copy — one real session holds one call and ten
// results. Resuming it sent them all, the API paired the call with the first
// and rejected the second ("unexpected tool_use_id found in
// web_fetch_tool_result blocks"), and the session could not continue. Only
// the first copy may go out; everything else about the history is untouched.
func TestRepeatedServerToolResultsAreSentOnce(t *testing.T) {
	call := paramMessage(t, `{"role":"assistant","content":[
		{"type":"tool_use","id":"tu0","name":"Bash","input":{}},
		{"type":"server_tool_use","id":"`+repeatFetchID+`","name":"web_fetch","input":{"url":"https://example.com/"}}]}`)
	user := paramMessage(t, `{"role":"user","content":"go"}`)

	tests := []struct {
		name        string
		copies      int
		wantResults int
	}{
		{name: "a single deferred result is left alone", copies: 1, wantResults: 1},
		{name: "re-delivered copies collapse to the first", copies: 4, wantResults: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msgs := []anthropic.BetaMessageParam{user, call, toolResultTurn(t, "tu0")}
			for i := 0; i < tc.copies; i++ {
				id := "tu" + string(rune('1'+i))
				msgs = append(msgs, fetchResultTurn(t, id), toolResultTurn(t, id))
			}
			out := sanitizeMessages(msgs)

			if len(out) != len(msgs) {
				t.Fatalf("sanitize changed the message count %d -> %d", len(msgs), len(out))
			}
			results, firstAt, bashCalls := 0, -1, 0
			for i, m := range out {
				for _, b := range m.Content {
					if id, ok := serverToolResultID(b); ok && id == repeatFetchID {
						results++
						if firstAt < 0 {
							firstAt = i
						}
					}
					if b.OfToolUse != nil {
						bashCalls++
					}
				}
			}
			if results != tc.wantResults {
				t.Errorf("%d results for the fetch went out, want %d", results, tc.wantResults)
			}
			if firstAt != 3 {
				t.Errorf("the kept result is in message %d, want 3 (the first copy)", firstAt)
			}
			if bashCalls != tc.copies+1 {
				t.Errorf("%d client tool calls survived, want all %d", bashCalls, tc.copies+1)
			}
		})
	}
}
