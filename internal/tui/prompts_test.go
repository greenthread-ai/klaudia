package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

func runeKey(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// askPermission delivers a permission request the way the agent goroutine
// does and returns the channel its answer will arrive on.
func askPermission(m *Model, req agent.ApprovalRequest) chan permission.Decision {
	reply := make(chan permission.Decision, 1)
	m.Update(permissionMsg{req: req, reply: reply})
	return reply
}

func TestPermissionPromptDescribesTheAction(t *testing.T) {
	cases := []struct {
		name string
		req  agent.ApprovalRequest
		want []string
	}{
		{
			name: "edit shows the replacement",
			req: agent.ApprovalRequest{ToolName: "Edit", Input: rawJSON(t, map[string]string{
				"file_path": "api.go", "old_string": "return nil", "new_string": "return err",
			})},
			want: []string{"Permission required: edit api.go", "file: api.go", `replace "return nil" → "return err"`},
		},
		{
			name: "write names the file",
			req:  agent.ApprovalRequest{ToolName: "Write", Input: rawJSON(t, map[string]string{"file_path": "new.go"})},
			want: []string{"write new.go", "file: new.go"},
		},
		{
			name: "notebook edit names the notebook",
			req:  agent.ApprovalRequest{ToolName: "NotebookEdit", Input: rawJSON(t, map[string]string{"notebook_path": "a.ipynb"})},
			want: []string{"edit notebook a.ipynb", "notebook: a.ipynb"},
		},
		{
			name: "bash prefers the description and shows the command",
			req: agent.ApprovalRequest{ToolName: "Bash", Input: rawJSON(t, map[string]string{
				"command": "rm -rf build", "description": "clean the build dir",
			})},
			want: []string{"run command — clean the build dir", "command: rm -rf build"},
		},
		{
			name: "bash without a description names the command",
			req: agent.ApprovalRequest{ToolName: "Bash", Specifier: "make test",
				Input: rawJSON(t, map[string]string{"command": "make test"})},
			want: []string{"run command make test"},
		},
		{
			name: "other tools fall back to the label and suggestion",
			req:  agent.ApprovalRequest{ToolName: "WebFetch", Specifier: "example.com", Suggestion: "fetches a page"},
			want: []string{"WebFetch (example.com)", "fetches a page"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := slashModel(t)
			askPermission(m, tc.req)
			if m.state != stateAwaitingPermission {
				t.Fatalf("state = %v, want awaiting permission", m.state)
			}
			out := shown(m)
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q:\n%s", w, out)
				}
			}
			if !strings.Contains(m.permissionPrompt(), "(y)es once / (a)lways / (n)o") {
				t.Errorf("prompt = %q", m.permissionPrompt())
			}
		})
	}
}

func TestPermissionAnswers(t *testing.T) {
	req := agent.ApprovalRequest{ToolName: "Bash", Specifier: "go vet ./...",
		Input: rawJSON(t, map[string]string{"command": "go vet ./..."})}

	m := slashModel(t)
	reply := askPermission(m, req)
	m.onKey(runeKey("y"))
	if d := <-reply; d.Behavior != permission.Allow {
		t.Errorf("y gave %v", d.Behavior)
	}
	if m.state != stateRunning || !strings.Contains(shown(m), "→ allowed") {
		t.Errorf("state=%v:\n%s", m.state, shown(m))
	}

	reply = askPermission(m, req)
	m.onKey(runeKey("n"))
	if d := <-reply; d.Behavior != permission.Deny || d.Message != "denied by user" {
		t.Errorf("n gave %+v", d)
	}

	reply = askPermission(m, req)
	m.onKey(runeKey("x")) // not an answer: still waiting
	if m.state != stateAwaitingPermission {
		t.Fatal("an unrelated key answered the prompt")
	}
	m.onKey(runeKey("s")) // "something else" only exists for host changes
	if m.state != stateAwaitingPermission {
		t.Fatal("s answered an ordinary permission prompt")
	}
	m.onKey(runeKey("a"))
	if d := <-reply; d.Behavior != permission.Allow {
		t.Errorf("a gave %v", d.Behavior)
	}
	if len(m.sessionAllow) != 1 || !strings.Contains(shown(m), "always allow Bash(go vet ./...)") {
		t.Errorf("always-allow was not remembered (%v):\n%s", m.sessionAllow, shown(m))
	}
	// And the next identical ask is answered without the user.
	reply = askPermission(m, req)
	if d := <-reply; d.Behavior != permission.Allow {
		t.Errorf("remembered rule gave %v", d.Behavior)
	}
}

