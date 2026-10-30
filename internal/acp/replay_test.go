package acp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/plan"
)

// Helpers for building a transcript the way the loop records one.
func userText(s string) anthropic.BetaMessageParam {
	return anthropic.BetaMessageParam{
		Role:    anthropic.BetaMessageParamRoleUser,
		Content: []anthropic.BetaContentBlockParamUnion{{OfText: &anthropic.BetaTextBlockParam{Text: s}}},
	}
}

func assistantText(s string) anthropic.BetaMessageParam {
	return anthropic.BetaMessageParam{
		Role:    anthropic.BetaMessageParamRoleAssistant,
		Content: []anthropic.BetaContentBlockParamUnion{{OfText: &anthropic.BetaTextBlockParam{Text: s}}},
	}
}

func assistantToolUse(id, name string, input any) anthropic.BetaMessageParam {
	return anthropic.BetaMessageParam{
		Role: anthropic.BetaMessageParamRoleAssistant,
		Content: []anthropic.BetaContentBlockParamUnion{{
			OfToolUse: &anthropic.BetaToolUseBlockParam{ID: id, Name: name, Input: input},
		}},
	}
}

func userToolResult(id, text string, isErr bool) anthropic.BetaMessageParam {
	block := &anthropic.BetaToolResultBlockParam{ToolUseID: id}
	if text != "" {
		block.Content = []anthropic.BetaToolResultBlockParamContentUnion{
			{OfText: &anthropic.BetaTextBlockParam{Text: text}},
		}
	}
	if isErr {
		block.IsError = anthropic.Bool(true)
	}
	return anthropic.BetaMessageParam{
		Role:    anthropic.BetaMessageParamRoleUser,
		Content: []anthropic.BetaContentBlockParamUnion{{OfToolResult: block}},
	}
}

