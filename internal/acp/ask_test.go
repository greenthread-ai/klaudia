package acp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/hostgate"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

// stubRequester answers permission requests with a canned outcome and records
// what it was asked.
type stubRequester struct {
	recorder
	asked   []requestPermissionParams
	outcome permissionOutcome
	err     error
}

func (s *stubRequester) requestPermission(_ context.Context, p requestPermissionParams) (permissionOutcome, error) {
	s.asked = append(s.asked, p)
	return s.outcome, s.err
}

func selected(id string) permissionOutcome {
	return permissionOutcome{Outcome: "selected", OptionID: id}
}

func TestApproverOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		outcome permissionOutcome
		err     error
		want    permission.Behavior
		wantMsg string // substring
		wantRun bool   // an in_progress patch was sent
	}{
		{
			name:    "allow",
			outcome: selected("allow"),
			want:    permission.Allow,
			wantRun: true,
		},
		{
			name:    "deny",
			outcome: selected("deny"),
			want:    permission.Deny,
			wantMsg: "did not approve",
		},
		{
			name: "the user dismissed the dialog",
			// ACP's "cancelled" outcome. Not an error — the client answered —
			// but not approval either.
			outcome: permissionOutcome{Outcome: "cancelled"},
			want:    permission.Deny,
			wantMsg: "did not approve",
		},
		{
			name: "the client cannot ask",
			// A client with no request_permission implementation, or a
			// cancelled turn. The message has to distinguish this from a
			// refusal: "nobody could ask you" and "you said no" lead the model
			// to different next steps.
			err:     errors.New("method not found"),
			want:    permission.Deny,
			wantMsg: "method not found",
		},
		{
			name:    "an option Klaudia never offered",
			outcome: selected("maybe"),
			want:    permission.Deny,
			wantMsg: "did not approve",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &stubRequester{outcome: tc.outcome, err: tc.err}
			a := &approver{req: req, sessionID: "sess_1"}
			got := a.Approve(context.Background(), agent.ApprovalRequest{
				ToolName:  "Bash",
				ToolUseID: "tu_1",
				Input:     json.RawMessage(`{"command":"rm -rf build"}`),
				Specifier: "rm",
			})
			if got.Behavior != tc.want {
				t.Errorf("behavior = %q, want %q", got.Behavior, tc.want)
			}
			if tc.wantMsg != "" && !strings.Contains(got.Message, tc.wantMsg) {
				t.Errorf("message = %q, want it to contain %q", got.Message, tc.wantMsg)
			}
			if gotRun := len(req.updates) > 0; gotRun != tc.wantRun {
				t.Errorf("in_progress patch sent = %v, want %v (updates: %v)", gotRun, tc.wantRun, req.updates)
			}
			if tc.wantRun {
				want := `{"sessionUpdate":"tool_call_update","toolCallId":"tu_1","status":"in_progress"}`
				if !jsonEqual(t, req.updates[0], want) {
					got, _ := json.Marshal(req.updates[0])
					t.Errorf("patch:\ngot  %s\nwant %s", got, want)
				}
			}
		})
	}
}

func TestApproverAsksAboutTheActualTool(t *testing.T) {
	req := &stubRequester{outcome: selected("allow")}
	a := &approver{req: req, sessionID: "sess_1"}
	a.Approve(context.Background(), agent.ApprovalRequest{
		ToolName:   "Bash",
		ToolUseID:  "tu_1",
		Input:      json.RawMessage(`{"command":"rm -rf build"}`),
		Specifier:  "rm",
		Suggestion: "rm deletes files irrecoverably",
	})
	if len(req.asked) != 1 {
		t.Fatalf("got %d requests, want 1", len(req.asked))
	}
	p := req.asked[0]
	if p.SessionID != "sess_1" {
		t.Errorf("sessionId = %q", p.SessionID)
	}
	if p.ToolCall.ToolCallID != "tu_1" {
		// Reusing the loop's tool_use id is what lets the client match the
		// dialog to the pending tool call it is already showing.
		t.Errorf("toolCallId = %q, want the tool_use id tu_1", p.ToolCall.ToolCallID)
	}
	if p.ToolCall.Title != "Bash rm" {
		t.Errorf("title = %q, want the specifier in it", p.ToolCall.Title)
	}
	if len(p.ToolCall.Content) != 1 || p.ToolCall.Content[0].Content.Text != "rm deletes files irrecoverably" {
		t.Errorf("the intrinsic suggestion did not reach the dialog: %+v", p.ToolCall.Content)
	}
	if len(p.Options) != 2 || p.Options[0].Kind != optAllowOnce || p.Options[1].Kind != optRejectOnce {
		t.Errorf("options = %+v, want one allow_once and one reject_once", p.Options)
	}
	if p.Meta != nil {
		t.Errorf("_meta = %v, want nothing for an ordinary tool ask", p.Meta)
	}
}

