package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// slashModel is a model with a working directory, an event channel and a
// context, which is what the slash commands that start work need. HOME is
// pinned so nothing reaches the developer's config.
func slashModel(t *testing.T) *Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	m := newTestModel()
	m.sess.CWD = t.TempDir()
	m.ctx = context.Background()
	m.events = make(chan tea.Msg, 16)
	return m
}

// shown is the plain text of everything printed to scrollback so far.
func shown(m *Model) string { return stripANSI(m.transcript.String()) }

// awaitMsg reads the next message the agent goroutine sent, or fails.
func awaitMsg(t *testing.T, ch chan tea.Msg) tea.Msg {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("no message arrived on the event channel")
		return nil
	}
}

// scriptedRun is a RunFunc that records the prompts it was given and returns a
// fixed result. Reading prompts is only safe after the turn's doneMsg has been
// received, which orders it after the goroutine's write.
type scriptedRun struct {
	mu      sync.Mutex
	prompts []string
	res     agent.Result
	err     error
}

func (s *scriptedRun) run(_ context.Context, prompt string, _ []anthropic.BetaMessageParam,
	_ agent.Approver, _ tools.Asker, _ tools.Planner, _ agent.Emitter,
	_ func() agent.Interjection, _ func(string, []string)) (agent.Result, error) {
	s.mu.Lock()
	s.prompts = append(s.prompts, prompt)
	s.mu.Unlock()
	return s.res, s.err
}

func (s *scriptedRun) last() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.prompts) == 0 {
		return ""
	}
	return s.prompts[len(s.prompts)-1]
}

// gitRepo makes a real repository with one commit, isolated from any global
// git configuration that could sign or hook the commit.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
		{"config", "core.hooksPath", "/dev/null"},
	} {
		if out, err := gitOutput(dir, args...); err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
	}
	write(t, dir, "app.go", "original\n")
	if out, err := gitOutput(dir, "add", "-A"); err != nil {
		t.Fatalf("git add: %s %v", out, err)
	}
	if out, err := gitOutput(dir, "commit", "-qm", "init"); err != nil {
		t.Fatalf("git commit: %s %v", out, err)
	}
	return dir
}

func TestHelpListsCommandsKeysAndSkills(t *testing.T) {
	m := slashModel(t)
	m.sess.Skills = []SkillCommand{
		{Name: "deploy", Description: "Ship it"},
		{Name: "tidy"},
	}
	m.handleSlash("/help")
	out := shown(m)
	for _, want := range []string{"/commit <msg>", "Keys:", "Ctrl+J", "Skills:", "/deploy", "Ship it", "(user-defined skill)"} {
		if !strings.Contains(out, want) {
			t.Errorf("/help is missing %q:\n%s", want, out)
		}
	}
}

func TestQuitAndExitBothQuit(t *testing.T) {
	for _, c := range []string{"/quit", "/exit"} {
		m := slashModel(t)
		if _, cmd := m.handleSlash(c); !isQuit(cmd) {
			t.Errorf("%s did not quit", c)
		}
	}
}

func TestClearDropsTheConversationButNotMidTurn(t *testing.T) {
	m := slashModel(t)
	m.history = []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))}
	m.appendLine("an old line")

	m.state = stateRunning
	m.handleSlash("/clear")
	if len(m.history) != 1 {
		t.Fatal("/clear wiped history while a turn was in flight")
	}
	if !strings.Contains(shown(m), "isn't available while Klaudia is working") {
		t.Errorf("the refusal did not say why:\n%s", shown(m))
	}

	m.state = stateIdle
	_, cmd := m.handleSlash("/clear")
	if m.history != nil {
		t.Error("/clear kept the history")
	}
	if cmd == nil {
		t.Error("/clear should clear the screen")
	}
	out := shown(m)
	if strings.Contains(out, "an old line") {
		t.Errorf("the transcript still holds pre-clear output:\n%s", out)
	}
	if !strings.Contains(out, "Earlier output remains in terminal scrollback") {
		t.Errorf("the clear notice is missing:\n%s", out)
	}
}

func TestModelArgumentSetsTheModel(t *testing.T) {
	m := slashModel(t)
	m.handleSlash("/model my-custom-model")
	if m.sess.Model != "my-custom-model" {
		t.Errorf("Model = %q, want my-custom-model", m.sess.Model)
	}
}

func TestModelWithoutAListerReportsTheCurrentModel(t *testing.T) {
	m := slashModel(t)
	if _, cmd := m.handleSlash("/model"); cmd != nil {
		t.Error("a provider that cannot list models should not start a fetch")
	}
	if !strings.Contains(shown(m), "Model: (default)") {
		t.Errorf("no model and no default should say so:\n%s", shown(m))
	}

	m = slashModel(t)
	m.sess.ResolvedModel = "claude-resolved"
	m.handleSlash("/model")
	if !strings.Contains(shown(m), "Model: claude-resolved") {
		t.Errorf("the concrete default should be named:\n%s", shown(m))
	}
}