func hostChange() *agent.HostChange {
	return &agent.HostChange{
		Summary:  "Install and configure nginx",
		Reason:   "the task needs a reverse proxy",
		Zone:     trust.ZoneHost,
		Paths:    []string{"/etc/nginx"},
		Services: []string{"nginx"},
		Packages: []string{"nginx"},
		Declared: true,
	}
}

func TestHostChangeCardAndApproval(t *testing.T) {
	m := slashModel(t)
	reply := askPermission(m, agent.ApprovalRequest{ToolName: "RequestHostChange", HostChange: hostChange()})
	out := shown(m)
	for _, want := range []string{
		"This changes your machine", "Install and configure nginx", "why: the task needs a reverse proxy",
		"paths: /etc/nginx", "services: nginx", "packages: nginx", "for this session only",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("card is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Permission required") {
		t.Errorf("a host change was put as a tool question:\n%s", out)
	}
	if got := m.permissionPrompt(); got != "Change this machine? (y)es / (n)o / (s)omething else" {
		t.Errorf("prompt = %q", got)
	}

	// No standing permission for a host change.
	m.onKey(runeKey("a"))
	if m.state != stateAwaitingPermission || len(m.sessionAllow) != 0 {
		t.Fatal("'always' was accepted for a host change")
	}
	m.onKey(runeKey("y"))
	if d := <-reply; d.Behavior != permission.Allow {
		t.Errorf("y gave %v", d.Behavior)
	}
	if !strings.Contains(shown(m), "approved for this session — paths: /etc/nginx; services: nginx; packages: nginx") {
		t.Errorf("approval does not say its reach:\n%s", shown(m))
	}
}

func TestHostChangeDeclineAndRedirect(t *testing.T) {
	m := slashModel(t)
	reply := askPermission(m, agent.ApprovalRequest{ToolName: "Bash", HostChange: hostChange()})
	m.onKey(runeKey("n"))
	d := <-reply
	if d.Behavior != permission.Deny || !strings.Contains(d.Message, "declined this change to their machine") {
		t.Errorf("n gave %+v", d)
	}
	if !strings.Contains(shown(m), "declined — Klaudia will carry on without it") {
		t.Errorf("decline echo:\n%s", shown(m))
	}

	reply = askPermission(m, agent.ApprovalRequest{ToolName: "Bash", HostChange: hostChange()})
	m.onKey(runeKey("s"))
	d = <-reply
	if d.Behavior != permission.Deny || !strings.Contains(d.Message, "redirecting") {
		t.Errorf("s gave %+v", d)
	}
	if !strings.Contains(shown(m), "say what you'd like instead") {
		t.Errorf("redirect echo:\n%s", shown(m))
	}
	if m.hostRedirect {
		t.Error("the redirect flag outlived the answer it described")
	}
}

func TestCaughtHostChangeCard(t *testing.T) {
	hc := &agent.HostChange{
		Summary: "restart a service",
		Drift:   true,
		Effects: []trust.Effect{
			{Kind: trust.KindServiceControl, Res: trust.Resource{Class: "service", ID: "nginx"}, Certain: true},
			{Kind: trust.KindServiceControl, Res: trust.Resource{Class: "service", ID: "nginx"}, Certain: true},
		},
	}
	out := stripANSI(strings.Join(hostCardLines(hc), "\n"))
	if !strings.Contains(out, "This wasn't part of what you approved") {
		t.Errorf("drift heading:\n%s", out)
	}
	if !strings.Contains(out, "caught on the way past") {
		t.Errorf("an undeclared change should say it was caught:\n%s", out)
	}
	if strings.Count(out, "controls service nginx") != 1 {
		t.Errorf("duplicate effects should be listed once:\n%s", out)
	}
	if hostPrompt(hc) != "Approve this too? (y)es / (n)o / (s)omething else" {
		t.Errorf("drift prompt = %q", hostPrompt(hc))
	}
	if hostCardLines(nil) != nil {
		t.Error("a nil change rendered a card")
	}
	if got := hostAnswerLine(&agent.HostChange{}, true, false); got != "approved for this session" {
		t.Errorf("unscoped approval = %q", got)
	}
}

func TestHostGrantLineDescribesTheGrant(t *testing.T) {
	if hostGrantLine(nil) != "" {
		t.Error("a nil grant rendered a line")
	}
	g := &trust.Grant{ID: "g1", Summary: "configure nginx", Scope: trust.Scope{Services: []string{"nginx"}}}
	if got := hostGrantLine(g); !strings.HasPrefix(got, "approved · ") || !strings.Contains(got, "nginx") {
		t.Errorf("grant line = %q", got)
	}
}

func TestPlanApproval(t *testing.T) {
	m := slashModel(t)
	m.sess.PermissionMode = string(permission.ModePlan)
	reply := make(chan bool, 1)
	m.Update(planMsg{plan: "1. read\n2. write", reply: reply})
	if m.state != stateAwaitingPlan || !strings.Contains(shown(m), "Proposed plan:") {
		t.Fatalf("state=%v:\n%s", m.state, shown(m))
	}
	m.onKey(runeKey("q")) // not an answer
	if m.state != stateAwaitingPlan {
		t.Fatal("an unrelated key answered the plan")
	}
	m.onKey(runeKey("n"))
	if ok := <-reply; ok {
		t.Error("n approved the plan")
	}
	if m.sess.PermissionMode != string(permission.ModePlan) || !strings.Contains(shown(m), "staying in plan mode") {
		t.Errorf("declining should stay in plan mode (%q)", m.sess.PermissionMode)
	}

	reply = make(chan bool, 1)
	m.Update(planMsg{plan: "try again", reply: reply})
	m.onKey(runeKey("Y"))
	if ok := <-reply; !ok {
		t.Error("Y did not approve the plan")
	}
	if m.sess.PermissionMode != string(permission.ModeAcceptEdits) || m.state != stateRunning {
		t.Errorf("approval: mode=%q state=%v", m.sess.PermissionMode, m.state)
	}
}

func TestChoicePickerKeys(t *testing.T) {
	m := slashModel(t)
	applied := ""
	m.startChoice("Pick one:", []choiceItem{
		{label: "alpha", apply: func() string { applied = "alpha"; return "chose alpha" }},
		{label: "beta", apply: func() string { applied = "beta"; return "chose beta" }},
	})
	m.onKey(runeKey("7")) // past the list
	m.onKey(runeKey("z")) // not a digit
	if m.state != stateAwaitingChoice || applied != "" {
		t.Fatalf("an invalid key chose something: state=%v applied=%q", m.state, applied)
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.state != stateIdle || applied != "" || !strings.Contains(shown(m), "→ cancelled") {
		t.Fatalf("Esc did not cancel the picker: state=%v applied=%q", m.state, applied)
	}
	m.startChoice("Pick one:", []choiceItem{
		{label: "alpha", apply: func() string { applied = "alpha"; return "chose alpha" }},
		{label: "beta", apply: func() string { applied = "beta"; return "chose beta" }},
	})
	m.onKey(runeKey("2"))
	if applied != "beta" || !strings.Contains(shown(m), "→ chose beta") {
		t.Errorf("2 applied %q:\n%s", applied, shown(m))
	}
}

func TestConfirmIgnoresOtherKeys(t *testing.T) {
	m := slashModel(t)
	ran := false
	m.confirmAction = func() string { ran = true; return "done it" }
	m.setState(stateAwaitingConfirm)
	m.onKey(runeKey("q"))
	if ran || m.state != stateAwaitingConfirm {
		t.Fatal("an unrelated key answered the confirmation")
	}
	m.onKey(runeKey("y"))
	if !ran || !strings.Contains(shown(m), "done it") {
		t.Errorf("y did not run the action:\n%s", shown(m))
	}
}

func TestAskMsgListsOptionsAndTheEscapeHatch(t *testing.T) {
	m := slashModel(t)
	reply := make(chan string, 1)
	m.Update(askMsg{question: "Which DB?", reply: reply, options: []tools.AskOption{
		{Label: "Postgres", Description: "relational"},
		{Label: "SQLite"},
	}})
	out := shown(m)
	for _, want := range []string{"? Which DB?", "1) Postgres — relational", "2) SQLite", "3) " + otherAnswerLabel} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	m.onKey(runeKey("2"))
	if got := <-reply; got != "SQLite" {
		t.Errorf("answer = %q", got)
	}
}

func TestApproverAskerAndPlannerBridgeToTheUI(t *testing.T) {
	events := make(chan tea.Msg, 1)

	a := &uiApprover{events: events}
	done := make(chan permission.Decision, 1)
	go func() { done <- a.Approve(context.Background(), agent.ApprovalRequest{ToolName: "Bash"}) }()
	pm := (<-events).(permissionMsg)
	if pm.req.ToolName != "Bash" {
		t.Errorf("request = %+v", pm.req)
	}
	pm.reply <- permission.Decision{Behavior: permission.Allow}
	if d := <-done; d.Behavior != permission.Allow {
		t.Errorf("Approve returned %v", d.Behavior)
	}

	ask := &uiAsker{events: events}
	answer := make(chan string, 1)
	go func() {
		s, _ := ask.Ask(context.Background(), "pick", []tools.AskOption{{Label: "x"}})
		answer <- s
	}()
	am := (<-events).(askMsg)
	am.reply <- "x"
	if got := <-answer; got != "x" {
		t.Errorf("Ask returned %q", got)
	}

	p := &uiPlanner{events: events}
	approved := make(chan bool, 1)
	go func() {
		ok, _ := p.ExitPlan(context.Background(), "the plan")
		approved <- ok
	}()
	plm := (<-events).(planMsg)
	if plm.plan != "the plan" {
		t.Errorf("plan = %q", plm.plan)
	}
	plm.reply <- true
	if !<-approved {
		t.Error("ExitPlan did not return the approval")
	}
}

// A turn interrupted while parked on a prompt must be released, not hang.
func TestBridgesReturnWhenTheTurnIsCancelled(t *testing.T) {
	events := make(chan tea.Msg, 3)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if d := (&uiApprover{events: events}).Approve(ctx, agent.ApprovalRequest{}); d.Behavior != permission.Deny || d.Message != "cancelled" {
		t.Errorf("cancelled Approve = %+v", d)
	}
	if _, err := (&uiAsker{events: events}).Ask(ctx, "q", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled Ask err = %v", err)
	}
	if ok, err := (&uiPlanner{events: events}).ExitPlan(ctx, "p"); ok || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled ExitPlan = %v, %v", ok, err)
	}
}

func TestDoneMsgReportsInterruptErrorAndSuccess(t *testing.T) {
	m := slashModel(t)
	m.state = stateRunning
	m.Update(doneMsg{err: context.Canceled})
	if !strings.Contains(shown(m), "⊘ interrupted after") || m.state != stateIdle {
		t.Errorf("interrupt (state=%v):\n%s", m.state, shown(m))
	}

	m.Update(doneMsg{err: errors.New("upstream exploded")})
	if !strings.Contains(shown(m), "error: ") {
		t.Errorf("error:\n%s", shown(m))
	}

	hist := []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("q"))}
	m.turnLiveTurns, m.turnLiveIn, m.turnLiveOut = 1, 100, 10
	m.statTurns, m.statIn, m.statOut = 1, 100, 10
	m.phase, m.activeToolName = "running Bash", "Bash"
	m.Update(doneMsg{res: agent.Result{Text: "answer", NumTurns: 2, InputTokens: 300, OutputTokens: 40, Messages: hist}})
	if !strings.Contains(shown(m), "✓ done in") {
		t.Errorf("success:\n%s", shown(m))
	}
	if len(m.history) != 1 {
		t.Error("the turn's messages did not replace history")
	}
	// Live usage already counted is not counted twice.
	if m.statTurns != 2 || m.statIn != 300 || m.statOut != 40 {
		t.Errorf("stats = %d/%d/%d, want 2/300/40", m.statTurns, m.statIn, m.statOut)
	}
	if m.phase != "" || m.activeToolName != "" {
		t.Errorf("phase state survived the turn: %q %q", m.phase, m.activeToolName)
	}
}

