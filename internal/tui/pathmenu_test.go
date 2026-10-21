package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// typeInto feeds a string through the real key path, so the menu is built the
// way a user builds it rather than by calling refreshPathMenu directly.
func typeInto(m *Model, s string) {
	for _, r := range s {
		m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
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

	m.onKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.menu.idx != 1 {
		t.Fatalf("Down did not move the selection: idx=%d", m.menu.idx)
	}
	if got := m.input.Value(); got != "@"+m.menu.hits[1] {
		t.Errorf("input = %q, want the selected hit", got)
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyUp})
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
	m.onKey(tea.KeyMsg{Type: tea.KeyEsc})

	if m.pathMenuView() != "" {
		t.Error("Esc left the menu on screen")
	}
	if m.input.Value() != "@alp" {
		t.Errorf("Esc changed the input to %q", m.input.Value())
	}
	// Tab brings it back — dismissing is about this keystroke, not the session.
	m.onKey(tea.KeyMsg{Type: tea.KeyTab})
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
	m.onKey(tea.KeyMsg{Type: tea.KeyTab})

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