func TestApproverRendersAHostChange(t *testing.T) {
	// "Allow Bash?" is the wrong question to put to someone about
	// `systemctl restart nginx`. ACP has no host-change card, so the summary
	// becomes the title, the reason and scope become readable content, and the
	// structured form goes in _meta for a client that can do better.
	req := &stubRequester{outcome: selected("allow")}
	a := &approver{req: req, sessionID: "sess_1"}
	a.Approve(context.Background(), agent.ApprovalRequest{
		ToolName:  "RequestHostChange",
		ToolUseID: "tu_9",
		Detail: &hostgate.HostChange{
			Summary:  "Install nginx and configure it as a development proxy",
			Reason:   "the task needs a reverse proxy in front of the dev server",
			Zone:     trust.ZoneHost,
			Paths:    []string{"/etc/nginx/nginx.conf"},
			Packages: []string{"nginx"},
			Declared: true,
		},
	})
	p := req.asked[0]
	if p.ToolCall.Title != "Install nginx and configure it as a development proxy" {
		t.Errorf("title = %q, want the model's own summary", p.ToolCall.Title)
	}
	if len(p.ToolCall.Content) != 1 {
		t.Fatalf("content = %+v, want the reason and scope", p.ToolCall.Content)
	}
	body := p.ToolCall.Content[0].Content.Text
	for _, want := range []string{"reverse proxy", "/etc/nginx/nginx.conf", "nginx", "Why", "host"} {
		if !strings.Contains(body, want) {
			t.Errorf("body is missing %q:\n%s", want, body)
		}
	}
	if p.Options[0].Name == "Allow" {
		t.Error("the allow label should say what is being allowed, not just 'Allow'")
	}
	hc, ok := p.Meta[metaPrefix+"hostChange"].(map[string]any)
	if !ok {
		t.Fatalf("_meta is missing the structured host change: %v", p.Meta)
	}
	if hc["summary"] == "" || hc["declared"] != true {
		t.Errorf("_meta host change = %v", hc)
	}
	if _, present := hc["services"]; present {
		// An absent scope must stay absent: "no services" and "services it did
		// not say" are different facts.
		t.Errorf("_meta host change invented an empty services scope: %v", hc)
	}
}

func TestHostBodyNamesDrift(t *testing.T) {
	body := hostBody(&hostgate.HostChange{
		Summary: "also write /etc/hosts",
		Zone:    trust.ZoneHost,
		Drift:   true,
		Paths:   []string{"/etc/hosts"},
	})
	if !strings.Contains(body, "not part of what you approved") {
		t.Errorf("drift is not mentioned:\n%s", body)
	}
}

func TestAskerPutsTheQuestion(t *testing.T) {
	req := &stubRequester{outcome: selected("1")}
	a := &asker{req: req, sessionID: "sess_1"}
	options := []tools.AskOption{
		{Label: "Postgres", Description: "heavier, but we need JSONB"},
		{Label: "SQLite", Description: "zero setup, single file"},
	}
	got, err := a.Ask(context.Background(), "Which database should the service use?", options)
	if err != nil {
		t.Fatal(err)
	}
	if got != "SQLite" {
		t.Errorf("answer = %q, want SQLite", got)
	}
	p := req.asked[0]
	if p.ToolCall.Title != "Which database should the service use?" {
		t.Errorf("title = %q, want the question", p.ToolCall.Title)
	}
	if len(p.Options) != 2 || p.Options[0].Name != "Postgres" || p.Options[1].Name != "SQLite" {
		t.Fatalf("options = %+v", p.Options)
	}
	// PermissionOption has no description field in v1. Dropping the text would
	// hide what makes the options distinguishable, so it goes to both the
	// content block and _meta.
	body := p.ToolCall.Content[0].Content.Text
	if !strings.Contains(body, "zero setup, single file") {
		t.Errorf("descriptions did not reach the content block:\n%s", body)
	}
	if p.Options[1].Meta[metaPrefix+"description"] != "zero setup, single file" {
		t.Errorf("option _meta = %v", p.Options[1].Meta)
	}
}