func TestModelListFailureOffersTypingTheName(t *testing.T) {
	m := slashModel(t)
	m.Update(modelsMsg{err: errors.New("endpoint unreachable")})
	out := shown(m)
	if !strings.Contains(out, "model:") || !strings.Contains(out, "/model <id>") {
		t.Errorf("a failed listing should point at the typed fallback:\n%s", out)
	}
}

func TestThemeCommand(t *testing.T) {
	t.Cleanup(func() { applyChromeTheme(defaultChromePalette) })

	m := slashModel(t)
	m.handleSlash("/theme no-such-theme")
	if !strings.Contains(shown(m), "unknown theme no-such-theme") {
		t.Errorf("an unknown theme was not reported:\n%s", shown(m))
	}

	m.handleSlash("/theme nord")
	if m.sess.Theme != "nord" || !strings.Contains(shown(m), "Theme: Nord") {
		t.Errorf("theme = %q, transcript:\n%s", m.sess.Theme, shown(m))
	}

	// The picker needs an idle session; mid-turn it points at the named form.
	m.state = stateRunning
	m.handleSlash("/theme")
	if m.state != stateRunning || !strings.Contains(shown(m), "use /theme <name>") {
		t.Errorf("mid-turn /theme should explain the named form, state=%v:\n%s", m.state, shown(m))
	}

	m.state = stateIdle
	m.handleSlash("/theme")
	if m.state != stateAwaitingChoice || len(m.choiceItems) == 0 {
		t.Fatalf("idle /theme should open a picker, state=%v", m.state)
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if m.state != stateIdle {
		t.Errorf("choosing a theme should close the picker, state=%v", m.state)
	}
	if m.sess.Theme != renderThemes[0].id {
		t.Errorf("Theme = %q, want the first listed %q", m.sess.Theme, renderThemes[0].id)
	}
}

func TestGoalStandingReminderSetAndClear(t *testing.T) {
	m := slashModel(t)
	m.handleSlash("/goal keep the API backwards compatible")
	if m.sess.Goal != "keep the API backwards compatible" {
		t.Fatalf("Goal = %q", m.sess.Goal)
	}
	if !strings.Contains(shown(m), "re-stated each turn") {
		t.Errorf("setting a goal should say what it does:\n%s", shown(m))
	}
	m.handleSlash("/goal clear")
	if m.sess.Goal != "" || !strings.Contains(shown(m), "Standing goal cleared") {
		t.Errorf("/goal clear left %q", m.sess.Goal)
	}
}

func TestGoalStandingReminderIsRestatedEachTurn(t *testing.T) {
	m := slashModel(t)
	sr := &scriptedRun{}
	m.run = sr.run
	m.sess.Goal = "ship v2"
	m.startTurn("fix the tests")
	awaitMsg(t, m.events)
	got := sr.last()
	if !strings.Contains(got, "Standing goal for this session: ship v2") || !strings.Contains(got, "fix the tests") {
		t.Errorf("the standing goal was not re-stated:\n%s", got)
	}
}

func TestGoalStop(t *testing.T) {
	m := slashModel(t)
	m.handleSlash("/goal stop")
	if !strings.Contains(shown(m), "No goal loop running") {
		t.Errorf("stopping nothing should say so:\n%s", shown(m))
	}

	cancelled := false
	m.loopRemaining, m.loopWrapUp = 3, true
	m.turnCancel = func() { cancelled = true }
	m.handleSlash("/goal stop")
	if !cancelled || m.turnCancel != nil {
		t.Error("/goal stop did not cancel the in-flight turn")
	}
	if m.loopRemaining != 0 || m.loopWrapUp {
		t.Errorf("loop state survived the stop: remaining=%d wrapUp=%v", m.loopRemaining, m.loopWrapUp)
	}
	if !strings.Contains(shown(m), "goal loop stopped") {
		t.Errorf("the stop was not reported:\n%s", shown(m))
	}
}

func TestGoalSettingToggles(t *testing.T) {
	m := slashModel(t)
	m.handleSlash("/goal")
	if !m.goalSetting {
		t.Fatal("/goal did not enter goal-setting")
	}
	if !strings.Contains(shown(m), "no spec yet") || !strings.Contains(shown(m), "GOAL.md") {
		t.Errorf("with no spec, /goal should offer to draft one:\n%s", shown(m))
	}
	m.handleSlash("/goal")
	if m.goalSetting || !strings.Contains(shown(m), "Goal-setting finished") {
		t.Errorf("a second /goal should finish goal-setting:\n%s", shown(m))
	}

	// With a spec on disk, it is loaded rather than drafted.
	write(t, m.sess.CWD, "PRD.md", "# Goal: build the widget\n\n- [ ] widget\n")
	m.handleSlash("/goal")
	if !strings.Contains(shown(m), "Loaded spec from") || !strings.Contains(shown(m), "PRD.md") {
		t.Errorf("an existing spec was not loaded:\n%s", shown(m))
	}

	m.goalSetting = false
	m.state = stateRunning
	m.handleSlash("/goal")
	if m.goalSetting {
		t.Error("/goal toggled goal-setting mid-turn")
	}
}

func TestGoalSettingFramesTheTurnAsSpecAuthoring(t *testing.T) {
	m := slashModel(t)
	sr := &scriptedRun{}
	m.run = sr.run
	m.goalSetting = true
	m.startTurn("a todo app")
	awaitMsg(t, m.events)
	if got := sr.last(); !strings.Contains(got, "User: a todo app") {
		t.Errorf("goal-setting turn was not framed:\n%s", got)
	}
}

func TestGoalRunNeedsASpecAndAValidCount(t *testing.T) {
	m := slashModel(t)
	m.handleSlash("/goal run")
	if !strings.Contains(shown(m), "No goal spec found") {
		t.Errorf("running without a spec should say how to make one:\n%s", shown(m))
	}
	if m.state == stateRunning {
		t.Error("a loop started without a spec")
	}

	write(t, m.sess.CWD, "PRD.md", "# Goal: widget\n\n## Progress\n\n- [ ] widget\n")
	for _, bad := range []string{"/goal run zero", "/goal run -2"} {
		m.handleSlash(bad)
	}
	if strings.Count(shown(m), "usage: /goal run [N]") != 2 {
		t.Errorf("bad iteration counts were not rejected:\n%s", shown(m))
	}
	if m.loopRemaining != 0 {
		t.Errorf("a loop started from a bad count: remaining=%d", m.loopRemaining)
	}

	m.state = stateRunning
	m.handleSlash("/goal run 2")
	if m.loopRemaining != 0 {
		t.Error("/goal run started a loop while a turn was in flight")
	}
}

func TestGoalRunBranchesCapsAndStartsTheFirstIteration(t *testing.T) {
	dir := gitRepo(t)
	m := slashModel(t)
	m.sess.CWD = dir
	sr := &scriptedRun{}
	m.run = sr.run
	write(t, dir, "PRD.md", "# Goal: build the widget\n\n## Progress\n\n- [ ] widget\n")

	_, cmd := m.handleSlash("/goal run 999")
	if cmd == nil {
		t.Fatal("/goal run returned no command to drive the turn")
	}
	awaitMsg(t, m.events)

	out := shown(m)
	if !strings.Contains(out, "capped at 50 iterations") {
		t.Errorf("an over-large count was not capped:\n%s", out)
	}
	if m.loopTotal != 50 || m.loopRemaining != 50 {
		t.Errorf("loop total/remaining = %d/%d, want 50/50", m.loopTotal, m.loopRemaining)
	}
	if m.loopBranch != "klaudia/goal-build-the-widget" || m.loopBaseBranch != "main" {
		t.Errorf("branch = %q base = %q", m.loopBranch, m.loopBaseBranch)
	}
	head, _ := gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if strings.TrimSpace(head) != m.loopBranch {
		t.Errorf("repository is on %q, want the goal branch", strings.TrimSpace(head))
	}
	if !strings.Contains(out, "iteration 1/50") || m.state != stateRunning {
		t.Errorf("the first iteration did not start (state=%v):\n%s", m.state, out)
	}
	if sr.last() == "" {
		t.Error("the agent was never run")
	}

	// A second run resumes on the existing branch rather than failing to
	// create it again.
	m.state = stateIdle
	m.handleSlash("/goal run 1")
	awaitMsg(t, m.events)
	if m.loopBranch != "klaudia/goal-build-the-widget" {
		t.Errorf("rerun did not reuse the branch: %q", m.loopBranch)
	}
	if m.loopBaseBranch != "" {
		t.Errorf("already on the goal branch, base should be unknown, got %q", m.loopBaseBranch)
	}
}

func TestGoalRunOutsideARepoRunsWithoutBranching(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	m := slashModel(t)
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(m.sess.CWD))
	sr := &scriptedRun{}
	m.run = sr.run
	write(t, m.sess.CWD, "PRD.md", "# Goal: widget\n\n## Progress\n\n- [ ] widget\n")
	m.handleSlash("/goal run 3")
	awaitMsg(t, m.events)
	if m.loopBranch != "" || !strings.Contains(shown(m), "not branching") {
		t.Errorf("outside a repo the loop should say it is not branching:\n%s", shown(m))
	}
	if m.loopRemaining != 3 {
		t.Errorf("loopRemaining = %d, want 3", m.loopRemaining)
	}
}

