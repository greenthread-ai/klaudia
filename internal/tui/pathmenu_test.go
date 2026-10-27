package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// typeInto feeds a string through the real key path, so the menu is built the
// way a user builds it rather than by calling refreshPathMenu directly.
func typeInto(m *Model, s string) {
	for _, r := range s {
		m.onKey(textKey(string(r)))
	}
}

func menuModel(t *testing.T, dir string) *Model {
	t.Helper()
	m := newTestModel()
	m.resize(80, 24)
	m.sess.CWD = dir
	m.ready = true
	return m
}

// The reported bug: typing "@" did nothing visible, so the completion nobody
// could see was reported as completion that did not exist.
func TestAtOpensTheMenuWhileTyping(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "alpha.go"))
	mustWrite(t, filepath.Join(dir, "alpine.go"))

	m := menuModel(t, dir)
	typeInto(m, "look at @alp")

	view := m.pathMenuView()
	if view == "" {
		t.Fatal("no menu after typing an @reference")
	}
	for _, want := range []string{"alpha.go", "alpine.go"} {
		if !strings.Contains(view, want) {
			t.Errorf("menu is missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "❯") {
		t.Errorf("menu has no selection marker:\n%s", view)
	}
	// The menu is only a view: nothing has been written into the prompt yet.
	if got := m.input.Value(); got != "look at @alp" {
		t.Errorf("typing changed the input to %q", got)
	}
}

// Down moves the highlight and writes it, so what is highlighted and what
// would be sent cannot disagree. Return therefore always means send.
func TestMenuArrowsSelectAndInsert(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "alpha.go"))
	mustWrite(t, filepath.Join(dir, "alpine.go"))

	m := menuModel(t, dir)
	typeInto(m, "@alp")
	first := m.menu.hits[0]

	m.onKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.menu.idx != 1 {
		t.Fatalf("Down did not move the selection: idx=%d", m.menu.idx)
	}
	if got := m.input.Value(); got != "@"+m.menu.hits[1] {
		t.Errorf("input = %q, want the selected hit", got)
	}
	m.onKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.input.Value(); got != "@"+first {
		t.Errorf("Up = %q, want back to %q", got, first)
	}
}

// Esc dismisses without clearing the line the user is typing.
func TestMenuEscDismisses(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "alpha.go"))

	m := menuModel(t, dir)
	typeInto(m, "@alp")
	m.onKey(tea.KeyPressMsg{Code: tea.KeyEsc})

	if m.pathMenuView() != "" {
		t.Error("Esc left the menu on screen")
	}
	if m.input.Value() != "@alp" {
		t.Errorf("Esc changed the input to %q", m.input.Value())
	}
	// Tab brings it back — dismissing is about this keystroke, not the session.
	m.onKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "@alpha.go" {
		t.Errorf("Tab after Esc = %q", m.input.Value())
	}
}

// A path with a space is inserted quoted. Unquoted it would be two tokens to
// everything downstream: the next Tab, and the model reading the prompt.
func TestMenuQuotesPathsWithSpaces(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "Screenshot 2026-10-21 at 4.44.59\u202fpm.png"))

	m := menuModel(t, dir)
	typeInto(m, "@Screenshot")
	m.onKey(tea.KeyPressMsg{Code: tea.KeyTab})

	want := `@"Screenshot 2026-10-21 at 4.44.59` + "\u202f" + `pm.png"`
	if got := m.input.Value(); got != want {
		t.Errorf("inserted %q, want %q", got, want)
	}
	// And the real filename survives, U+202F and all, so the path the model
	// receives is one that actually opens.
	if !strings.Contains(m.input.Value(), "\u202f") {
		t.Error("completion normalised the narrow no-break space away")
	}
}