func TestDoneMsgSendsAQueuedMessageAsTheNextTurn(t *testing.T) {
	m := slashModel(t)
	sr := &scriptedRun{}
	m.run = sr.run
	m.state = stateRunning
	m.steer.add("now add docs", "now add docs")
	m.Update(doneMsg{res: agent.Result{Text: "ok"}})
	if m.state != stateRunning {
		t.Fatalf("the queued message did not start a turn (state=%v)", m.state)
	}
	awaitMsg(t, m.events)
	if got := sr.last(); got != "now add docs" {
		t.Errorf("next turn prompt = %q", got)
	}
	if !strings.Contains(shown(m), "› now add docs") {
		t.Errorf("the queued message was not echoed:\n%s", shown(m))
	}
}

func TestInterruptResendTellsTheModelWhatIsStillRunning(t *testing.T) {
	m := slashModel(t)
	sr := &scriptedRun{}
	m.run = sr.run
	m.sess.Jobs = &fakeJobs{jobs: []tools.JobStatus{
		{ID: "bash_1", Name: "dev", Running: true, Port: "3000", Where: "container:api"},
		{ID: "bash_2", Name: "old", Running: false},
	}}
	m.interruptResend = agent.Interjection{Text: "use port 4000 instead"}
	m.Update(doneMsg{err: context.Canceled})
	awaitMsg(t, m.events)
	got := sr.last()
	if !strings.Contains(got, "dev (bash_1) on :3000 @container:api") {
		t.Errorf("running job not described:\n%s", got)
	}
	if strings.Contains(got, "old (bash_2)") {
		t.Errorf("an exited job was listed as running:\n%s", got)
	}
	if !strings.HasSuffix(got, "use port 4000 instead") {
		t.Errorf("the user's message should come last:\n%s", got)
	}
}