// fakeMCP is an MCPController that records what the picker asked for.
type fakeMCP struct {
	servers      []MCPServerInfo
	disconnected []string
	reconnected  []string
	err          error
}

func (f *fakeMCP) Servers() []MCPServerInfo { return f.servers }
func (f *fakeMCP) Reconnect(name string) error {
	f.reconnected = append(f.reconnected, name)
	return f.err
}
func (f *fakeMCP) Disconnect(name string) error {
	f.disconnected = append(f.disconnected, name)
	return f.err
}

func TestMCPWithoutServersSaysWhereToAddThem(t *testing.T) {
	m := slashModel(t)
	m.handleSlash("/mcp")
	if !strings.Contains(shown(m), "No MCP servers configured") {
		t.Errorf("an empty /mcp should say where servers go:\n%s", shown(m))
	}
	if m.state == stateAwaitingChoice {
		t.Error("an empty /mcp opened a picker")
	}
}

func TestMCPPickerDisconnectsAndReconnects(t *testing.T) {
	f := &fakeMCP{servers: []MCPServerInfo{
		{Name: "github", Connected: true, Tools: 12},
		{Name: "godot", Connected: false},
	}}
	m := slashModel(t)
	m.sess.MCP = f
	m.handleSlash("/mcp")
	out := shown(m)
	for _, want := range []string{"● connected  github (12 tools)", "○ disconnected  godot", "Disconnect github", "Reconnect godot"} {
		if !strings.Contains(out, want) {
			t.Errorf("/mcp is missing %q:\n%s", want, out)
		}
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if len(f.disconnected) != 1 || f.disconnected[0] != "github" {
		t.Errorf("choice 1 disconnected %v", f.disconnected)
	}
	if !strings.Contains(shown(m), "Disconnected github") {
		t.Errorf("the disconnect was not confirmed:\n%s", shown(m))
	}

	f.err = errors.New("spawn failed")
	m.handleSlash("/mcp")
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if len(f.reconnected) != 1 || !strings.Contains(shown(m), "reconnect failed: spawn failed") {
		t.Errorf("a failed reconnect was not reported (%v):\n%s", f.reconnected, shown(m))
	}

	m.handleSlash("/mcp")
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if !strings.Contains(shown(m), "disconnect failed: spawn failed") {
		t.Errorf("a failed disconnect was not reported:\n%s", shown(m))
	}

	f.err = nil
	m.handleSlash("/mcp")
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if !strings.Contains(shown(m), "Reconnected godot") {
		t.Errorf("a reconnect was not confirmed:\n%s", shown(m))
	}
}

func TestStatsAndStatus(t *testing.T) {
	m := slashModel(t)
	m.statTurns, m.statIn, m.statOut = 2, 1500, 300
	m.sess.ContextWindow, m.sess.ContextWindowSource = 200000, "model default"
	m.handleSlash("/stats")
	if !strings.Contains(shown(m), "turns=2  input_tokens=1500  output_tokens=300") ||
		!strings.Contains(shown(m), "model default") {
		t.Errorf("/stats:\n%s", shown(m))
	}

	m.sess.SessionID = "abc123"
	m.handleSlash("/status")
	out := shown(m)
	if !strings.Contains(out, "model=(default)") || !strings.Contains(out, "messages=0") {
		t.Errorf("/status:\n%s", out)
	}
	if !strings.Contains(out, "klaudia --resume abc123") {
		t.Errorf("/status should say how to resume:\n%s", out)
	}
}

func TestDeprecatedAllowAndDenyStillWork(t *testing.T) {
	m := slashModel(t)
	m.handleSlash("/allow")
	if !strings.Contains(shown(m), "usage: /allow <rule>") {
		t.Errorf("/allow with no rule:\n%s", shown(m))
	}
	m.handleSlash("/deny Bash(rm -rf")
	if !strings.Contains(shown(m), "invalid rule") {
		t.Errorf("an unparseable rule was accepted:\n%s", shown(m))
	}
	m.handleSlash("/allow Bash(git status)")
	m.handleSlash("/deny Bash(git push)")
	if len(m.sessionAllow) != 1 || len(m.sessionDeny) != 1 {
		t.Fatalf("allow=%v deny=%v", m.sessionAllow, m.sessionDeny)
	}
	out := shown(m)
	if !strings.Contains(out, "/allow is deprecated") || !strings.Contains(out, "/deny is deprecated") {
		t.Errorf("the deprecation hint is missing:\n%s", out)
	}
	if strings.Contains(out, "saved to .klaudia/config.toml") {
		t.Errorf("a project without .klaudia/ had a rule persisted:\n%s", out)
	}

	// The rules now answer matching permission asks without prompting.
	allowReply := make(chan permission.Decision, 1)
	m.Update(permissionMsg{req: agent.ApprovalRequest{ToolName: "Bash", Specifier: "git status"}, reply: allowReply})
	if d := <-allowReply; d.Behavior != permission.Allow {
		t.Errorf("session allow rule gave %v", d.Behavior)
	}
	denyReply := make(chan permission.Decision, 1)
	m.Update(permissionMsg{req: agent.ApprovalRequest{ToolName: "Bash", Specifier: "git push"}, reply: denyReply})
	if d := <-denyReply; d.Behavior != permission.Deny || d.Message != "denied by session rule" {
		t.Errorf("session deny rule gave %+v", d)
	}
	if m.state == stateAwaitingPermission {
		t.Error("a rule-answered ask still prompted the user")
	}
}

func TestAllowPersistsWhenTheProjectOptsIn(t *testing.T) {
	m := slashModel(t)
	if err := os.MkdirAll(filepath.Join(m.sess.CWD, ".klaudia"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.handleSlash("/allow Bash(go test ./...)")
	if !strings.Contains(shown(m), "saved to .klaudia/config.toml") {
		t.Errorf("the rule was not persisted:\n%s", shown(m))
	}
	data, err := os.ReadFile(filepath.Join(m.sess.CWD, ".klaudia", "config.toml"))
	if err != nil || !strings.Contains(string(data), "go test ./...") {
		t.Errorf("config.toml = %q, %v", data, err)
	}
}

func TestModeCommand(t *testing.T) {
	m := slashModel(t)
	m.handleSlash("/mode sideways")
	if !strings.Contains(shown(m), "unknown mode sideways") {
		t.Errorf("an invalid mode was accepted:\n%s", shown(m))
	}
	m.handleSlash("/mode autonomous")
	if m.sess.PermissionMode == string(permission.ModeAutonomous) {
		t.Error("autonomous was allowed without a host guardrail")
	}
	if !strings.Contains(shown(m), "autonomous needs the host guardrail") {
		t.Errorf("the refusal did not say why:\n%s", shown(m))
	}
	m.handleSlash("/mode plan")
	if m.sess.PermissionMode != string(permission.ModePlan) {
		t.Errorf("mode = %q, want plan", m.sess.PermissionMode)
	}

	m.handleSlash("/mode")
	if m.state != stateAwaitingChoice {
		t.Fatalf("/mode with no argument should open the picker, state=%v", m.state)
	}
	if !strings.Contains(shown(m), "(current)") {
		t.Errorf("the picker should mark the current mode:\n%s", shown(m))
	}
	// Autonomous is first and still refused through the picker.
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if m.sess.PermissionMode != string(permission.ModePlan) {
		t.Errorf("the picker bypassed the autonomous refusal: %q", m.sess.PermissionMode)
	}
	m.handleSlash("/mode")
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if m.sess.PermissionMode != string(permission.ModeBypassPermissions) {
		t.Errorf("picker choice 3 gave %q", m.sess.PermissionMode)
	}
}

func TestStopAsksForAGracefulHalt(t *testing.T) {
	m := slashModel(t)
	m.handleSlash("/stop")
	if !strings.Contains(shown(m), "isn't working on anything") {
		t.Errorf("/stop while idle:\n%s", shown(m))
	}
	if _, halt := m.steer.peek(); halt {
		t.Error("/stop while idle requested a halt")
	}
	m.state = stateRunning
	m.handleSlash("/stop")
	if _, halt := m.steer.peek(); !halt {
		t.Error("/stop mid-turn did not request a halt")
	}
	if in := m.steer.drain(); !in.Halt {
		t.Error("the agent would not see the halt")
	}
}

func TestPlanModeOnAndOff(t *testing.T) {
	m := slashModel(t)
	m.handleSlash("/plan")
	if m.sess.PermissionMode != string(permission.ModePlan) {
		t.Errorf("mode = %q, want plan", m.sess.PermissionMode)
	}
	m.handleSlash("/plan off")
	if m.sess.PermissionMode != string(permission.ModeDefault) {
		t.Errorf("mode = %q, want default", m.sess.PermissionMode)
	}
	if !strings.Contains(shown(m), "Left plan mode") {
		t.Errorf("leaving plan mode was not reported:\n%s", shown(m))
	}
}

func TestDoctorConfigAgentsAndContext(t *testing.T) {
	m := slashModel(t)
	m.handleSlash("/doctor")
	if !strings.Contains(shown(m), "doctor is not available") {
		t.Errorf("/doctor without a doctor:\n%s", shown(m))
	}
	m.sess.Doctor = func() string { return "all green" }
	m.handleSlash("/doctor")
	if !strings.Contains(shown(m), "all green") {
		t.Errorf("/doctor did not show the report:\n%s", shown(m))
	}

	m.handleSlash("/config")
	if !strings.Contains(shown(m), "provider=anthropic") || !strings.Contains(shown(m), "sandbox=local") ||
		!strings.Contains(shown(m), "model=(default)") {
		t.Errorf("/config defaults:\n%s", shown(m))
	}
	m.sess.Provider, m.sess.Model, m.sess.SandboxMode = "openai", "gpt-x", "container"
	m.handleSlash("/config")
	if !strings.Contains(shown(m), "provider=openai") || !strings.Contains(shown(m), "model=gpt-x") ||
		!strings.Contains(shown(m), "sandbox=container") {
		t.Errorf("/config overrides:\n%s", shown(m))
	}

	m.handleSlash("/agents")
	if !strings.Contains(shown(m), "No sub-agent types available") {
		t.Errorf("/agents with none:\n%s", shown(m))
	}
	m.sess.Agents = []AgentInfo{{Name: "explore", Description: "Read-only search"}}
	m.handleSlash("/agents")
	if !strings.Contains(shown(m), "explore") || !strings.Contains(shown(m), "Read-only search") {
		t.Errorf("/agents listing:\n%s", shown(m))
	}

	m.handleSlash("/context")
	if !strings.Contains(shown(m), filepath.Base(m.sess.CWD)) {
		t.Errorf("/context should say where Klaudia is working:\n%s", shown(m))
	}
}

func TestPinUnpinAndForgetCommands(t *testing.T) {
	m := slashModel(t)
	m.handleSlash("/pin")
	if !strings.Contains(shown(m), "Nothing pinned") {
		t.Errorf("/pin with nothing pinned:\n%s", shown(m))
	}
	write(t, m.sess.CWD, "real.go", "package x\n")
	m.handleSlash("/pin real.go")
	m.handleSlash("/pin later.go")
	out := shown(m)
	if !strings.Contains(out, "Pinned real.go. It will be re-stated") {
		t.Errorf("pinning an existing file:\n%s", out)
	}
	if !strings.Contains(out, "Pinned later.go (not found") {
		t.Errorf("pinning a missing file should warn:\n%s", out)
	}
	m.handleSlash("/pin real.go")
	if !strings.Contains(shown(m), "real.go is already pinned") {
		t.Errorf("re-pinning:\n%s", shown(m))
	}
	m.handleSlash("/pin")
	if !strings.Contains(shown(m), "Pinned:\n  later.go\n  real.go") {
		t.Errorf("/pin listing:\n%s", shown(m))
	}
	m.handleSlash("/unpin nope.go")
	if !strings.Contains(shown(m), "nope.go was not pinned") {
		t.Errorf("unpinning a stranger:\n%s", shown(m))
	}
	m.handleSlash("/unpin later.go")
	if !strings.Contains(shown(m), "Unpinned later.go.") || len(m.pinned) != 1 {
		t.Errorf("unpin left %v:\n%s", m.pinned, shown(m))
	}

	m.handleSlash("/forget")
	if !strings.Contains(shown(m), "usage: /forget <path>") {
		t.Errorf("/forget with no path:\n%s", shown(m))
	}
	m.handleSlash("/forget ghost.go")
	if !strings.Contains(shown(m), "ghost.go is not in the tracked context") {
		t.Errorf("/forget an untracked path:\n%s", shown(m))
	}
}

func TestAddDirCommand(t *testing.T) {
	m := slashModel(t)
	m.handleSlash("/add-dir")
	if !strings.Contains(shown(m), "No extra directories added") {
		t.Errorf("empty /add-dir:\n%s", shown(m))
	}
	m.handleSlash("/add-dir ../shared lib")
	if len(m.sess.ExtraDirs) != 1 || m.sess.ExtraDirs[0] != "../shared lib" {
		t.Errorf("ExtraDirs = %v", m.sess.ExtraDirs)
	}
	m.handleSlash("/add-dir")
	if !strings.Contains(shown(m), "Extra directories:\n  ../shared lib") {
		t.Errorf("/add-dir listing:\n%s", shown(m))
	}
}

func TestCompactCommandGuards(t *testing.T) {
	m := slashModel(t)
	m.state = stateRunning
	m.handleSlash("/compact")
	if !strings.Contains(shown(m), "/compact isn't available") {
		t.Errorf("mid-turn /compact:\n%s", shown(m))
	}
	m.state = stateIdle
	m.handleSlash("/compact")
	if !strings.Contains(shown(m), "compaction is not available") {
		t.Errorf("/compact without a compactor:\n%s", shown(m))
	}
	m.sess.Compact = func(context.Context, []anthropic.BetaMessageParam) ([]anthropic.BetaMessageParam, string, error) {
		t.Error("compacted an empty conversation")
		return nil, "", nil
	}
	m.handleSlash("/compact")
	if !strings.Contains(shown(m), "Nothing to compact yet") {
		t.Errorf("/compact with no history:\n%s", shown(m))
	}
}

func TestCompactReplacesHistoryWithTheSummary(t *testing.T) {
	m := slashModel(t)
	orig := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("one")),
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("two")),
	}
	short := orig[:1]
	m.history = orig
	var gotLen int
	m.sess.Compact = func(_ context.Context, h []anthropic.BetaMessageParam) ([]anthropic.BetaMessageParam, string, error) {
		gotLen = len(h)
		return short, "  we talked about one and two  ", nil
	}
	_, cmd := m.handleSlash("/compact")
	if cmd == nil || m.state != stateRunning {
		t.Fatalf("/compact should run in the background (state=%v)", m.state)
	}
	msg := awaitMsg(t, m.events)
	if gotLen != 2 {
		t.Errorf("compactor saw %d messages, want 2", gotLen)
	}
	m.Update(msg)
	if len(m.history) != 1 || m.state != stateIdle {
		t.Errorf("history len = %d state = %v", len(m.history), m.state)
	}
	if !strings.Contains(shown(m), "Summary:\nwe talked about one and two") {
		t.Errorf("the summary was not shown:\n%s", shown(m))
	}
}