func TestReplay(t *testing.T) {
	tests := []struct {
		name      string
		history   []anthropic.BetaMessageParam
		wantShown int
		want      []string
	}{
		{
			name: "a conversation replays in wire order",
			// The client needs no second code path for a loaded session: the
			// updates are the same ones the live stream sends.
			history: []anthropic.BetaMessageParam{
				userText("what is in b.go?"),
				assistantToolUse("tu_1", "Read", map[string]any{"file_path": "/a/b.go"}),
				userToolResult("tu_1", "package main", false),
				assistantText("a package clause."),
			},
			wantShown: 4,
			want: []string{
				`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"what is in b.go?"}}`,
				`{"sessionUpdate":"tool_call","toolCallId":"tu_1","name":"Read",
				  "title":"Read /a/b.go","kind":"read","status":"pending",
				  "locations":[{"path":"/a/b.go"}],"rawInput":{"file_path":"/a/b.go"}}`,
				`{"sessionUpdate":"tool_call_update","toolCallId":"tu_1","status":"completed",
				  "content":[{"type":"content","content":{"type":"text","text":"package main"}}]}`,
				`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"a package clause."}}`,
			},
		},
		{
			name: "a recorded TodoWrite replays as a plan, result and all",
			// Suppression has to work across the replay too: the tool_result
			// arrives further down the same history and would otherwise patch
			// a call the client was never sent.
			history: []anthropic.BetaMessageParam{
				assistantToolUse("tu_1", "TodoWrite", map[string]any{"todos": []any{
					map[string]any{"content": "ship it", "status": "pending"},
				}}),
				userToolResult("tu_1", "1 todo", false),
			},
			wantShown: 1,
			want: []string{
				`{"sessionUpdate":"plan","entries":[
				  {"content":"ship it","priority":"medium","status":"pending"}]}`,
			},
		},
		{
			name: "a failed tool replays as failed",
			history: []anthropic.BetaMessageParam{
				assistantToolUse("tu_1", "Read", map[string]any{"file_path": "/nope"}),
				userToolResult("tu_1", "no such file", true),
			},
			wantShown: 2,
			want: []string{
				`{"sessionUpdate":"tool_call","toolCallId":"tu_1","name":"Read",
				  "title":"Read /nope","kind":"read","status":"pending",
				  "locations":[{"path":"/nope"}],"rawInput":{"file_path":"/nope"}}`,
				`{"sessionUpdate":"tool_call_update","toolCallId":"tu_1","status":"failed",
				  "content":[{"type":"content","content":{"type":"text","text":"no such file"}}]}`,
			},
		},
		{
			name: "an unfinished tool call stays pending",
			// The turn was interrupted and the call never completed. Marking it
			// failed would claim an error the transcript does not record.
			history: []anthropic.BetaMessageParam{
				assistantToolUse("tu_1", "Bash", map[string]any{"command": "sleep 60"}),
			},
			wantShown: 1,
			want: []string{
				`{"sessionUpdate":"tool_call","toolCallId":"tu_1","name":"Bash",
				  "title":"Bash sleep 60","kind":"execute","status":"pending",
				  "rawInput":{"command":"sleep 60"}}`,
			},
		},
		{
			name: "a Write in the history replays without a diff",
			// A diff needs the file as it was before the edit, and the only
			// moment those bytes exist is just before the tool runs. Reading
			// the file now and labelling it "before" would show the user a
			// diff of something that never happened.
			history: []anthropic.BetaMessageParam{
				assistantToolUse("tu_1", "Write", map[string]any{
					"file_path": "/a/b.go", "content": "package new\n",
				}),
			},
			wantShown: 1,
			want: []string{
				`{"sessionUpdate":"tool_call","toolCallId":"tu_1","name":"Write",
				  "title":"Write /a/b.go","kind":"edit","status":"pending",
				  "locations":[{"path":"/a/b.go"}],
				  "rawInput":{"file_path":"/a/b.go","content":"package new\n"}}`,
			},
		},
		{
			name: "a whitespace-only message says nothing",
			history: []anthropic.BetaMessageParam{
				assistantText("   \n"),
			},
			wantShown: 0,
			want:      nil,
		},
		{
			name: "an empty tool result sends a status with no content",
			// A tool that printed nothing still finished. Omitting the update
			// would leave the call pending forever.
			history: []anthropic.BetaMessageParam{
				assistantToolUse("tu_1", "Bash", map[string]any{"command": "true"}),
				userToolResult("tu_1", "", false),
			},
			wantShown: 2,
			want: []string{
				`{"sessionUpdate":"tool_call","toolCallId":"tu_1","name":"Bash",
				  "title":"Bash true","kind":"execute","status":"pending",
				  "rawInput":{"command":"true"}}`,
				`{"sessionUpdate":"tool_call_update","toolCallId":"tu_1","status":"completed"}`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var r recorder
			shown := newTranslator(&r, "sess_1").replay(tc.history, nil)
			if shown != tc.wantShown {
				t.Errorf("shown = %d, want %d", shown, tc.wantShown)
			}
			if len(r.updates) != len(tc.want) {
				t.Fatalf("got %d updates, want %d: %v", len(r.updates), len(tc.want), r.updates)
			}
			for i, want := range tc.want {
				if !jsonEqual(t, r.updates[i], want) {
					t.Errorf("update %d:\ngot  %v\nwant %s", i, r.updates[i], want)
				}
			}
		})
	}
}

