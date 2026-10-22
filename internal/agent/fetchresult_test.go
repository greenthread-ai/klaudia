package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// responseMessage decodes a response the way the stream accumulator hands it
// to the loop: as an anthropic.BetaMessage, not as a param.
func responseMessage(t *testing.T, raw string) anthropic.BetaMessage {
	t.Helper()
	var m anthropic.BetaMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// The shape of a real goal-setting session: the model fetched a page alongside
// a Bash call, so the API deferred the fetch result to the start of the next
// assistant message. The live loop put that message into history with
// ToParam(), which leaves a healthy web_fetch result with no
// OfRequestWebFetchResultBlock. The repair read it as corrupt, dropped the
// fetch along with the turn it arrived in, and told the user "an earlier web
// fetch was interrupted". With the turn gone the API delivered the fetch
// again, and the model asked the user's already-answered questions a second
// and third time. Replaying the transcript never showed this: replay decodes
// from JSON, and only the live path used ToParam() alone.
func TestDeferredFetchResultSurvivesTheLivePath(t *testing.T) {
	const fetchID = "srvtoolu_fetch1"
	callTurn := responseMessage(t, `{"id":"m1","type":"message","role":"assistant","model":"x",
		"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1},
		"content":[
			{"type":"tool_use","id":"tu1","name":"Probe","input":{}},
			{"type":"server_tool_use","id":"`+fetchID+`","name":"web_fetch","input":{"url":"https://example.com/"}}
		]}`)
	resultTurn := responseMessage(t, `{"id":"m2","type":"message","role":"assistant","model":"x",
		"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1},
		"content":[
			{"type":"web_fetch_tool_result","tool_use_id":"`+fetchID+`","content":{
				"type":"web_fetch_result","url":"https://example.com/","retrieved_at":"2026-10-22T00:00:00Z",
				"content":{"type":"document","source":{"type":"text","media_type":"text/plain","data":"page body"}}}},
			{"type":"tool_use","id":"tu2","name":"Probe","input":{}}
		]}`)
	endTurn := responseMessage(t, `{"id":"m3","type":"message","role":"assistant","model":"x",
		"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1},
		"content":[{"type":"text","text":"done"}]}`)

	// The SDK fact the fix rests on. If ToParam() ever starts filling the
	// field, historyParam is no longer needed, and this row says so.
	if _, broken := brokenResultToolUseID(resultTurn.ToParam().Content[0]); !broken {
		t.Log("note: ToParam() now keeps the web_fetch result typed; historyParam may be redundant")
	}
	if _, broken := brokenResultToolUseID(historyParam(resultTurn).Content[0]); broken {
		t.Fatal("historyParam still yields a fetch result the repair reads as corrupt")
	}

	var notices []string
	res, err := New(&scriptedProvider{turns: []anthropic.BetaMessage{callTurn, resultTurn, endTurn}},
		tools.NewRegistry(&probeTool{name: "Probe", log: &runLog{}})).Run(context.Background(), Options{
		Prompt:     "look at the page",
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
	}, func(e Event) {
		if e.Type == "notice" {
			notices = append(notices, e.Content)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(notices) != 0 {
		t.Errorf("a healthy fetch was announced as dropped: %q", notices)
	}
	kept := false
	for _, m := range res.Messages {
		for _, b := range m.Content {
			if r := b.OfWebFetchToolResult; r != nil && r.ToolUseID == fetchID {
				kept = true
			}
		}
	}
	if !kept {
		t.Error("the fetch result was dropped from the conversation")
	}
}