func TestCompactFailureKeepsHistory(t *testing.T) {
	m := slashModel(t)
	m.history = []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("keep me"))}
	m.Update(compactDoneMsg{err: errors.New("model overloaded")})
	if len(m.history) != 1 {
		t.Error("a failed compaction dropped the history")
	}
	if !strings.Contains(shown(m), "compact:") || m.state != stateIdle {
		t.Errorf("failure not reported or state stuck (state=%v):\n%s", m.state, shown(m))
	}
}

func TestDiffCommand(t *testing.T) {
	dir := gitRepo(t)
	m := slashModel(t)
	m.sess.CWD = dir
	m.handleSlash("/diff")
	if !strings.Contains(shown(m), "No changes.") {
		t.Errorf("clean tree:\n%s", shown(m))
	}
	write(t, dir, "app.go", "changed\n")
	m.handleSlash("/diff")
	if !strings.Contains(shown(m), "+changed") || !strings.Contains(shown(m), "-original") {
		t.Errorf("/diff did not show the change:\n%s", shown(m))
	}
	m.handleSlash("/diff --no-such-flag")
	if !strings.Contains(shown(m), "git diff:") {
		t.Errorf("a git failure was not reported:\n%s", shown(m))
	}
}

func TestExportWritesTheConversation(t *testing.T) {
	m := slashModel(t)
	m.history = []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("how do I frobnicate?")),
	}
	m.handleSlash("/export")
	matches, _ := filepath.Glob(filepath.Join(m.sess.CWD, "klaudia-export-*.md"))
	if len(matches) != 1 {
		t.Fatalf("export files = %v", matches)
	}
	data, _ := os.ReadFile(matches[0])
	if !strings.Contains(string(data), "how do I frobnicate?") {
		t.Errorf("export is missing the conversation:\n%s", data)
	}
	if !strings.Contains(shown(m), "Exported transcript to "+matches[0]) {
		t.Errorf("the export path was not reported:\n%s", shown(m))
	}

	m.sess.CWD = filepath.Join(m.sess.CWD, "missing", "dir")
	m.handleSlash("/export")
	if !strings.Contains(shown(m), "export:") {
		t.Errorf("an unwritable export was not reported:\n%s", shown(m))
	}
}