func TestDoneMsgRunsTheNextGoalIteration(t *testing.T) {
	m := slashModel(t)
	sr := &scriptedRun{}
	m.run = sr.run
	m.state = stateRunning
	m.loopTotal, m.loopRemaining = 3, 3
	m.loopSpecPath = writeSpec(t, "# Goal\n\n- [ ] thing\n")
	m.Update(doneMsg{res: agent.Result{Text: "progress", NumTurns: 1}})
	awaitMsg(t, m.events)
	if m.loopRemaining != 2 || m.state != stateRunning {
		t.Errorf("remaining=%d state=%v", m.loopRemaining, m.state)
	}
	if !strings.Contains(sr.last(), m.loopSpecPath) {
		t.Errorf("the next iteration prompt does not name the spec:\n%s", sr.last())
	}
}

func TestPagerDoneRemovesTheTempFileAndReportsErrors(t *testing.T) {
	m := slashModel(t)
	p := filepath.Join(t.TempDir(), "page.txt")
	write(t, filepath.Dir(p), "page.txt", "x")
	m.Update(pagerDoneMsg{path: p, err: errors.New("exit status 2")})
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("the pager's temp file was left behind")
	}
	if !strings.Contains(shown(m), "pager: exit status 2") {
		t.Errorf("pager failure:\n%s", shown(m))
	}
}