// Typing inside a quoted reference keeps matching: the space must not end the
// token, which is the whole reason for quoting it.
func TestMenuStaysOpenInsideQuotes(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "my notes.md"))

	m := menuModel(t, dir)
	typeInto(m, `@"my no`)
	if !m.menuActive() {
		t.Fatal("a space inside quotes closed the reference")
	}
	if !strings.Contains(m.pathMenuView(), "my notes.md") {
		t.Errorf("menu does not offer the spaced filename:\n%s", m.pathMenuView())
	}
}

// An '@' in the middle of a word is an email address or a module path, not a
// file reference.
func TestAtMustStartAWord(t *testing.T) {
	for _, in := range []string{"mail nick@example.com", "go get pkg@v1.2.3"} {
		if _, ok := parseAtToken(in); ok {
			t.Errorf("%q was read as a file reference", in)
		}
	}
	for _, in := range []string{"@src", "look at @src", "@"} {
		if _, ok := parseAtToken(in); !ok {
			t.Errorf("%q was not read as a file reference", in)
		}
	}
}

// Completion outside the project. The cached index only covers the working
// directory, so this used to match nothing — which is how a path got typed by
// hand, with the wrong kind of space in it.
func TestMenuCompletesOutsideTheProject(t *testing.T) {
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "report.pdf"))
	project := t.TempDir()

	m := menuModel(t, project)
	typeInto(m, "@"+outside+"/rep")

	if !m.menuActive() {
		t.Fatalf("no menu for an absolute reference")
	}
	if got := m.menu.hits; len(got) == 0 || !strings.HasSuffix(got[0], "report.pdf") {
		t.Fatalf("hits = %v, want the file outside the project", got)
	}
}

func TestTildeReferencesStayShort(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory")
	}
	hits, isFS := matchFilesystemPaths("~/")
	if !isFS {
		t.Fatal("~/ was not treated as a filesystem reference")
	}
	for _, h := range hits {
		if strings.HasPrefix(h, home) {
			t.Errorf("hit %q expanded the home directory instead of keeping ~", h)
		}
	}
}

// The menu describes the current line. Anything that replaces the line without
// going through the key path — history recall, submit — must not leave it up.
func TestMenuGoesAwayWhenTheLineChanges(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "alpha.go"))

	m := menuModel(t, dir)
	typeInto(m, "@alp")
	if m.pathMenuView() == "" {
		t.Fatal("menu never opened")
	}
	m.input.SetValue("something else entirely")
	if m.pathMenuView() != "" {
		t.Error("menu survived the line it was describing")
	}
	if m.menuActive() {
		t.Error("stale menu would still swallow Up/Down")
	}
}

// The reported bug: "@ only works outside of thinking mode". You can type a
// follow-up while the model works, but the dropdown was gated on stateIdle, so
// the box that accepted the keystrokes offered no completion for them.
//
// This is the same shape as TestPasteGateMatchesTheEditableStates: what it pins
// down is the SET of states, which is the part a future state can still get
// wrong. Both now ask editableInput, so they cannot disagree by construction.
func TestMenuShowsInEveryEditableState(t *testing.T) {
	states := []struct {
		name     string
		state    uiState
		editable bool
	}{
		{"idle", stateIdle, true},
		{"running", stateRunning, true},
		{"other-answer", stateAnsweringOther, true},
		{"permission", stateAwaitingPermission, false},
		{"answer picker", stateAwaitingAnswer, false},
		{"plan y/n", stateAwaitingPlan, false},
		{"confirm y/n", stateAwaitingConfirm, false},
		{"choice", stateAwaitingChoice, false},
	}
	for _, tt := range states {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			mustWrite(t, filepath.Join(dir, "alpha.go"))
			m := menuModel(t, dir)

			// Open the menu on the idle line, then move to the state under test.
			// A state change can arrive (a permission prompt, a turn starting)
			// while the dropdown is up, so this also covers the menu having to
			// get out of the way of a prompt that owns the keyboard.
			typeInto(m, "look at @alp")
			if m.pathMenuView() == "" {
				t.Fatal("menu never opened on the idle line")
			}
			m.setState(tt.state)

			if shown := m.pathMenuView() != ""; shown != tt.editable {
				t.Errorf("menu on screen = %v, want %v", shown, tt.editable)
			}
			if active := m.menuActive(); active != tt.editable {
				t.Errorf("menu owns Up/Down = %v, want %v", active, tt.editable)
			}
			if inBottom := strings.Contains(m.bottomView(), "alpha.go"); inBottom != tt.editable {
				t.Errorf("menu rendered under the box = %v, want %v", inBottom, tt.editable)
			}
		})
	}
}

