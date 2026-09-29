package tools

import (
	"encoding/json"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/memory"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/sandbox"
	"github.com/greenthread-ai/klaudia/internal/tasks"
)

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

// Every tool falls into one of three intrinsic permission classes, and which
// one is a safety property: a read-only tool that started asking would be a
// nuisance, but a command runner that stopped asking would be a hole. Pin the
// class of every tool so a copy-pasted CheckPermissions cannot move a tool
// between them unnoticed.
func TestIntrinsicPermissionClassOfEveryTool(t *testing.T) {
	store := tasks.New()
	readOnly := []Tool{
		must[*Read](t)(NewRead()),
		must[*Glob](t)(NewGlob()),
		must[*Grep](t)(NewGrep()),
		must[*BashOutput](t)(NewBashOutput(nil)),
		must[*KillShell](t)(NewKillShell(nil)),
		must[*Jobs](t)(NewJobs(nil)),
		must[*RestartJob](t)(NewRestartJob(nil)),
		must[*TodoWrite](t)(NewTodoWrite(&TodoStore{})),
		must[*TaskCreate](t)(NewTaskCreate(store)),
		must[*TaskGet](t)(NewTaskGet(store)),
		must[*TaskList](t)(NewTaskList(store)),
		must[*TaskUpdate](t)(NewTaskUpdate(store)),
		must[*AskUserQuestion](t)(NewAskUserQuestion()),
		must[*ExitPlanMode](t)(NewExitPlanMode()),
		must[*RequestHostChange](t)(NewRequestHostChange()),
		must[*Agent](t)(NewAgent(nil, nil)),
		must[*Memory](t)(NewMemory(memory.New(t.TempDir()))),
		must[*ToolSearch](t)(NewToolSearch(nil)),
		must[*Diagnostics](t)(NewDiagnostics(nil)),
		must[Tool](t)(NewDefinition(nil)),
		must[Tool](t)(NewReferences(nil)),
		must[*Skill](t)(NewSkill([]SkillInfo{{Name: "s", Render: func(string) string { return "" }}})),
	}
	edits := []Tool{
		must[*Write](t)(NewWrite()),
		must[*Edit](t)(NewEdit()),
		must[*NotebookEdit](t)(NewNotebookEdit()),
	}
	execs := []Tool{
		must[*Bash](t)(NewBash(sandbox.NewLocal())),
	}

	type row struct {
		mode permission.Mode
		want permission.Behavior
	}
	classes := []struct {
		name  string
		tools []Tool
		rows  []row
	}{
		{"read-only", readOnly, []row{
			{permission.ModeDefault, permission.Allow},
			{permission.ModeAcceptEdits, permission.Allow},
			{permission.ModePlan, permission.Allow},
			{permission.ModeDontAsk, permission.Allow},
			{permission.ModeAutonomous, permission.Allow},
		}},
		{"edit", edits, []row{
			{permission.ModeDefault, permission.Ask},
			{permission.ModeAcceptEdits, permission.Allow},
			{permission.ModePlan, permission.Deny},
			{permission.ModeDontAsk, permission.Deny},
			{permission.ModeAutonomous, permission.Allow},
		}},
		{"exec", execs, []row{
			{permission.ModeDefault, permission.Ask},
			// acceptEdits is about file edits; it must not wave commands through.
			{permission.ModeAcceptEdits, permission.Ask},
			{permission.ModePlan, permission.Deny},
			{permission.ModeDontAsk, permission.Deny},
			{permission.ModeAutonomous, permission.Allow},
		}},
	}
	for _, c := range classes {
		for _, tool := range c.tools {
			for _, r := range c.rows {
				got := tool.CheckPermissions(permission.Context{Mode: permission.StaticMode(r.mode)}, permission.PermissionRequest{})
				if got.Behavior != r.want {
					t.Errorf("%s (%s) in %s: behavior = %q, want %q", tool.Name(), c.name, r.mode, got.Behavior, r.want)
				}
				if got.Behavior == permission.Deny && got.Message == "" {
					t.Errorf("%s in %s: a denial must say why", tool.Name(), r.mode)
				}
			}
		}
	}
}

func TestNetworkToolsAreAllowedWhenAutonomous(t *testing.T) {
	ws, _ := NewBrowserSearch(nil)
	got := ws.CheckPermissions(permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)}, permission.PermissionRequest{})
	if got.Behavior != permission.Allow {
		t.Errorf("autonomous: behavior = %q, want allow", got.Behavior)
	}
}

// The specifier is what allow/deny rules match against, so it has to be the
// thing a user would write a rule about: the file for edits, the command
// prefix for Bash, the URL or query for the web tools.
func TestPermissionRequestSpecifiers(t *testing.T) {
	cases := []struct {
		tool Tool
		raw  string
		want string
	}{
		{must[*Write](t)(NewWrite()), `{"file_path":"/p/a.txt","content":"x"}`, "/p/a.txt"},
		{must[*NotebookEdit](t)(NewNotebookEdit()), `{"notebook_path":"n.ipynb","new_source":""}`, "n.ipynb"},
		{must[*Bash](t)(NewBash(sandbox.NewLocal())), `{"command":"git status --short"}`, "git status"},
		{must[*BrowserNavigate](t)(NewBrowserNavigate(nil)), `{"url":"https://example.com/x"}`, "https://example.com/x"},
		{must[*BrowserFetch](t)(NewBrowserFetch(nil)), `{"url":"https://example.com/y"}`, "https://example.com/y"},
		{must[*BrowserSearch](t)(NewBrowserSearch(nil)), `{"query":"go generics"}`, "go generics"},
		{must[*Read](t)(NewRead()), `{"file_path":"/p/a.txt"}`, ""},
		{must[*Glob](t)(NewGlob()), `{"pattern":"*"}`, ""},
	}
	for _, c := range cases {
		got := c.tool.PermissionRequest(json.RawMessage(c.raw)).Specifier
		if got != c.want {
			t.Errorf("%s: specifier = %q, want %q", c.tool.Name(), got, c.want)
		}
	}
}

// A Bash rule written as a prefix must catch the command however it is
// spelled after the prefix, and a deny rule must beat the mode.
func TestBashRulesMatchTheParsedPrefix(t *testing.T) {
	b, _ := NewBash(sandbox.NewLocal())
	req := b.PermissionRequest(json.RawMessage(`{"command":"git push --force origin main"}`))

	deny := permission.Context{
		Mode: permission.StaticMode(permission.ModeAutonomous),
		Deny: []permission.Rule{{Tool: "Bash", Specifier: "git push:*"}},
	}
	if got := permission.Check(deny, b, req); got.Behavior != permission.Deny {
		t.Errorf("deny rule: behavior = %q, want deny", got.Behavior)
	}

	allow := permission.Context{Allow: []permission.Rule{{Tool: "Bash", Specifier: "git push:*"}}}
	if got := permission.Check(allow, b, req); got.Behavior != permission.Allow {
		t.Errorf("allow rule: behavior = %q, want allow", got.Behavior)
	}

	other := b.PermissionRequest(json.RawMessage(`{"command":"git status"}`))
	if got := permission.Check(allow, b, other); got.Behavior != permission.Ask {
		t.Errorf("unmatched command: behavior = %q, want ask", got.Behavior)
	}
}