func TestJobExitAndMCPReloadArriveThroughUpdate(t *testing.T) {
	m := slashModel(t)
	_, cmd := m.Update(jobExitMsg{status: tools.JobStatus{Name: "api", ExitCode: 2}})
	if cmd == nil {
		t.Error("the event reader was not re-armed after a job exit")
	}
	m.Update(mcpReloadMsg{event: MCPReloadEvent{ConfigErr: "bad json"}})
	out := shown(m)
	if !strings.Contains(out, "job api exited with code 2") || !strings.Contains(out, "bad json") {
		t.Errorf("job exit / reload:\n%s", out)
	}
}

func TestEventMsgRendersAndRearms(t *testing.T) {
	m := slashModel(t)
	_, cmd := m.Update(eventMsg{ev: agent.Event{Type: "tool_use", ToolName: "Read",
		Input: rawJSON(t, map[string]string{"file_path": "main.go"})}})
	if cmd == nil {
		t.Error("the event reader was not re-armed")
	}
	if !strings.Contains(shown(m), "⚙ Read") {
		t.Errorf("tool use not rendered:\n%s", shown(m))
	}
}

func TestBeforeEditCheckpointsForUndo(t *testing.T) {
	m, dir := undoRepo(t)
	m.turnLabel = "rewrite app"
	m.beforeEdit("Edit", []string{"app.go"})
	write(t, dir, "app.go", "klaudia's version\n")
	m.noteTouched("app.go")

	m.handleSlash("/changes")
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "Klaudia") || !strings.Contains(out, "app.go") {
		t.Errorf("/changes did not attribute the edit:\n%s", out)
	}

	m.handleSlash("/undo")
	if m.state != stateAwaitingConfirm {
		t.Fatalf("/undo did not ask first (state=%v):\n%s", m.state, stripANSI(m.transcript.String()))
	}
	if !strings.Contains(stripANSI(m.transcript.String()), "rewrite app") {
		t.Errorf("/undo does not name the operation:\n%s", stripANSI(m.transcript.String()))
	}
	m.onKey(runeKey("y"))
	if got := read(t, dir, "app.go"); got != "original\n" {
		t.Errorf("app.go = %q after undo", got)
	}
	m.handleSlash("/undo")
	if !strings.Contains(stripANSI(m.transcript.String()), "Nothing to undo") {
		t.Errorf("a second /undo:\n%s", stripANSI(m.transcript.String()))
	}
}

