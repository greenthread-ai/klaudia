package tools

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/lsp"
	"github.com/greenthread-ai/klaudia/internal/tasks"
)

// --- Skill ---

func newSkillTool(t *testing.T) *Skill {
	t.Helper()
	s, err := NewSkill([]SkillInfo{
		{Name: "review", Description: "Review a diff", Render: func(a string) string { return "Review: " + a }},
		{Name: "deploy", Description: "Ship it", Render: func(a string) string { return "Deploy " + a }},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSkillToolIsOmittedWithoutSkills(t *testing.T) {
	s, err := NewSkill(nil)
	if s != nil || err != nil {
		t.Errorf("NewSkill(nil) = %v, %v; want nil, nil so the caller skips registering it", s, err)
	}
}

func TestSkillDescriptionListsSkillsSorted(t *testing.T) {
	desc, _ := newSkillTool(t).Description(context.Background())
	d, r := strings.Index(desc, "- deploy: Ship it"), strings.Index(desc, "- review: Review a diff")
	if d < 0 || r < 0 || d > r {
		t.Errorf("description does not list both skills in name order:\n%s", desc)
	}
}

func TestSkillValidateAndExecute(t *testing.T) {
	s := newSkillTool(t)
	if err := s.ValidateInput(json.RawMessage(`{"name":"review"}`)); err != nil {
		t.Errorf("known skill rejected: %v", err)
	}
	err := s.ValidateInput(json.RawMessage(`{"name":"lint"}`))
	if err == nil || !strings.Contains(err.Error(), "available: deploy, review") {
		t.Errorf("unknown skill: err = %v, want the available names", err)
	}
	if err := s.ValidateInput(json.RawMessage(`{"name":" "}`)); err == nil {
		t.Error("blank name accepted")
	}

	res := runTool(t, s, Context{}, SkillInput{Name: "deploy", Arguments: "to staging"})
	if res.IsError || res.Content != "Deploy to staging" {
		t.Errorf("res = %+v, want the rendered body with arguments", res)
	}
	res = runTool(t, s, Context{}, SkillInput{Name: "lint"})
	if !res.IsError || res.Content != `No such skill "lint".` {
		t.Errorf("unknown at execute: %+v", res)
	}
}

// --- LSP tools ---

func lspTools(t *testing.T, pool *lsp.Pool) []Tool {
	d, err := NewDiagnostics(pool)
	if err != nil {
		t.Fatal(err)
	}
	def, err := NewDefinition(pool)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := NewReferences(pool)
	if err != nil {
		t.Fatal(err)
	}
	return []Tool{d, def, ref}
}

func TestLSPToolsWithoutAPool(t *testing.T) {
	for _, tool := range lspTools(t, nil) {
		res, err := tool.Execute(context.Background(), Context{}, json.RawMessage(`{"file":"a.go","line":1,"character":1}`))
		if err != nil || !res[0].IsError || res[0].Content != "language servers are not available" {
			t.Errorf("%s: res = %+v err = %v", tool.Name(), res, err)
		}
	}
}

// A file no server handles, a disabled language, and a server that is not
// installed each come back as an error the model can read — never a spawn.
func TestLSPToolsExplainAMissingServer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	cases := []struct {
		name string
		pool *lsp.Pool
		file string
		want string
	}{
		{"unhandled extension", lsp.NewPool(context.Background(), root, nil, nil), "notes.txt", `no language server configured for ".txt" files`},
		{"disabled language", lsp.NewPool(context.Background(), root, []string{"go"}, nil), "main.go", `no language server configured for ".go" files`},
		{"server not installed", lsp.NewPool(context.Background(), root, nil, nil), "main.go", "no go language server found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Cleanup(c.pool.Close)
			for _, tool := range lspTools(t, c.pool) {
				res, err := tool.Execute(context.Background(), Context{WorkingDir: root},
					json.RawMessage(`{"file":"`+c.file+`","line":3,"character":0}`))
				if err != nil {
					t.Fatal(err)
				}
				if !res[0].IsError || !strings.Contains(res[0].Content, c.want) {
					t.Errorf("%s: res = %+v, want %q", tool.Name(), res[0], c.want)
				}
			}
		})
	}
}

func TestLSPToolsValidateInput(t *testing.T) {
	tools := lspTools(t, nil)
	diag, loc := tools[0], tools[1]
	if err := diag.ValidateInput(json.RawMessage(`{"file":" "}`)); err == nil {
		t.Error("Diagnostics accepted a blank file")
	}
	if err := diag.ValidateInput(json.RawMessage(`{"file":"a.go"}`)); err != nil {
		t.Errorf("Diagnostics rejected a file: %v", err)
	}
	if err := loc.ValidateInput(json.RawMessage(`{"file":"a.go","line":0,"character":1}`)); err == nil {
		t.Error("Definition accepted line 0 (lines are 1-based)")
	}
	if err := loc.ValidateInput(json.RawMessage(`{"file":"a.go","line":4,"character":2}`)); err != nil {
		t.Errorf("Definition rejected a valid position: %v", err)
	}
	for _, tool := range tools {
		if desc, _ := tool.Description(context.Background()); desc == "" {
			t.Errorf("%s has no description", tool.Name())
		}
	}
}

// --- Agent / AskUserQuestion / ExitPlanMode ---

type failingSpawner struct{}

func (failingSpawner) Spawn(context.Context, string, string, func(string)) (string, error) {
	return "", errors.New("model unavailable")
}

func TestAgentHasTypeAndReportsSpawnFailure(t *testing.T) {
	a := newTestAgent(t, failingSpawner{})
	if !a.HasType("Explore") || a.HasType("Bash") {
		t.Error("HasType does not reflect the configured sub-agent types")
	}
	res := runTool(t, a, Context{}, AgentInput{SubagentType: "Explore", Prompt: "look"})
	if !res.IsError || res.Content != "Sub-agent failed: model unavailable" {
		t.Errorf("res = %+v", res)
	}
}

type errAsker struct{}

func (errAsker) Ask(context.Context, string, []AskOption) (string, error) {
	return "", errors.New("closed")
}

func TestAskUserQuestionAskerFailure(t *testing.T) {
	a := newAsk(t)
	res := runTool(t, a, Context{Ask: errAsker{}}, AskUserQuestionInput{Question: "q?", Options: []AskOption{{Label: "a"}}})
	if !res.IsError || res.Content != "Could not get an answer: closed" {
		t.Errorf("res = %+v", res)
	}
	if err := a.ValidateInput(json.RawMessage(`{"question":"q?","options":[{"label":"a"}]}`)); err != nil {
		t.Errorf("valid question rejected: %v", err)
	}
}

type errPlanner struct{}

func (errPlanner) ExitPlan(context.Context, string) (bool, error) { return false, errors.New("gone") }

func TestExitPlanApprovalFailure(t *testing.T) {
	e := newExitPlan(t)
	res := runTool(t, e, Context{Plan: errPlanner{}}, ExitPlanModeInput{Plan: "do it"})
	if !res.IsError || res.Content != "Plan approval failed: gone" {
		t.Errorf("res = %+v", res)
	}
	if err := e.ValidateInput(json.RawMessage(`{"plan":"p"}`)); err != nil {
		t.Errorf("valid plan rejected: %v", err)
	}
}

// --- TodoWrite / task tools / ToolSearch ---

func TestTodoWriteRendersEveryStatusAndClearing(t *testing.T) {
	store := &TodoStore{}
	tw, _ := NewTodoWrite(store)
	res := runTool(t, tw, Context{}, TodoInput{Todos: []TodoItem{
		{Content: "plan", Status: "completed"},
		{Content: "build", Status: "in_progress"},
		{Content: "ship", Status: "pending"},
	}})
	want := "Todos updated:\n[x] plan\n[~] build\n[ ] ship"
	if res.Content != want {
		t.Errorf("render =\n%s\nwant\n%s", res.Content, want)
	}

	res = runTool(t, tw, Context{}, TodoInput{Todos: []TodoItem{}})
	if res.Content != "Todo list cleared." || len(store.Items()) != 0 {
		t.Errorf("clearing: res = %q, store = %v", res.Content, store.Items())
	}
	if err := tw.ValidateInput(json.RawMessage(`{"todos":[{"content":"a","status":"pending"}]}`)); err != nil {
		t.Errorf("valid todo rejected: %v", err)
	}
}

func TestTaskToolsValidateAgainstSchema(t *testing.T) {
	store := tasks.New()
	create, _ := NewTaskCreate(store)
	get, _ := NewTaskGet(store)
	list, _ := NewTaskList(store)
	update, _ := NewTaskUpdate(store)
	cases := []struct {
		tool  Tool
		raw   string
		valid bool
	}{
		{create, `{"subject":"s","description":"d"}`, true},
		{create, `{}`, false},
		{get, `{"id":"task-1"}`, true},
		{get, `{}`, false},
		{list, `{}`, true},
		{update, `{"id":"task-1"}`, true}, // status is optional
		{update, `{"id":"task-1","status":"completed"}`, true},
		{update, `{}`, false},
	}
	for _, c := range cases {
		err := c.tool.ValidateInput(json.RawMessage(c.raw))
		if (err == nil) != c.valid {
			t.Errorf("%s %s: err = %v, want valid=%v", c.tool.Name(), c.raw, err, c.valid)
		}
	}
}

func TestToolSearchRejectsBlankQuery(t *testing.T) {
	ts := newToolSearch(t)
	if err := ts.ValidateInput(json.RawMessage(`{"query":"   "}`)); err == nil || !strings.Contains(err.Error(), "query is required") {
		t.Errorf("err = %v", err)
	}
	if ts.Name() != "ToolSearch" {
		t.Errorf("name = %q", ts.Name())
	}
	if desc, _ := ts.Description(context.Background()); desc == "" {
		t.Error("empty description")
	}
}

// --- Registry options ---

func TestDefaultRegistryUsesSuppliedResources(t *testing.T) {
	store := newTestJobStore(t)
	pool := lsp.NewPool(context.Background(), t.TempDir(), nil, nil)
	t.Cleanup(pool.Close)
	reg, err := DefaultRegistry(nil, WithJobStore(store), WithLSP(pool), WithBrowserEngine(nil))
	if err != nil {
		t.Fatal(err)
	}
	names := reg.Names()
	for _, want := range []string{"Diagnostics", "Definition", "References"} {
		if !slices.Contains(names, want) {
			t.Errorf("WithLSP did not register %s", want)
		}
	}
	// The job tools share the supplied store: a job started through the
	// registry's Bash is visible to its Jobs tool and to the caller's store.
	bash, _ := reg.Lookup("Bash")
	runTool(t, bash, Context{WorkingDir: t.TempDir()}, BashInput{Command: "sleep 30", RunInBackground: true})
	if n := len(store.List()); n != 1 {
		t.Fatalf("caller's store has %d jobs, want 1", n)
	}
	jobs, _ := reg.Lookup("Jobs")
	if res := runTool(t, jobs, Context{}, JobsInput{}); !strings.HasPrefix(res.Content, "bash_1  sleep") {
		t.Errorf("Jobs listing = %q", res.Content)
	}
}
