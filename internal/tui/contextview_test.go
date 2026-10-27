package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/pkg/agent"
)

func ctxModel(t *testing.T) *Model {
	t.Helper()
	return &Model{
		sess:    &Session{CWD: t.TempDir(), GitBranch: "main", SessionID: "s1"},
		touched: map[string]bool{},
		base:    newBaseline(),
	}
}

// seen records paths as inspected, creating them: /context lists only files
// that exist.
func seen(t *testing.T, m *Model, paths ...string) {
	t.Helper()
	for _, p := range paths {
		full := filepath.Join(m.sess.CWD, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte("x"), 0o644)
	}
	m.recentPaths = append(m.recentPaths, paths...)
}

// A pinned file is re-stated every turn. Mentioned once forty turns ago is,
// for practical purposes, not in context at all.
func TestPinnedFilesAreRestatedEveryTurn(t *testing.T) {
	m := ctxModel(t)
	os.WriteFile(filepath.Join(m.sess.CWD, "arch.md"), []byte("x"), 0o644)

	if _, ok := m.pin("arch.md"); !ok {
		t.Fatal("pin failed")
	}
	block := m.pinnedBlock()
	if !strings.Contains(block, "arch.md") {
		t.Errorf("pinned block does not name the file:\n%s", block)
	}
	// Twice is the point — this is what survives compaction.
	if second := m.pinnedBlock(); second != block {
		t.Error("the pinned block is not stable across turns")
	}
}

// A pinned file that vanished must say so rather than silently pin nothing.
func TestPinnedMissingFileIsFlagged(t *testing.T) {
	m := ctxModel(t)
	m.pin("gone.md")
	if !strings.Contains(m.pinnedBlock(), "no longer present") {
		t.Errorf("a missing pin was not flagged:\n%s", m.pinnedBlock())
	}
	if !strings.Contains(stripANSI(m.renderContext()), "no longer present") {
		t.Error("/context does not flag the missing pin")
	}
}

func TestPinIsIdempotentAndUnpinnable(t *testing.T) {
	m := ctxModel(t)
	m.pin("a.go")
	if _, ok := m.pin("a.go"); ok {
		t.Error("pinning twice added a duplicate")
	}
	if len(m.pinned) != 1 {
		t.Fatalf("pinned = %v", m.pinned)
	}
	if _, ok := m.unpin("a.go"); !ok {
		t.Error("unpin failed")
	}
	if len(m.pinned) != 0 {
		t.Errorf("pinned = %v after unpin", m.pinned)
	}
	if _, ok := m.unpin("never.go"); ok {
		t.Error("unpinning something unpinned reported success")
	}
}

// Absolute paths and repo-relative ones are the same pin.
func TestPinNormalisesPaths(t *testing.T) {
	m := ctxModel(t)
	m.pin(filepath.Join(m.sess.CWD, "src", "a.go"))
	if len(m.pinned) != 1 || m.pinned[0] != "src/a.go" {
		t.Fatalf("pinned = %v, want [src/a.go]", m.pinned)
	}
	if _, ok := m.unpin("src/a.go"); !ok {
		t.Error("the same file could not be unpinned by its relative path")
	}
}

// Twelve files in one directory is one line, not twelve.
func TestActiveAreasCollapseByDirectory(t *testing.T) {
	areas := activeAreas([]string{
		"src/auth/a.go", "src/auth/b.go", "src/auth/c.go",
		"test/auth/a_test.go",
		"README.md",
	})
	joined := strings.Join(areas, "\n")
	if !strings.Contains(joined, "src/auth/*  (3 files)") {
		t.Errorf("directory was not collapsed:\n%s", joined)
	}
	if !strings.Contains(joined, "test/auth/") {
		t.Errorf("single-file directory missing:\n%s", joined)
	}
	// A lone top-level file is not an "area".
	if strings.Contains(joined, "README") {
		t.Errorf("a single top-level file was listed as an area:\n%s", joined)
	}
}