func TestUndoRefusalsAndUserOnlyChanges(t *testing.T) {
	m, dir := undoRepo(t)
	m.state = stateRunning
	m.handleSlash("/undo")
	if !strings.Contains(stripANSI(m.transcript.String()), "/undo isn't available") {
		t.Errorf("mid-turn /undo:\n%s", stripANSI(m.transcript.String()))
	}
	m.state = stateIdle

	// Klaudia changed it, then the user did too: nothing is safe to undo.
	m.snapshotBefore("edit", []string{"app.go"})
	write(t, dir, "app.go", "klaudia\n")
	m.noteTouched("app.go")
	write(t, dir, "app.go", "klaudia\nplus the user\n")
	m.handleSlash("/undo")
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "Nothing safe to undo") || !strings.Contains(out, "app.go") {
		t.Errorf("user-touched undo:\n%s", out)
	}
	if m.state == stateAwaitingConfirm {
		t.Error("/undo offered to overwrite the user's edit")
	}

	m.sess.CWD = ""
	m.handleSlash("/undo")
	m.handleSlash("/changes")
	if n := strings.Count(stripANSI(m.transcript.String()), "no working directory"); n != 2 {
		t.Errorf("no-cwd refusals = %d", n)
	}
}

func TestChangesOnACleanTreeAndOutsideARepo(t *testing.T) {
	m, _ := undoRepo(t)
	m.handleSlash("/changes")
	if !strings.Contains(stripANSI(m.transcript.String()), "Working tree clean.") {
		t.Errorf("clean tree:\n%s", stripANSI(m.transcript.String()))
	}
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	m.sess.CWD = dir
	m.handleSlash("/changes")
	if !strings.Contains(stripANSI(m.transcript.String()), "git: ") {
		t.Errorf("outside a repo:\n%s", stripANSI(m.transcript.String()))
	}
}

