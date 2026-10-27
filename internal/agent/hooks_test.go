package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/config"
	"github.com/greenthread-ai/klaudia/internal/hooks"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// hookProbeTool notes every call so a test can prove a blocked call did not run.
// "The model was told no" and "the tool did not execute" are different claims,
// and only the second one is the point of a PreToolUse hook.
type hookProbeTool struct {
	calls  int
	output string
}

func (r *hookProbeTool) Name() string                                { return "Probe" }
func (r *hookProbeTool) Description(context.Context) (string, error) { return "", nil }
func (r *hookProbeTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
}
func (r *hookProbeTool) ValidateInput(json.RawMessage) error { return nil }
func (r *hookProbeTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (r *hookProbeTool) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (r *hookProbeTool) Execute(context.Context, tools.Context, json.RawMessage) ([]tools.Result, error) {
	r.calls++
	out := r.output
	if out == "" {
		out = "probe ran"
	}
	return []tools.Result{{Content: out}}, nil
}

// userHooks builds a Runner with the hooks in the user's own config, so these
// tests exercise the loop's four call sites rather than the trust prompt.
func userHooks(t *testing.T, entries ...config.Hook) Hooks {
	t.Helper()
	r := hooks.New(t.TempDir(), "sess", entries, nil)
	r.Store = hooks.Store{Path: filepath.Join(t.TempDir(), "hooks.json")}
	return testHooks{r}
}

// testHooks mirrors the CLI's loopHooks (cli/hooks.go), which tests here
// cannot import.
type testHooks struct{ r *hooks.Runner }

func (h testHooks) Has(ev HookEvent) bool   { return h.r.Has(hooks.Event(ev)) }
func (h testHooks) ClaimSessionStart() bool { return h.r.ClaimSessionStart() }

// The trust prompt's card is hostgate's (cli/hooks.go); these tests use user
// hooks, which are never put to the user, so a plain request stands in for it.
func (h testHooks) Run(ctx context.Context, in HookInput, approver Approver) HookResult {
	var c hooks.Confirm
	if approver != nil {
		c = func(ctx context.Context, _ []hooks.Hook, file string) bool {
			return approver.Approve(ctx, ApprovalRequest{ToolName: "Hooks", Specifier: file}).Behavior == permission.Allow
		}
	}
	res := h.r.Run(ctx, hooks.Input{
		Event: hooks.Event(in.Event), CWD: in.CWD,
		ToolName: in.ToolName, ToolInput: in.ToolInput, ToolResult: in.ToolResult, ToolError: in.ToolError,
		Prompt: in.Prompt,
	}, c)
	return HookResult{Blocked: res.Blocked, Reason: res.Reason, Context: res.Context, Notices: res.Notices}
}

func dispatchProbe(t *testing.T, probe *hookProbeTool, opts Options, emit Emitter) anthropic.BetaContentBlockParamUnion {
	t.Helper()
	l := New(nil, tools.NewRegistry(probe))
	tu := anthropic.BetaToolUseBlock{ID: "t1", Name: "Probe", Input: map[string]any{"path": "x.go"}}
	return l.dispatch(context.Background(), tu, opts, emit, func(...string) {}, newFailureState())
}

func TestPreToolUseHookStopsTheCall(t *testing.T) {
	probe := &hookProbeTool{}
	opts := Options{Hooks: userHooks(t, config.Hook{
		Event:   "PreToolUse",
		Command: `echo "generated file, edit the template" >&2; exit 2`,
	})}

	got := toolResultText(t, dispatchProbe(t, probe, opts, nil))

	if probe.calls != 0 {
		t.Errorf("the tool ran %d times despite being blocked", probe.calls)
	}
	if !strings.Contains(got, "generated file, edit the template") {
		t.Errorf("result = %q, want the hook's reason", got)
	}
	// Named as a hook, or the model concludes the tool is broken and retries it.
	if !strings.Contains(got, "hook") {
		t.Errorf("result = %q, want it to say a hook was responsible", got)
	}
}

func TestPreToolUseHookSeesTheCall(t *testing.T) {
	payload := filepath.Join(t.TempDir(), "seen.json")
	probe := &hookProbeTool{}
	opts := Options{Hooks: userHooks(t, config.Hook{
		Event:   "PreToolUse",
		Matcher: "^Probe$",
		Command: `cat > ` + payload,
	})}

	dispatchProbe(t, probe, opts, nil)

	data, err := os.ReadFile(payload)
	if err != nil {
		t.Fatalf("the hook was not run: %v", err)
	}
	var in hooks.Input
	if err := json.Unmarshal(data, &in); err != nil {
		t.Fatalf("payload is not Input JSON: %v (%s)", err, data)
	}
	if in.ToolName != "Probe" {
		t.Errorf("tool_name = %q, want Probe", in.ToolName)
	}
	if !strings.Contains(string(in.ToolInput), `"path":"x.go"`) {
		t.Errorf("tool_input = %s, want the model's arguments verbatim", in.ToolInput)
	}
}

// A hook that lets the call through must leave no trace in the result. Otherwise
// every tool result in a session with hooks configured carries noise.
func TestAnAllowingHookChangesNothing(t *testing.T) {
	probe := &hookProbeTool{output: "the tool's own output"}
	opts := Options{Hooks: userHooks(t,
		config.Hook{Event: "PreToolUse", Command: "true"},
		config.Hook{Event: "PostToolUse", Command: "true"},
	)}

	got := toolResultText(t, dispatchProbe(t, probe, opts, nil))

	if got != "the tool's own output" {
		t.Errorf("result = %q, want the tool's output untouched", got)
	}
	if probe.calls != 1 {
		t.Errorf("the tool ran %d times, want once", probe.calls)
	}
}

func TestPostToolUseFeedbackReachesTheModel(t *testing.T) {
	probe := &hookProbeTool{output: "wrote 1 file"}
	opts := Options{Hooks: userHooks(t, config.Hook{
		Event:   "PostToolUse",
		Command: `echo "gofmt reformatted it"`,
	})}

	got := toolResultText(t, dispatchProbe(t, probe, opts, nil))

	if !strings.Contains(got, "wrote 1 file") {
		t.Errorf("result = %q, want the tool's own output kept", got)
	}
	if !strings.Contains(got, "gofmt reformatted it") {
		t.Errorf("result = %q, want the hook's feedback", got)
	}
	if strings.Index(got, "wrote 1 file") > strings.Index(got, "gofmt reformatted it") {
		t.Error("the hook's commentary came before the tool's own output")
	}
	if !strings.Contains(got, "PostToolUse hook") {
		t.Errorf("result = %q, want the feedback attributed, not passed off as the tool's", got)
	}
}

func TestPostToolUseCanFailASuccessfulCall(t *testing.T) {
	probe := &hookProbeTool{output: "wrote 1 file"}
	opts := Options{Hooks: userHooks(t, config.Hook{
		Event:   "PostToolUse",
		Command: `echo "lint: unused import" >&2; exit 2`,
	})}

	blk := dispatchProbe(t, probe, opts, nil)
	tr := blk.OfToolResult
	if tr == nil {
		t.Fatal("no tool_result block")
	}
	if !tr.IsError.Value {
		t.Error("the result is not an error; the model will believe the write was accepted")
	}
	if got := toolResultText(t, blk); !strings.Contains(got, "lint: unused import") {
		t.Errorf("result = %q, want the hook's reason", got)
	}
}

func TestPostToolUseSeesTheResult(t *testing.T) {
	payload := filepath.Join(t.TempDir(), "seen.json")
	probe := &hookProbeTool{output: "wrote 1 file"}
	opts := Options{Hooks: userHooks(t, config.Hook{
		Event:   "PostToolUse",
		Command: `cat > ` + payload,
	})}

	dispatchProbe(t, probe, opts, nil)

	data, err := os.ReadFile(payload)
	if err != nil {
		t.Fatalf("the hook was not run: %v", err)
	}
	var in hooks.Input
	if err := json.Unmarshal(data, &in); err != nil {
		t.Fatalf("payload is not Input JSON: %v", err)
	}
	if in.ToolResult != "wrote 1 file" {
		t.Errorf("tool_response = %q, want the result the model is being given", in.ToolResult)
	}
}

// A hook that could not run is the operator's problem. Telling the model its
// environment is broken invites it to work around the breakage.
func TestAMalfunctioningHookIsReportedToTheUserOnly(t *testing.T) {
	probe := &hookProbeTool{output: "the tool's own output"}
	var notices []string
	emit := func(ev Event) {
		if ev.Type == "notice" {
			notices = append(notices, ev.Content)
		}
	}
	opts := Options{Hooks: userHooks(t, config.Hook{
		Event:   "PreToolUse",
		Command: `echo "formatter: not found" >&2; exit 127`,
	})}

	got := toolResultText(t, dispatchProbe(t, probe, opts, emit))

	if probe.calls != 1 {
		t.Errorf("the tool ran %d times; a broken hook must not block it", probe.calls)
	}
	if strings.Contains(got, "formatter") {
		t.Errorf("result = %q, want no sign of the hook's malfunction", got)
	}
	if !strings.Contains(strings.Join(notices, "\n"), "formatter: not found") {
		t.Errorf("notices = %v, want the malfunction surfaced to the user", notices)
	}
}

// The permission check and the host gate run first, so a hook is only ever asked
// about a call Klaudia was otherwise going to make. A hook that could see a
// refused call could be used to launder one.
func TestPreToolUseDoesNotSeeARefusedCall(t *testing.T) {
	fired := filepath.Join(t.TempDir(), "fired")
	probe := &hookProbeTool{}
	opts := Options{
		Hooks: userHooks(t, config.Hook{Event: "PreToolUse", Command: `touch ` + fired}),
		Permission: permission.Context{
			Mode: func() permission.Mode { return permission.ModePlan },
		},
	}
	// Plan mode denies a mutating tool; Probe is not one, so deny it explicitly
	// by giving the loop an Approver that refuses.
	opts.Approver = ApproverFunc(func(context.Context, ApprovalRequest) permission.Decision {
		return permission.Decision{Behavior: permission.Deny, Message: "not in plan mode"}
	})

	l := New(nil, tools.NewRegistry(&denyingTool{}))
	tu := anthropic.BetaToolUseBlock{ID: "t1", Name: "Denied", Input: map[string]any{}}
	l.dispatch(context.Background(), tu, opts, nil, func(...string) {}, newFailureState())

	if _, err := os.Stat(fired); err == nil {
		t.Error("a PreToolUse hook ran for a call the permission check had already refused")
	}
	if probe.calls != 0 {
		t.Error("the probe ran")
	}
}

// denyingTool always refuses at the permission check.
type denyingTool struct{}

func (denyingTool) Name() string                                { return "Denied" }
func (denyingTool) Description(context.Context) (string, error) { return "", nil }
func (denyingTool) InputSchema() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (denyingTool) ValidateInput(json.RawMessage) error         { return nil }
func (denyingTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (denyingTool) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Deny, Message: "no"}
}
func (denyingTool) Execute(context.Context, tools.Context, json.RawMessage) ([]tools.Result, error) {
	return []tools.Result{{Content: "should not run"}}, nil
}

