package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemIncludesEnvAndSecurity(t *testing.T) {
	dir := t.TempDir()
	p := System(dir, "claude-haiku-4-5")

	for _, want := range []string{
		"You are Klaudia",
		"authorized security testing", // security clause
		"<env>",
		"Working directory: " + dir,
		"Today's date:",
		"claude-haiku-4-5",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}

func TestSystemLoadsProjectClaudeMd(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate from a real ~/.claude/CLAUDE.md
	dir := t.TempDir()
	body := "# Klaudia repo\nAlways run gofmt before committing."
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	p := System(dir, "")
	if !strings.Contains(p, "Project instructions (from CLAUDE.md)") {
		t.Error("expected CLAUDE.md section header")
	}
	if !strings.Contains(p, "Always run gofmt before committing.") {
		t.Error("expected CLAUDE.md content to be included")
	}
}

func TestSystemRecallsMemory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	klaudiaDir := filepath.Join(dir, ".klaudia")
	if err := os.MkdirAll(klaudiaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(klaudiaDir, "MEMORY.md"), []byte("# Memory\n\n- 2026 prefer doublestar\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := System(dir, "")
	if !strings.Contains(p, "# Recalled memory") || !strings.Contains(p, "prefer doublestar") {
		t.Errorf("system prompt should include recalled memory")
	}
	if !strings.Contains(p, "Memory tool") {
		t.Errorf("recall section should mention the Memory tool")
	}
}

func TestSystemRecallsLinkedMemoryFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	memDir := filepath.Join(dir, ".klaudia", "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// MEMORY.md is the index — session bullets plus a "## Linked memory" section
	// of pointers (maintained on disk by memory.Store.SyncLinks).
	index := "# Memory\n\n- Root memory\n\n## Linked memory\n\n- [tools](memory/tools.md) — Tools\n"
	if err := os.WriteFile(filepath.Join(dir, ".klaudia", "MEMORY.md"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "tools.md"), []byte("# Tools\n\n- Use rg for search\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := System(dir, "")
	// Recall surfaces the index and its pointers, but never inlines the detail
	// note's contents, so it stays cheap as memory grows.
	for _, want := range []string{"Root memory", "[tools](memory/tools.md)"} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt missing recalled memory %q", want)
		}
	}
	if strings.Contains(p, "Use rg for search") {
		t.Errorf("detail-note contents should not be inlined into recall")
	}
}

func TestSystemRecallsLegacyMemoryPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	memDir := filepath.Join(dir, ".klaudia", "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("# Memory\n\n- Legacy memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := System(dir, "")
	if !strings.Contains(p, "Legacy memory") {
		t.Errorf("system prompt should include legacy recalled memory")
	}
}

func TestSystemRecallsUserMemory(t *testing.T) {
	// Point the user config root at a temp dir so the test never reads the real
	// $HOME/.klaudia (issue #161 / #110).
	cfgDir := t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", cfgDir)
	if err := os.WriteFile(filepath.Join(cfgDir, "MEMORY.md"), []byte("# Memory\n\n- user prefers tabs over spaces\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := System(t.TempDir(), "")
	if !strings.Contains(p, "# Recalled memory") {
		t.Error("expected recalled-memory section")
	}
	if !strings.Contains(p, "User memory") {
		t.Error("expected a user-memory label")
	}
	if !strings.Contains(p, "user prefers tabs over spaces") {
		t.Error("expected user memory content to be injected")
	}
}

func TestSystemNoUserMemory(t *testing.T) {
	// An empty config root (no MEMORY.md) must be a no-op — no user-memory
	// label and no spurious recall section.
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	p := System(t.TempDir(), "")
	if strings.Contains(p, "User memory") {
		t.Error("absent user MEMORY.md should not emit a user-memory label")
	}
	if strings.Contains(p, "# Recalled memory") {
		t.Error("no memory anywhere should not emit a recall section")
	}
}

func TestSystemRecallsUserAndProjectMemory(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", cfgDir)
	if err := os.WriteFile(filepath.Join(cfgDir, "MEMORY.md"), []byte("# Memory\n\n- global fact about the user\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	klaudiaDir := filepath.Join(dir, ".klaudia")
	if err := os.MkdirAll(klaudiaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(klaudiaDir, "MEMORY.md"), []byte("# Memory\n\n- project-specific fact\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := System(dir, "")
	for _, want := range []string{"User memory", "Project memory", "global fact about the user", "project-specific fact"} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
	// Project memory comes last so it can refine the user-level notes.
	if strings.Index(p, "User memory") > strings.Index(p, "Project memory") {
		t.Error("user memory should be injected before project memory")
	}
}

func TestSystemRecallsKnowledge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".klaudia"), 0o755)
	os.WriteFile(filepath.Join(dir, ".klaudia", "KNOWLEDGE.md"),
		[]byte("# Knowledge\n\n- The build uses CGO_ENABLED=0\n"), 0o644)

	p := System(dir, "")
	if !strings.Contains(p, "# Project knowledge") || !strings.Contains(p, "CGO_ENABLED=0") {
		t.Errorf("system prompt should include recalled project knowledge")
	}
}

func TestSystemNoClaudeMd(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate from a real ~/.claude/CLAUDE.md
	p := System(t.TempDir(), "")
	if strings.Contains(p, "Project instructions (from CLAUDE.md)") {
		t.Error("should not emit CLAUDE.md section when none exists")
	}
}

// AGENTS.md is the cross-agent standard — several hundred thousand
// repositories carry one, and Klaudia read none of them. A user who had
// written the instructions for this exact purpose got a model that had never
// seen them.
func TestSystemLoadsAgentsMd(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	write(t, filepath.Join(dir, "AGENTS.md"), "# Conventions\nUse tabs, never spaces.")

	p := System(dir, "")
	if !strings.Contains(p, "Use tabs, never spaces.") {
		t.Error("AGENTS.md content was not included")
	}
	if !strings.Contains(p, "Project instructions (from AGENTS.md)") {
		t.Error("the section header should name the file the instructions came from")
	}
}

// Both files, with different content, is a real configuration: the generic one
// for every agent and a Claude-specific refinement beside it. Reading one and
// ignoring the other silently drops instructions.
func TestSystemLoadsBothInstructionFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	write(t, filepath.Join(dir, "AGENTS.md"), "generic: run the linter")
	write(t, filepath.Join(dir, "CLAUDE.md"), "specific: sign your commits")

	p := System(dir, "")
	for _, want := range []string{"generic: run the linter", "specific: sign your commits"} {
		if !strings.Contains(p, want) {
			t.Errorf("missing %q", want)
		}
	}
	// Generic first, agent-specific after it, so the specific one reads as the
	// refinement rather than being contradicted by what follows.
	if strings.Index(p, "generic:") > strings.Index(p, "specific:") {
		t.Error("AGENTS.md should come before CLAUDE.md")
	}
	if !strings.Contains(p, "(from AGENTS.md, CLAUDE.md)") {
		t.Error("the header should name both files")
	}
}

// `ln -s AGENTS.md CLAUDE.md` is the commonest way a repo supports both today.
// filepath.Abs gives the two names different paths, so without resolving the
// link the entire instruction block is sent twice in every request.
func TestSymlinkedInstructionFileIsReadOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	write(t, filepath.Join(dir, "AGENTS.md"), "a distinctive instruction line")
	if err := os.Symlink("AGENTS.md", filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	p := System(dir, "")
	if n := strings.Count(p, "a distinctive instruction line"); n != 1 {
		t.Errorf("instructions appear %d times, want 1", n)
	}
}

// And the second commonest way: a copy. No path comparison can catch that, so
// identity is also established by content.
func TestDuplicatedInstructionContentIsReadOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	body := "the same instructions in two files"
	write(t, filepath.Join(dir, "AGENTS.md"), body)
	write(t, filepath.Join(dir, "CLAUDE.md"), body)

	p := System(dir, "")
	if n := strings.Count(p, body); n != 1 {
		t.Errorf("instructions appear %d times, want 1", n)
	}
	if !strings.Contains(p, "(from AGENTS.md)") {
		t.Error("the header should name only the file that was actually used")
	}
}

// A global AGENTS.md belongs with skills and .mcp.json under ~/.klaudia. The
// home-level ~/.claude/CLAUDE.md is still read for ecosystem compatibility.
func TestSystemLoadsGlobalInstructions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	write(t, filepath.Join(home, ".claude", "CLAUDE.md"), "ecosystem global")
	write(t, filepath.Join(home, ".klaudia", "AGENTS.md"), "klaudia global")

	p := System(t.TempDir(), "")
	for _, want := range []string{"ecosystem global", "klaudia global"} {
		if !strings.Contains(p, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
