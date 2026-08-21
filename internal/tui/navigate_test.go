package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func navModel(t *testing.T) *Model {
	t.Helper()
	m := newTestModel()
	m.resize(80, 200) // tall, so /search prints inline instead of paging
	return m
}

func TestSearchFindsAcrossKinds(t *testing.T) {
	m := navModel(t)
	m.noteNav(navUser, "why does refreshToken expire", "why does refreshToken expire", 0)
	m.noteNav(navAssistant, "because the clock skews", "because the clock skews", 0)
	m.renderEvent(mkResult("Bash", "PASS\nrefreshToken_test.go ok", false))
	m.noteNav(navCommand, "Bash go test", "", 1)

	m.searchConversation([]string{"refreshToken"})
	out := visibleText(m.transcript.String())
	if !strings.Contains(out, "match") {
		t.Fatalf("expected matches:\n%s", out)
	}
	if !strings.Contains(out, "you") || !strings.Contains(out, "tool") {
		t.Errorf("search should span your messages and tool output:\n%s", out)
	}
}

func TestSearchFiltersByKind(t *testing.T) {
	m := navModel(t)
	m.noteNav(navUser, "fix the widget", "fix the widget", 0)
	m.noteNav(navAssistant, "the widget is fixed", "the widget is fixed", 0)

	m.searchConversation([]string{"--mine", "widget"})
	out := visibleText(m.transcript.String())
	if strings.Contains(out, "klaudia") {
		t.Errorf("--mine should exclude Klaudia's messages:\n%s", out)
	}
	if !strings.Contains(out, "you") {
		t.Errorf("--mine should still match your messages:\n%s", out)
	}
}

func TestSearchSupportsRegex(t *testing.T) {
	m := navModel(t)
	m.noteNav(navUser, "error code 4711 today", "error code 4711 today", 0)
	m.searchConversation([]string{"/code \\d+/"})
	if !strings.Contains(visibleText(m.transcript.String()), "match") {
		t.Error("a /regex/ query should match")
	}
}

func TestSearchRejectsBadRegexCleanly(t *testing.T) {
	m := navModel(t)
	m.noteNav(navUser, "x", "x", 0)
	m.searchConversation([]string{"/[unclosed/"})
	if !strings.Contains(visibleText(m.transcript.String()), "bad regex") {
		t.Error("a malformed regex should report itself, not panic or match nothing silently")
	}
}

func TestSearchReportsNoMatches(t *testing.T) {
	m := navModel(t)
	m.noteNav(navUser, "hello", "hello", 0)
	m.searchConversation([]string{"nowherenear"})
	if !strings.Contains(visibleText(m.transcript.String()), "No match") {
		t.Error("expected a clear no-match message")
	}
}

func TestErrorsListsOnlyFailures(t *testing.T) {
	m := navModel(t)
	m.noteNav(navCommand, "Bash go build", "", 0)
	m.noteNav(navError, "Bash go test → FAIL auth", "", 0)
	m.noteNav(navCommand, "Read main.go", "", 0)

	m.listErrors(nil)
	out := visibleText(m.transcript.String())
	if !strings.Contains(out, "FAIL auth") {
		t.Errorf("the failure should be listed:\n%s", out)
	}
	if strings.Contains(out, "go build") || strings.Contains(out, "Read main.go") {
		t.Errorf("/errors should list only errors:\n%s", out)
	}
}

func TestOutlineShowsSessionShape(t *testing.T) {
	m := navModel(t)
	m.noteNav(navUser, "add retries", "add retries", 0)
	m.noteNav(navCommand, "Edit client.go", "", 0)
	m.noteNav(navError, "Bash go test → FAIL", "", 0)

	m.outline()
	out := visibleText(m.transcript.String())
	for _, want := range []string{"add retries", "Edit client.go", "FAIL", "/show"} {
		if !strings.Contains(out, want) {
			t.Errorf("outline missing %q:\n%s", want, out)
		}
	}
}

func TestShowEntryResolvesToolOutputThroughTheRing(t *testing.T) {
	m := navModel(t)
	m.renderEvent(mkResult("Bash", "the full build log", false))
	m.noteNav(navCommand, "Bash go build", "", 1)

	m.showEntry([]string{"1"})
	if !strings.Contains(visibleText(m.transcript.String()), "the full build log") {
		t.Error("/show should resolve a tool entry to its full stored output")
	}
}

func TestShowEntryRejectsUnknown(t *testing.T) {
	m := navModel(t)
	m.showEntry([]string{"42"})
	if !strings.Contains(visibleText(m.transcript.String()), "/outline") {
		t.Error("an unknown entry should point at /outline")
	}
}

// --- file references ------------------------------------------------------

func TestFuzzyCompletionFindsNestedFile(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "src", "auth", "session.ts"))
	mustWrite(t, filepath.Join(dir, "unrelated.go"))

	m := newTestModel()
	m.sess.CWD = dir
	got := m.matchPaths("session.ts")
	if len(got) == 0 || got[0] != filepath.Join("src", "auth", "session.ts") {
		t.Fatalf("fuzzy completion should find the nested file, got %v", got)
	}
}

func TestRecentlyTouchedFilesRankFirst(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "aaa_first.go"))
	mustWrite(t, filepath.Join(dir, "zzz_later.go"))

	m := newTestModel()
	m.sess.CWD = dir
	m.noteRecentPath(filepath.Join(dir, "zzz_later.go"))

	got := m.matchPaths("")
	if len(got) == 0 || got[0] != "zzz_later.go" {
		t.Fatalf("the file Klaudia just touched should rank first, got %v", got)
	}
}