func TestUserPromptSubmitAddsContextBeforeThePrompt(t *testing.T) {
	provider := &scriptedProvider{}
	l := New(provider, tools.NewRegistry())
	res, err := l.Run(context.Background(), Options{
		Model:      "test",
		Prompt:     "fix the test",
		MaxTurns:   2,
		Permission: bypassPerm(),
		Hooks:      userHooks(t, config.Hook{Event: "UserPromptSubmit", Command: `echo "on branch main"`}),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	blocks := userBlocks(t, res.Messages)
	if len(blocks) != 2 {
		t.Fatalf("user message has %d text blocks, want the context and the prompt as separate blocks", len(blocks))
	}
	if !strings.Contains(blocks[0], "on branch main") {
		t.Errorf("first block = %q, want the hook's context", blocks[0])
	}
	if blocks[1] != "fix the test" {
		t.Errorf("second block = %q, want exactly what the user typed", blocks[1])
	}
}

func TestUserPromptSubmitCanStopTheTurn(t *testing.T) {
	provider := &scriptedProvider{}
	l := New(provider, tools.NewRegistry())
	var notices []string
	emit := func(ev Event) {
		if ev.Type == "notice" {
			notices = append(notices, ev.Content)
		}
	}
	res, err := l.Run(context.Background(), Options{
		Model:      "test",
		Prompt:     "deploy to production",
		MaxTurns:   2,
		Permission: bypassPerm(),
		Hooks: userHooks(t, config.Hook{
			Event:   "UserPromptSubmit",
			Command: `echo "not from this branch" >&2; exit 2`,
		}),
	}, emit)
	if err != nil {
		t.Fatal(err)
	}

	if provider.n != 0 {
		t.Errorf("the model was called %d times; a blocked prompt must not be sent", provider.n)
	}
	if res.StopReason != "blocked_by_hook" {
		t.Errorf("StopReason = %q, want blocked_by_hook", res.StopReason)
	}
	if !strings.Contains(res.Text, "not from this branch") {
		t.Errorf("Text = %q, want the hook's reason", res.Text)
	}
	if !strings.Contains(strings.Join(notices, "\n"), "not from this branch") {
		t.Errorf("notices = %v, want the user told why their message was dropped", notices)
	}
}

// Run is called once per user turn, not once per session. SessionStart keying off
// "the first Run" would fire again on every message.
func TestSessionStartFiresOncePerSession(t *testing.T) {
	log := filepath.Join(t.TempDir(), "starts")
	runner := userHooks(t, config.Hook{Event: "SessionStart", Command: `echo start >> ` + log})

	for range 3 {
		l := New(&scriptedProvider{}, tools.NewRegistry())
		if _, err := l.Run(context.Background(), Options{
			Model:      "test",
			Prompt:     "hello",
			MaxTurns:   2,
			Permission: bypassPerm(),
			Hooks:      runner,
		}, nil); err != nil {
			t.Fatal(err)
		}
	}

	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the SessionStart hook never ran: %v", err)
	}
	if got := string(data); got != "start\n" {
		t.Errorf("SessionStart fired %q, want once across three turns", got)
	}
}

// A sub-agent's prompt was written by the model. A hook that pastes the current
// ticket in front of what the user typed has nothing to attach itself to.
func TestSubAgentRunsToolHooksButNotPromptHooks(t *testing.T) {
	prompts := filepath.Join(t.TempDir(), "prompts")
	runner := userHooks(t, config.Hook{Event: "UserPromptSubmit", Command: `echo fired >> ` + prompts})

	l := New(&scriptedProvider{}, tools.NewRegistry())
	if _, err := l.Run(context.Background(), Options{
		Model:      "test",
		Prompt:     "research the API",
		MaxTurns:   2,
		Permission: bypassPerm(),
		Hooks:      runner,
		SubAgent:   true,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(prompts); err == nil {
		t.Error("a UserPromptSubmit hook fired for a sub-agent's model-written prompt")
	}

	// The tool half does fire, or a formatter has an invisible exception.
	probe := &hookProbeTool{}
	fired := filepath.Join(t.TempDir(), "tool")
	opts := Options{
		SubAgent: true,
		Hooks:    userHooks(t, config.Hook{Event: "PostToolUse", Command: `touch ` + fired}),
	}
	dispatchProbe(t, probe, opts, nil)
	if _, err := os.Stat(fired); err != nil {
		t.Error("a PostToolUse hook did not fire for a sub-agent's tool call")
	}
}

// With no hooks configured the loop must behave exactly as before, and must not
// pay for the feature.
func TestNoHooksConfiguredIsInert(t *testing.T) {
	probe := &hookProbeTool{output: "untouched"}
	got := toolResultText(t, dispatchProbe(t, probe, Options{}, nil))
	if got != "untouched" {
		t.Errorf("result = %q, want the tool's output unchanged", got)
	}

	provider := &scriptedProvider{}
	l := New(provider, tools.NewRegistry())
	res, err := l.Run(context.Background(), Options{
		Model: "test", Prompt: "hello", MaxTurns: 2, Permission: bypassPerm(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	blocks := userBlocks(t, res.Messages)
	if len(blocks) != 1 || blocks[0] != "hello" {
		t.Errorf("user blocks = %q, want just the prompt", blocks)
	}
}

// userBlocks returns the text blocks of the first user message.
func userBlocks(t *testing.T, messages []anthropic.BetaMessageParam) []string {
	t.Helper()
	for _, m := range messages {
		if m.Role != anthropic.BetaMessageParamRoleUser {
			continue
		}
		var out []string
		for _, b := range m.Content {
			if b.OfText != nil {
				out = append(out, b.OfText.Text)
			}
		}
		return out
	}
	t.Fatal("no user message in the result")
	return nil
}