func TestCommitCommandRefusals(t *testing.T) {
	m := slashModel(t)
	m.state = stateRunning
	m.handleSlash("/commit wip")
	if !strings.Contains(shown(m), "/commit isn't available") {
		t.Errorf("mid-turn /commit:\n%s", shown(m))
	}
	m.state = stateIdle
	m.handleSlash("/commit")
	if !strings.Contains(shown(m), "usage: /commit <message>") {
		t.Errorf("/commit with no message:\n%s", shown(m))
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(m.sess.CWD))
	m.handleSlash("/commit wip")
	if !strings.Contains(shown(m), "git:") {
		t.Errorf("/commit outside a repo:\n%s", shown(m))
	}

	dir := gitRepo(t)
	m.sess.CWD = dir
	m.base = newBaseline()
	m.handleSlash("/commit wip")
	if !strings.Contains(shown(m), "Nothing to commit (working tree clean)") {
		t.Errorf("/commit on a clean tree:\n%s", shown(m))
	}

	// Only the user's changes: nothing Klaudia may commit.
	write(t, dir, "app.go", "the user's edit\n")
	m.handleSlash("/commit wip")
	if !strings.Contains(shown(m), "Stage the changes you want with git add") {
		t.Errorf("/commit with only user changes:\n%s", shown(m))
	}
	if m.state == stateAwaitingConfirm {
		t.Error("/commit asked to commit the user's changes")
	}
}