func TestReplayDropsImagesFromToolResults(t *testing.T) {
	// A reopened session that took twenty screenshots would otherwise re-send
	// every one of them as base64, which is a slow, enormous update storm for
	// a conversation the user is only looking back at.
	history := []anthropic.BetaMessageParam{
		assistantToolUse("tu_1", "Bash", map[string]any{"command": "screencapture x.png"}),
		{
			Role: anthropic.BetaMessageParamRoleUser,
			Content: []anthropic.BetaContentBlockParamUnion{{
				OfToolResult: &anthropic.BetaToolResultBlockParam{
					ToolUseID: "tu_1",
					Content: []anthropic.BetaToolResultBlockParamContentUnion{
						{OfText: &anthropic.BetaTextBlockParam{Text: "[image: x.png]"}},
						{OfImage: &anthropic.BetaImageBlockParam{
							Source: anthropic.BetaImageBlockParamSourceUnion{
								OfBase64: &anthropic.BetaBase64ImageSourceParam{
									MediaType: anthropic.BetaBase64ImageSourceMediaType("image/png"),
									Data:      "AAAABBBBCCCC",
								},
							},
						}},
					},
				},
			}},
		},
	}
	var r recorder
	newTranslator(&r, "sess_1").replay(history, nil)
	if len(r.updates) != 2 {
		t.Fatalf("got %d updates, want 2: %v", len(r.updates), r.updates)
	}
	want := `{"sessionUpdate":"tool_call_update","toolCallId":"tu_1","status":"completed",
		"content":[{"type":"content","content":{"type":"text","text":"[image: x.png]"}}]}`
	if !jsonEqual(t, r.updates[1], want) {
		t.Errorf("got %v\nwant %s", r.updates[1], want)
	}
}

func TestReplayJoinsMultipleTextBlocksOfAResult(t *testing.T) {
	history := []anthropic.BetaMessageParam{
		assistantToolUse("tu_1", "Grep", map[string]any{"pattern": "x"}),
		{
			Role: anthropic.BetaMessageParamRoleUser,
			Content: []anthropic.BetaContentBlockParamUnion{{
				OfToolResult: &anthropic.BetaToolResultBlockParam{
					ToolUseID: "tu_1",
					Content: []anthropic.BetaToolResultBlockParamContentUnion{
						{OfText: &anthropic.BetaTextBlockParam{Text: "a.go:1"}},
						{OfText: &anthropic.BetaTextBlockParam{Text: "b.go:2"}},
					},
				},
			}},
		},
	}
	var r recorder
	newTranslator(&r, "sess_1").replay(history, nil)
	want := `{"sessionUpdate":"tool_call_update","toolCallId":"tu_1","status":"completed",
		"content":[{"type":"content","content":{"type":"text","text":"a.go:1\nb.go:2"}}]}`
	if !jsonEqual(t, r.updates[1], want) {
		t.Errorf("got %v\nwant %s", r.updates[1], want)
	}
}

// A loaded session showed the last TodoWrite in its history as the plan —
// the previous plan's list when a newer plan was approved without one — and
// posted every intermediate list on the way. The stored plan wins; a history
// with no stored plan falls back to its last list, posted once.
func TestLoadShowsTheStoredPlan(t *testing.T) {
	history := []anthropic.BetaMessageParam{
		assistantToolUse("tu_1", "TodoWrite", map[string]any{"todos": []any{map[string]any{"content": "first list", "status": "pending"}}}),
		userToolResult("tu_1", "ok", false),
		assistantToolUse("tu_2", "TodoWrite", map[string]any{"todos": []any{map[string]any{"content": "old plan step", "status": "completed"}}}),
		userToolResult("tu_2", "ok", false),
	}
	stored := plan.New("")
	_ = stored.ApproveNew("The newer plan.", []string{"new plan step"})

	tests := []struct {
		name  string
		store *plan.Store
		want  string
	}{
		{name: "the stored plan wins", store: stored, want: "new plan step"},
		{name: "no stored plan: the history's last list, once", store: plan.New(""), want: "old plan step"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var r recorder
			newTranslator(&r, "sess_1").replay(history, tc.store)
			var plans []string
			for _, u := range r.updates {
				j, _ := json.Marshal(u)
				if strings.Contains(string(j), `"sessionUpdate":"plan"`) {
					plans = append(plans, string(j))
				}
			}
			if len(plans) != 1 {
				t.Fatalf("posted %d plan updates, want 1: %v", len(plans), plans)
			}
			if !strings.Contains(plans[0], tc.want) {
				t.Errorf("plan = %s, want it to show %q", plans[0], tc.want)
			}
		})
	}
}