// Typing a reference into a queued follow-up: the whole flow through the real
// key path while the model is working. Tab completes, the arrows pick.
func TestMenuKeysWorkWhileTheModelIsWorking(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "alpha.go"))
	mustWrite(t, filepath.Join(dir, "alpine.go"))

	m := menuModel(t, dir)
	m.setState(stateRunning)
	typeInto(m, "also check @alp")

	if m.pathMenuView() == "" {
		t.Fatal("no dropdown while the model works")
	}
	m.onKey(tea.KeyPressMsg{Code: tea.KeyTab})
	first := m.input.Value()
	if !strings.HasPrefix(first, "also check @") || strings.HasSuffix(first, "@alp") {
		t.Fatalf("Tab did not complete the reference: %q", first)
	}
	m.onKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if second := m.input.Value(); second == first {
		t.Errorf("Down did not move the selection: still %q", second)
	}
}

// Up with the menu open belongs to the menu, not to the queued message. Taking
// the queued text back is what Up means on a line with no dropdown on it.
func TestMenuUpDoesNotRecallTheQueuedMessage(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "alpha.go"))

	m := menuModel(t, dir)
	m.setState(stateRunning)
	m.steer.add("queued text", "queued text")
	typeInto(m, "@alp")

	m.onKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.input.Value(); strings.Contains(got, "queued text") {
		t.Errorf("Up recalled the queued message over the open menu: %q", got)
	}
	if !strings.Contains(m.input.Value(), "alpha.go") {
		t.Errorf("Up did not pick from the menu: %q", m.input.Value())
	}
}

// Esc closes the dropdown. It must not also kill the turn behind it — the
// second Esc does that, once there is nothing on screen to close.
func TestMenuEscDoesNotInterruptTheTurn(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "alpha.go"))

	m := menuModel(t, dir)
	m.setState(stateRunning)
	interrupted := false
	m.turnCancel = func() { interrupted = true }
	typeInto(m, "@alp")

	m.onKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if interrupted {
		t.Fatal("closing the dropdown interrupted the turn")
	}
	if m.pathMenuView() != "" {
		t.Error("Esc did not close the menu")
	}
	m.onKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !interrupted {
		t.Error("a second Esc, with the menu gone, did not interrupt")
	}
}

// Slash completion follows the submit path, not the box. "Answer in your own
// words" sends its text as an answer, so a leading "/" there is a character and
// offering to complete it would be a lie.
func TestSlashCompletionFollowsTheSubmitPath(t *testing.T) {
	for _, tt := range []struct {
		name  string
		state uiState
		live  bool
	}{
		{"idle", stateIdle, true},
		{"running", stateRunning, true},
		{"other-answer", stateAnsweringOther, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := menuModel(t, t.TempDir())
			m.setState(tt.state)
			m.input.SetValue("/mod")
			m.onKey(tea.KeyPressMsg{Code: tea.KeyTab})

			if completed := m.input.Value() != "/mod"; completed != tt.live {
				t.Errorf("Tab completed the command = %v, want %v (input %q)", completed, tt.live, m.input.Value())
			}
			if shown := strings.Contains(m.bottomView(), "/model"); shown != tt.live {
				t.Errorf("suggestion line on screen = %v, want %v", shown, tt.live)
			}
		})
	}
}