// §18's last checkbox: Klaudia admits what it has not looked at.
func TestContextAdmitsWhatItHasNotRead(t *testing.T) {
	m := ctxModel(t)
	out := stripANSI(m.renderContext())
	if !strings.Contains(out, "has not read any files") {
		t.Errorf("a fresh session does not admit it has read nothing:\n%s", out)
	}

	seen(t, m, "a.go", "b.go")
	out = stripANSI(m.renderContext())
	if !strings.Contains(out, "not what the task needs") {
		t.Errorf("/context implies its listing is sufficient:\n%s", out)
	}
	if !strings.Contains(out, "/pin") {
		t.Errorf("/context does not say what to do about a gap:\n%s", out)
	}
}

// Files Klaudia changed are listed separately from files it merely read.
func TestChangedAndInspectedAreSeparate(t *testing.T) {
	m := ctxModel(t)
	seen(t, m, "src/a.go", "src/b.go")
	m.touched["src/a.go"] = true

	out := stripANSI(m.renderContext())
	changedAt := strings.Index(out, "Changed this session")
	inspectedAt := strings.Index(out, "Recently inspected")
	if changedAt < 0 || inspectedAt < 0 {
		t.Fatalf("both sections should be present:\n%s", out)
	}
	if changedAt > inspectedAt {
		t.Error("what Klaudia changed should come before what it merely read")
	}
	// A changed file must not also appear under "recently inspected".
	tail := out[inspectedAt:]
	if strings.Contains(tail, "src/a.go") {
		t.Errorf("a changed file was also listed as merely inspected:\n%s", tail)
	}
}

// /context keeps the orientation facts §13 asks to always be understandable.
func TestContextKeepsWhereYouAre(t *testing.T) {
	m := ctxModel(t)
	m.sess.ExtraDirs = []string{"/other/repo"}
	out := stripANSI(m.renderContext())
	for _, want := range []string{"cwd=", "git-branch=main", "session-id=s1", "also=/other/repo"} {
		if !strings.Contains(out, want) {
			t.Errorf("/context missing %q:\n%s", want, out)
		}
	}
}

// /forget cannot unsee what the model has read, and must not pretend it can.
func TestForgetIsHonestAboutItsLimits(t *testing.T) {
	m := ctxModel(t)
	seen(t, m, "noise.go")
	m.forgetCommand([]string{"noise.go"})
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "stays in the conversation") {
		t.Errorf("/forget implied it removed the file from the model's context:\n%s", out)
	}
	if len(m.recentPaths) != 0 {
		t.Error("the path was not dropped from tracking")
	}
}

func TestForgetAlsoUnpins(t *testing.T) {
	m := ctxModel(t)
	m.pin("a.go")
	if _, ok := m.forget("a.go"); !ok {
		t.Fatal("forget reported nothing to drop")
	}
	if len(m.pinned) != 0 {
		t.Errorf("forget left the file pinned: %v", m.pinned)
	}
}

// A Read of a file that does not exist used to be listed as recently
// inspected: the path was noted when the call started. Now only a call that
// succeeded counts, and a file deleted since drops out.
func TestContextListsOnlyFilesThatExist(t *testing.T) {
	m := newTestModel()
	m.sess.CWD = t.TempDir()
	os.WriteFile(filepath.Join(m.sess.CWD, "real.go"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(m.sess.CWD, "gone.go"), []byte("x"), 0o644)
	read := func(id, path string, failed bool) {
		m.renderEvent(agent.Event{Type: "tool_use", ToolUseID: id, ToolName: "Read",
			Input: json.RawMessage(`{"file_path":"` + filepath.Join(m.sess.CWD, path) + `"}`)})
		m.renderEvent(agent.Event{Type: "tool_result", ToolUseID: id, ToolName: "Read", IsError: failed, Content: "x"})
	}
	read("1", "real.go", false)
	read("2", "ghost.go", true)
	read("3", "gone.go", false)

	out := stripANSI(m.renderContext())
	if strings.Contains(out, "ghost.go") {
		t.Errorf("a failed Read is listed as inspected:\n%s", out)
	}
	if !strings.Contains(out, "gone.go") {
		t.Fatalf("precondition: a successful Read of an existing-at-the-time file is listed:\n%s", out)
	}
	os.Remove(filepath.Join(m.sess.CWD, "gone.go"))
	out = stripANSI(m.renderContext())
	if strings.Contains(out, "gone.go") || !strings.Contains(out, "real.go") || !strings.Contains(out, "(1 file)") {
		t.Errorf("want only real.go, counted once:\n%s", out)
	}
}