func TestPathIndexIsCached(t *testing.T) {
	orig := globFiles
	t.Cleanup(func() { globFiles = orig })
	calls := 0
	globFiles = func(root string) ([]string, error) {
		calls++
		return []string{filepath.Join(root, "a.go")}, nil
	}

	var idx pathIndex
	now := time.Now()
	idx.files("/repo", now)
	idx.files("/repo", now.Add(time.Second))
	if calls != 1 {
		t.Errorf("index rebuilt %d times within the TTL, want 1", calls)
	}
	idx.files("/repo", now.Add(pathIndexTTL+time.Second))
	if calls != 2 {
		t.Errorf("index should rebuild after the TTL, got %d calls", calls)
	}
	idx.files("/other", now.Add(pathIndexTTL+time.Second))
	if calls != 3 {
		t.Error("changing root should rebuild the index")
	}
}

func TestTabCyclesThroughCandidates(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "alpha.go"))
	mustWrite(t, filepath.Join(dir, "alpine.go"))

	m := newTestModel()
	m.resize(80, 24)
	m.sess.CWD = dir
	m.input.SetValue("@alp")
	m.input.CursorEnd()

	// Consecutive Tabs with no intervening keystroke cycle the candidates.
	m.completeAtPath()
	first := m.input.Value()
	m.completeAtPath()
	second := m.input.Value()
	if first == second {
		t.Errorf("repeated Tab should cycle candidates, stuck on %q", first)
	}
	m.completeAtPath()
	if third := m.input.Value(); third != first {
		t.Errorf("cycling should wrap back to the first hit, got %q want %q", third, first)
	}
}

func TestCompletionKeepsLineSuffix(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "session.ts"))

	m := newTestModel()
	m.resize(80, 24)
	m.sess.CWD = dir
	m.input.SetValue("@session.ts:184")
	m.input.CursorEnd()
	m.completeAtPath()

	if got := m.input.Value(); !strings.HasSuffix(got, ":184") {
		t.Errorf("completion dropped the line number: %q", got)
	}
}

func TestParseFileRef(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "session.ts"))

	for _, tc := range []struct {
		in        string
		ok        bool
		path      string
		line, col int
	}{
		{"session.ts:184", true, "session.ts", 184, 0},
		{"session.ts:184:12", true, "session.ts", 184, 12},
		{"session.ts", true, "session.ts", 0, 0},
		{"(session.ts:9)", true, "session.ts", 9, 0},
		{"https://example.com:443/x", false, "", 0, 0},
		{"10:30", false, "", 0, 0},
		{"nonexistent.go:12", false, "", 0, 0},
	} {
		got, ok := parseFileRef(dir, tc.in, nil)
		if ok != tc.ok {
			t.Errorf("parseFileRef(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if ok && (got.Path != tc.path || got.Line != tc.line || got.Col != tc.col) {
			t.Errorf("parseFileRef(%q) = %+v, want %s:%d:%d", tc.in, got, tc.path, tc.line, tc.col)
		}
	}
}

func TestSplitLineSuffix(t *testing.T) {
	for _, tc := range []struct {
		in        string
		path      string
		line, col int
	}{
		{"a.go:12:3", "a.go", 12, 3},
		{"a.go:12", "a.go", 12, 0},
		{"a.go", "a.go", 0, 0},
		{"a.go:", "a.go:", 0, 0},
		{"a.go:abc", "a.go:abc", 0, 0},
	} {
		p, l, c := splitLineSuffix(tc.in)
		if p != tc.path || l != tc.line || c != tc.col {
			t.Errorf("splitLineSuffix(%q) = (%q,%d,%d), want (%q,%d,%d)",
				tc.in, p, l, c, tc.path, tc.line, tc.col)
		}
	}
}

func TestFuzzyScoreRanking(t *testing.T) {
	// An exact-prefix path must outrank a mere subsequence hit.
	prefix, ok1 := fuzzyScore("src/au", "src/auth/session.ts")
	sub, ok2 := fuzzyScore("src/au", "s/r/c/a/u/other.go")
	if !ok1 || !ok2 {
		t.Fatal("both should match")
	}
	if prefix <= sub {
		t.Errorf("prefix match (%d) should outrank subsequence (%d)", prefix, sub)
	}
	if _, ok := fuzzyScore("qqq", "src/auth/session.ts"); ok {
		t.Error("unrelated pattern should not match")
	}
}

func mustWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEditorCommandArgv(t *testing.T) {
	ref := fileRef{Path: "src/a.go", Line: 42}
	for _, tc := range []struct{ editor, wantArg string }{
		{"vim", "+42"},
		{"nvim", "+42"},
		{"code", "-g"},
		{"hx", "src/a.go:42"},
	} {
		env := func(k string) string {
			if k == "EDITOR" {
				return tc.editor
			}
			return ""
		}
		cmd, ok := editorCommand(env, "/repo", ref)
		if !ok {
			t.Logf("%s not installed, skipping", tc.editor)
			continue
		}
		if !contains(cmd.Args, tc.wantArg) {
			t.Errorf("%s argv = %v, want it to contain %q", tc.editor, cmd.Args, tc.wantArg)
		}
	}
}

func TestEditorCommandNeedsEditorSet(t *testing.T) {
	if _, ok := editorCommand(func(string) string { return "" }, "/repo", fileRef{Path: "a.go"}); ok {
		t.Error("with no $EDITOR or $VISUAL there is nothing to open with")
	}
}

func TestOpenRejectsMissingFile(t *testing.T) {
	m := navModel(t)
	m.sess.CWD = t.TempDir()
	m.openInEditor([]string{"does/not/exist.go:12"})
	if !strings.Contains(visibleText(m.transcript.String()), "no such file") {
		t.Error("/open should report a missing file clearly")
	}
}