func TestCommitCommitsKlaudiasChangesAfterConfirmation(t *testing.T) {
	dir := gitRepo(t)
	m := slashModel(t)
	m.sess.CWD = dir
	m.base = newBaseline()
	m.touched = map[string]bool{}

	write(t, dir, "new.go", "klaudia wrote this\n")
	m.noteTouched("new.go")
	write(t, dir, "scratch.txt", "the user's\n")

	m.handleSlash("/commit add new.go")
	if m.state != stateAwaitingConfirm {
		t.Fatalf("state = %v, want a confirmation", m.state)
	}
	out := shown(m)
	if !strings.Contains(out, "+ new.go") || !strings.Contains(out, "· scratch.txt") {
		t.Errorf("the plan does not show what is in and out:\n%s", out)
	}

	// "n" cancels and commits nothing.
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.state != stateIdle || m.confirmAction != nil {
		t.Fatalf("n did not cancel: state=%v", m.state)
	}
	if log, _ := gitOutput(dir, "log", "--oneline"); strings.Contains(log, "add new.go") {
		t.Fatal("a cancelled commit was made")
	}

	m.handleSlash("/commit add new.go")
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if !strings.Contains(shown(m), "Committed.") {
		t.Errorf("the commit was not confirmed:\n%s", shown(m))
	}
	log, _ := gitOutput(dir, "log", "--oneline", "-1", "--name-only")
	if !strings.Contains(log, "add new.go") || !strings.Contains(log, "new.go") {
		t.Errorf("last commit:\n%s", log)
	}
	if strings.Contains(log, "scratch.txt") {
		t.Errorf("the user's file was swept into the commit:\n%s", log)
	}
}