func TestTurnSummaryCountsChangedLines(t *testing.T) {
	m, dir := undoRepo(t)
	m.turnTouched = map[string]bool{}
	write(t, dir, "app.go", "original\nadded one\nadded two\n")
	m.noteTouched("app.go")
	block := stripANSI(m.turnSummaryBlock())
	if !strings.Contains(block, "app.go") || !strings.Contains(block, "+2") {
		t.Errorf("summary block:\n%s", block)
	}
	if !strings.Contains(block, "/diff to review") {
		t.Errorf("summary block has no next step:\n%s", block)
	}
}

func TestNewWiresJobExitsAndMCPReloadsIntoTheEventLoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { applyChromeTheme(defaultChromePalette) })
	jobs := &fakeJobs{}
	var reload func(MCPReloadEvent)
	ft := &fakeTrust{policy: agent.HostEnforce}
	history := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaToolResultBlock("tu_1", "Started job dev (bash_1) on :3000.", false)),
	}
	m := New(context.Background(), (&scriptedRun{}).run, history, &Session{
		Jobs:        jobs,
		Trust:       ft,
		OnMCPReload: func(fn func(MCPReloadEvent)) { reload = fn },
	})

	out := shown(m)
	for _, want := range []string{"Resuming", "dev  stopped when the previous session ended", "host changes need your agreement"} {
		if !strings.Contains(out, want) {
			t.Errorf("resume banner is missing %q:\n%s", want, out)
		}
	}

	if jobs.onExit == nil || reload == nil {
		t.Fatal("New did not register the job-exit and MCP-reload listeners")
	}
	jobs.onExit(tools.JobStatus{Name: "dev", ExitCode: 1})
	if msg, ok := awaitMsg(t, m.events).(jobExitMsg); !ok || msg.status.Name != "dev" {
		t.Errorf("job exit arrived as %#v", msg)
	}
	reload(MCPReloadEvent{ConfigErr: "bad"})
	if msg, ok := awaitMsg(t, m.events).(mcpReloadMsg); !ok || msg.event.ConfigErr != "bad" {
		t.Errorf("reload arrived as %#v", msg)
	}

	// A full queue must not block the job's own goroutine.
	for i := 0; i < cap(m.events)+5; i++ {
		jobs.onExit(tools.JobStatus{Name: "flood"})
		reload(MCPReloadEvent{ConfigErr: "flood"})
	}
}

func TestNewInADirtyRepoWarnsAboutExistingChanges(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { applyChromeTheme(defaultChromePalette) })
	dir := gitRepo(t)
	write(t, dir, "app.go", "the user's work in progress\n")
	m := New(context.Background(), (&scriptedRun{}).run, nil, &Session{CWD: dir})
	out := shown(m)
	if strings.Contains(out, "Resuming") {
		t.Errorf("a fresh session showed the resume banner:\n%s", out)
	}
	if !strings.Contains(out, "1 file(s) were already modified before this session: app.go") {
		t.Errorf("uncommitted changes at startup were not mentioned:\n%s", out)
	}
	// The pre-existing change stays the user's.
	for _, f := range m.classify(" M app.go\n") {
		if f.Owner != ownerUser {
			t.Errorf("%s owner = %v, want the user", f.Path, f.Owner)
		}
	}
}
