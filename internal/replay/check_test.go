package replay

import (
	"testing"
)

func msg(role string, blocks ...Block) Message { return Message{Role: role, Blocks: blocks} }
func text(s string) Block                      { return Block{Type: "text", Text: s} }
func use(id string) Block                      { return Block{Type: "tool_use", ID: id, Name: "Read"} }
func result(id string) Block                   { return Block{Type: "tool_result", ToolUseID: id, Result: "ok"} }

// Each rule fires on the shape it exists for, and a well-formed exchange
// trips none of them.
func TestCheckRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		msgs []Message
		want string // the one kind expected, "" for none
	}{
		{"well formed", []Message{msg("user", text("hi")), msg("assistant", use("a")), msg("user", result("a"))}, ""},
		{"orphan result", []Message{msg("user", text("hi")), msg("assistant", text("x")), msg("user", result("a"))}, "orphan-tool-result"},
		{"unanswered use", []Message{msg("user", text("hi")), msg("assistant", use("a")), msg("user", text("next"))}, "unanswered-tool-use"},
		{"two assistants", []Message{msg("user", text("hi")), msg("assistant", text("a")), msg("assistant", text("b")), msg("user", text("c"))}, "consecutive-assistant"},
		{"blank user text", []Message{msg("user", text("  "))}, "empty-text"},
		{"blank assistant text is the API's", []Message{msg("user", text("hi")), msg("assistant", text("")), msg("user", text("c"))}, ""},
		{"ends on assistant", []Message{msg("user", text("hi")), msg("assistant", text("a"))}, "last-not-user"},
	} {
		got := check(Request{Messages: tc.msgs})
		switch {
		case tc.want == "" && len(got) > 0:
			t.Errorf("%s: unexpected %v", tc.name, got)
		case tc.want != "" && (len(got) != 1 || got[0].Kind != tc.want):
			t.Errorf("%s: got %v, want one %s", tc.name, got, tc.want)
		}
	}
}

// A bad message stays in the conversation and is found again in every later
// request; it is reported once, with the count.
func TestCollapse(t *testing.T) {
	f := Finding{Turn: 1, Request: 1, Kind: "empty-text", Detail: "message 3 has an empty text block"}
	g := f
	g.Request = 2
	out := collapse([]Finding{f, g, g})
	if len(out) != 1 || out[0].Request != 1 || out[0].Detail != f.Detail+" (and in 2 later requests)" {
		t.Errorf("collapse = %+v", out)
	}
}