func TestAskerRefusesToInventAnAnswer(t *testing.T) {
	// A fabricated answer is reported to the model as the user's choice, and it
	// is not. Every one of these has to be an error.
	options := []tools.AskOption{{Label: "Yes"}, {Label: "No"}}
	tests := []struct {
		name    string
		outcome permissionOutcome
		err     error
		options []tools.AskOption
	}{
		{name: "cancelled", outcome: permissionOutcome{Outcome: "cancelled"}, options: options},
		{name: "an index Klaudia never offered", outcome: selected("7"), options: options},
		{name: "a label instead of an index", outcome: selected("Yes"), options: options},
		{name: "the client could not ask", err: errors.New("no permission handler"), options: options},
		{name: "nothing to choose from", outcome: selected("0"), options: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &stubRequester{outcome: tc.outcome, err: tc.err}
			a := &asker{req: req, sessionID: "sess_1"}
			got, err := a.Ask(context.Background(), "Proceed?", tc.options)
			if err == nil {
				t.Fatalf("got answer %q, want an error", got)
			}
			if got != "" {
				t.Errorf("answer = %q, want empty alongside the error", got)
			}
		})
	}
}

func TestPlannerApproval(t *testing.T) {
	tests := []struct {
		name         string
		outcome      permissionOutcome
		err          error
		wantApproved bool
		wantErr      bool
		wantFlipped  bool
	}{
		{
			name:         "approved leaves plan mode",
			outcome:      selected("approve"),
			wantApproved: true,
			wantFlipped:  true,
		},
		{
			name:    "keep planning stays in plan mode",
			outcome: selected("keep_planning"),
		},
		{
			name:    "dismissing the dialog is not approval",
			outcome: permissionOutcome{Outcome: "cancelled"},
		},
		{
			name:    "a client that cannot ask is an error, not a refusal",
			err:     errors.New("no permission handler"),
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &stubRequester{outcome: tc.outcome, err: tc.err}
			flipped := false
			p := &planner{req: req, sessionID: "sess_1", approved: func() { flipped = true }}
			got, err := p.ExitPlan(context.Background(), "1. read the spec\n2. write the code")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.wantApproved {
				t.Errorf("approved = %v, want %v", got, tc.wantApproved)
			}
			if flipped != tc.wantFlipped {
				t.Errorf("mode flipped = %v, want %v", flipped, tc.wantFlipped)
			}
			if !tc.wantErr {
				call := req.asked[0].ToolCall
				if call.ToolKind != kindSwitchMode {
					t.Errorf("kind = %q, want %q", call.ToolKind, kindSwitchMode)
				}
				if len(call.Content) != 1 || !strings.Contains(call.Content[0].Content.Text, "read the spec") {
					t.Errorf("the plan itself did not reach the dialog: %+v", call.Content)
				}
			}
		})
	}
}

func TestAskIDsAreUnique(t *testing.T) {
	// AskUserQuestion and ExitPlanMode have no tool_use id of their own, so the
	// id is synthesised. A client uses it to match a dialog to what it is
	// showing, so two asks in one turn must not collide.
	req := &stubRequester{outcome: selected("0")}
	a := &asker{req: req, sessionID: "sess_1"}
	opts := []tools.AskOption{{Label: "ok"}}
	_, _ = a.Ask(context.Background(), "first?", opts)
	_, _ = a.Ask(context.Background(), "second?", opts)
	if req.asked[0].ToolCall.ToolCallID == req.asked[1].ToolCall.ToolCallID {
		t.Errorf("both asks used id %q", req.asked[0].ToolCall.ToolCallID)
	}
}