func TestSkillCommandRunsTheRenderedSkill(t *testing.T) {
	m := slashModel(t)
	sr := &scriptedRun{}
	m.run = sr.run
	var gotArgs string
	m.sess.Skills = []SkillCommand{{Name: "review", Render: func(a string) string {
		gotArgs = a
		return "Review this carefully: " + a
	}}}

	m.state = stateRunning
	m.handleSlash("/review main.go")
	if gotArgs != "" || !strings.Contains(shown(m), "/review isn't available") {
		t.Errorf("a skill started mid-turn:\n%s", shown(m))
	}

	m.state = stateIdle
	_, cmd := m.handleSlash("/review main.go utils.go")
	if cmd == nil || m.state != stateRunning {
		t.Fatalf("the skill did not start a turn (state=%v)", m.state)
	}
	awaitMsg(t, m.events)
	if gotArgs != "main.go utils.go" {
		t.Errorf("skill arguments = %q", gotArgs)
	}
	if !strings.Contains(sr.last(), "Review this carefully: main.go utils.go") {
		t.Errorf("the rendered skill was not sent: %q", sr.last())
	}
	if !strings.Contains(shown(m), "Running skill /review") {
		t.Errorf("the skill was not announced:\n%s", shown(m))
	}
}

func TestUnknownCommandPointsAtHelp(t *testing.T) {
	m := slashModel(t)
	m.handleSlash("/frobnicate now")
	if !strings.Contains(shown(m), "Unknown command /frobnicate. Try /help.") {
		t.Errorf("unknown command:\n%s", shown(m))
	}
}

func TestSlashCommandsWithoutTheirBackends(t *testing.T) {
	m := slashModel(t)
	m.handleSlash("/trust revoke all")
	m.handleSlash("/jobs")
	m.handleSlash("/logs api")
	m.handleSlash("/restart api")
	m.handleSlash("/stopjob api")
	out := shown(m)
	if !strings.Contains(out, "no host guardrail in this session") {
		t.Errorf("/trust without a gate:\n%s", out)
	}
	if n := strings.Count(out, "background jobs are not available"); n != 4 {
		t.Errorf("job commands without a store reported %d times, want 4:\n%s", n, out)
	}
	m.handleSlash("/logs stop")
	if !strings.Contains(shown(m), "not following anything") {
		t.Errorf("/logs stop with nothing followed:\n%s", shown(m))
	}
}
