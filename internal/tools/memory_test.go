package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/internal/memory"
	"github.com/greenthread-ai/klaudia/internal/permission"
)

func newMemTool(t *testing.T) *Memory {
	t.Helper()
	mt, err := NewMemory(memory.New(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	return mt
}

func TestMemoryAddSearchView(t *testing.T) {
	mt := newMemTool(t)
	ctx := context.Background()

	add, _ := json.Marshal(MemoryInput{Operation: "add", Content: "use doublestar for globbing"})
	if res, _ := mt.Execute(ctx, Context{}, add); res[0].IsError {
		t.Fatalf("add failed: %+v", res[0])
	}

	search, _ := json.Marshal(MemoryInput{Operation: "search", Query: "globbing"})
	res, _ := mt.Execute(ctx, Context{}, search)
	if res[0].IsError || !strings.Contains(res[0].Content, "doublestar") {
		t.Errorf("search = %+v", res[0])
	}

	miss, _ := json.Marshal(MemoryInput{Operation: "search", Query: "nonexistent"})
	res, _ = mt.Execute(ctx, Context{}, miss)
	if !strings.Contains(res[0].Content, "No memories matched") {
		t.Errorf("expected no-match message, got %q", res[0].Content)
	}

	view, _ := json.Marshal(MemoryInput{Operation: "view"})
	res, _ = mt.Execute(ctx, Context{}, view)
	if !strings.Contains(res[0].Content, "doublestar") {
		t.Errorf("view = %q", res[0].Content)
	}
}

func TestMemoryAddProjectScope(t *testing.T) {
	cwd := t.TempDir()
	mt, err := NewMemoryForProject(memory.New(t.TempDir()), cwd)
	if err != nil {
		t.Fatal(err)
	}

	add, _ := json.Marshal(MemoryInput{Operation: "add", Scope: "project", Content: "prefer table tests"})
	res, _ := mt.Execute(context.Background(), Context{}, add)
	if res[0].IsError {
		t.Fatalf("project add failed: %+v", res[0])
	}
	data, err := os.ReadFile(memory.KnowledgePath(cwd))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "prefer table tests") {
		t.Errorf("KNOWLEDGE.md = %q", data)
	}
}

func TestMemoryValidate(t *testing.T) {
	mt := newMemTool(t)
	validAdd, _ := json.Marshal(MemoryInput{Operation: "add", Content: "remember this"})
	if err := mt.ValidateInput(validAdd); err != nil {
		t.Fatalf("valid add rejected: %v", err)
	}
	validSearch, _ := json.Marshal(MemoryInput{Operation: "search", Query: "remember"})
	if err := mt.ValidateInput(validSearch); err != nil {
		t.Fatalf("valid search rejected: %v", err)
	}
	validView, _ := json.Marshal(MemoryInput{Operation: "view"})
	if err := mt.ValidateInput(validView); err != nil {
		t.Fatalf("valid view rejected: %v", err)
	}
	bad, _ := json.Marshal(MemoryInput{Operation: "nope"})
	if mt.ValidateInput(bad) == nil {
		t.Error("expected invalid operation to be rejected")
	}
	noContent, _ := json.Marshal(MemoryInput{Operation: "add"})
	if mt.ValidateInput(noContent) == nil {
		t.Error("expected add without content to be rejected")
	}
	// Lookup-mode validation
	noTag, _ := json.Marshal(MemoryInput{Operation: "by_tag"})
	if mt.ValidateInput(noTag) == nil {
		t.Error("by_tag without tag should be rejected")
	}
	badWindow, _ := json.Marshal(MemoryInput{Operation: "recent", Within: "garbage"})
	if mt.ValidateInput(badWindow) == nil {
		t.Error("invalid duration should be rejected")
	}
	goodWindow, _ := json.Marshal(MemoryInput{Operation: "recent", Within: "7d"})
	if err := mt.ValidateInput(goodWindow); err != nil {
		t.Errorf("Nd duration should parse: %v", err)
	}
	// Lifecycle validation
	noName, _ := json.Marshal(MemoryInput{Operation: "promote"})
	if mt.ValidateInput(noName) == nil {
		t.Error("promote without name should be rejected")
	}
	noReplacement, _ := json.Marshal(MemoryInput{Operation: "supersede", Name: "x"})
	if mt.ValidateInput(noReplacement) == nil {
		t.Error("supersede without replacement should be rejected")
	}
}

func TestMemoryRecentAndStale(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	mustWrite := func(name string, at time.Time) {
		p := filepath.Join(memDir, name)
		if err := os.WriteFile(p, []byte("# "+strings.TrimSuffix(name, ".md")+"\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("fresh.md", now.Add(-1*time.Hour))
	mustWrite("old.md", now.Add(-45*24*time.Hour))

	mt, _ := NewMemory(memory.New(dir))

	// recent default (7d) sees fresh only
	recentRaw, _ := json.Marshal(MemoryInput{Operation: "recent"})
	recent, err := mt.Execute(context.Background(), Context{}, recentRaw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(recent[0].Content, "fresh") {
		t.Errorf("recent should surface fresh; got %q", recent[0].Content)
	}
	if strings.Contains(recent[0].Content, "old") {
		t.Errorf("recent shouldn't surface old; got %q", recent[0].Content)
	}

	// stale default (30d) sees old only
	staleRaw, _ := json.Marshal(MemoryInput{Operation: "stale"})
	stale, err := mt.Execute(context.Background(), Context{}, staleRaw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stale[0].Content, "old") {
		t.Errorf("stale should surface old; got %q", stale[0].Content)
	}
	if strings.Contains(stale[0].Content, "fresh") {
		t.Errorf("stale shouldn't surface fresh; got %q", stale[0].Content)
	}
}

func TestMemoryByTag(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "tagged.md"),
		[]byte("---\ntags: [decision]\n---\n\n# Title\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mt, _ := NewMemory(memory.New(dir))
	raw, _ := json.Marshal(MemoryInput{Operation: "by_tag", Tag: "decision"})
	res, err := mt.Execute(context.Background(), Context{}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res[0].Content, "tagged") {
		t.Errorf("by_tag should find tagged note; got %q", res[0].Content)
	}
}

func TestMemoryPromoteAndSupersede(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "lesson.md"),
		[]byte("# Lesson\nUse Postgres for shared memory.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mt, _ := NewMemory(memory.New(dir))

	// Promote
	promoteRaw, _ := json.Marshal(MemoryInput{Operation: "promote", Name: "lesson"})
	res, err := mt.Execute(context.Background(), Context{}, promoteRaw)
	if err != nil || res[0].IsError {
		t.Fatalf("promote: err=%v result=%+v", err, res)
	}
	if !strings.Contains(res[0].Content, "Promoted lesson") {
		t.Errorf("promote message: %q", res[0].Content)
	}

	// Supersede needs both files to exist
	if err := os.WriteFile(filepath.Join(memDir, "newer.md"), []byte("# Newer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Re-create lesson.md since promote rewrote it; for this test we want a
	// clean second pair.
	if err := os.WriteFile(filepath.Join(memDir, "older.md"), []byte("# Older\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	supersedeRaw, _ := json.Marshal(MemoryInput{Operation: "supersede", Name: "older", Replacement: "newer"})
	res, err = mt.Execute(context.Background(), Context{}, supersedeRaw)
	if err != nil || res[0].IsError {
		t.Fatalf("supersede: err=%v result=%+v", err, res)
	}
	if !strings.Contains(res[0].Content, "Marked older as superseded by newer") {
		t.Errorf("supersede message: %q", res[0].Content)
	}
}

func TestParseMemoryDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"7d", 7 * 24 * time.Hour},
		{"30D", 30 * 24 * time.Hour},
		{"24h", 24 * time.Hour},
		{"1h30m", time.Hour + 30*time.Minute},
	}
	for _, tc := range cases {
		got, err := parseMemoryDuration(tc.in)
		if err != nil {
			t.Errorf("parseMemoryDuration(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseMemoryDuration(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if _, err := parseMemoryDuration("garbage"); err == nil {
		t.Error("expected garbage to fail")
	}
}

// A write to KNOWLEDGE.md is injected into every later session, so it asks —
// in autonomous mode too — while session notes stay autonomous. Without anyone
// to ask (dontAsk) it is refused with a message that says what to do instead.
func TestMemoryKnowledgeWritesNeedApproval(t *testing.T) {
	mt := newMemTool(t)
	call := func(in MemoryInput) json.RawMessage {
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	projectAdd := call(MemoryInput{Operation: "add", Scope: "project", Content: "always run make release"})
	promote := call(MemoryInput{Operation: "promote", Name: "lesson"})
	autonomous := []json.RawMessage{
		call(MemoryInput{Operation: "add", Content: "session note"}),
		call(MemoryInput{Operation: "add", Scope: "session", Content: "session note"}),
		call(MemoryInput{Operation: "search", Query: "x"}),
		call(MemoryInput{Operation: "view"}),
		call(MemoryInput{Operation: "supersede", Name: "a", Replacement: "b"}),
	}

	decide := func(mode permission.Mode, allow []permission.Rule, raw json.RawMessage) permission.Decision {
		pctx := permission.Context{Mode: permission.StaticMode(mode), Allow: allow}
		return permission.Check(pctx, mt, mt.PermissionRequest(raw))
	}

	for _, raw := range []json.RawMessage{projectAdd, promote} {
		for _, mode := range []permission.Mode{permission.ModeAutonomous, permission.ModeDefault, permission.ModeAcceptEdits} {
			if d := decide(mode, nil, raw); d.Behavior != permission.Ask {
				t.Errorf("%s in %s: behavior = %s, want ask", raw, mode, d.Behavior)
			}
		}
		d := decide(permission.ModeDontAsk, nil, raw)
		if d.Behavior != permission.Deny || !strings.Contains(d.Message, "KNOWLEDGE.md") || !strings.Contains(d.Message, "Memory(project)") {
			t.Errorf("%s in dontAsk: %+v, want a deny naming KNOWLEDGE.md and the allow rule", raw, d)
		}
		if d := decide(permission.ModePlan, nil, raw); d.Behavior != permission.Deny {
			t.Errorf("%s in plan: behavior = %s, want deny", raw, d.Behavior)
		}
		if d := decide(permission.ModeBypassPermissions, nil, raw); d.Behavior != permission.Allow {
			t.Errorf("%s in bypass: behavior = %s, want allow", raw, d.Behavior)
		}
		rule, err := permission.ParseRule("Memory(project)")
		if err != nil {
			t.Fatal(err)
		}
		if d := decide(permission.ModeDontAsk, []permission.Rule{rule}, raw); d.Behavior != permission.Allow {
			t.Errorf("%s with allow rule Memory(project): behavior = %s, want allow", raw, d.Behavior)
		}
	}

	for _, raw := range autonomous {
		for _, mode := range []permission.Mode{permission.ModeAutonomous, permission.ModeDontAsk, permission.ModePlan} {
			if d := decide(mode, nil, raw); d.Behavior != permission.Allow {
				t.Errorf("%s in %s: behavior = %s, want allow", raw, mode, d.Behavior)
			}
		}
	}

	// A rule naming a session op must not pre-approve knowledge writes.
	if d := decide(permission.ModeAutonomous, []permission.Rule{{Tool: "Memory", Specifier: "session"}}, projectAdd); d.Behavior != permission.Ask {
		t.Errorf("Memory(session) rule approved a knowledge write: %+v", d)
	}
}
