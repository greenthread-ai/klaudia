package tui

import (
	"runtime"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
)

// keymap is every chord the prompt handles itself, with the help text that
// documents it attached to the binding that implements it.
//
// The point is that there is no second copy. /help used to render a hand-kept
// const listing the bindings, written out beside the code that matched them:
// adding the @ menu meant editing both, and nothing failed if you edited one.
// Worse, two of the lines were conditional on config and the const could not
// say so — with input.enter = "newline" the help and the intro both still said
// Enter sends, which it does not.
//
// So the bindings are built from the same setting that decides the behaviour,
// and the help is rendered from the bindings.
type keymap struct {
	Submit    key.Binding
	Newline   key.Binding
	Complete  key.Binding
	Prev      key.Binding
	Next      key.Binding
	Dismiss   key.Binding
	Interrupt key.Binding
	// Suspend, Clear and Quit are the terminal's own chords, which raw mode
	// delivers as keys: nothing happens unless the app acts on them.
	Suspend key.Binding
	Clear   key.Binding
	Quit    key.Binding
}

// newKeymap builds the bindings for a Return mode. See keyAction for why the
// chords are what they are — in particular why alt+Return rather than
// Ctrl+Return is the internal contract.
func newKeymap(enterInserts bool) keymap {
	// Shift+Enter and Ctrl+Enter are bound everywhere: a terminal that cannot
	// tell them from Enter never sends them. They are only *named* once the
	// terminal has said it can (keysDisambiguated), so the help never offers
	// a chord that would send the prompt instead.
	newlineLabel, sendLabel := altReturnLabel(), altReturnLabel()
	if keysDisambiguated {
		newlineLabel, sendLabel = "Shift+"+returnKey(), "Ctrl+"+returnKey()
	}
	submit := key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "Send the prompt"))
	newline := key.NewBinding(key.WithKeys("shift+enter", "ctrl+j", "alt+enter"),
		key.WithHelp(newlineLabel, "Newline without sending (or end the line with \\)"))
	if enterInserts {
		submit = key.NewBinding(key.WithKeys("ctrl+enter", "ctrl+j", "alt+enter"), key.WithHelp(sendLabel, "Send the prompt"))
		newline = key.NewBinding(key.WithKeys("enter", "shift+enter"), key.WithHelp("Enter", "Newline without sending"))
	}
	// Prev and Next share a description on purpose: keyHelp merges bindings
	// that mean the same thing into one row, so this renders as "↑ / ↓".
	const history = "Previous prompts (queued message first), or move in the @ menu"
	return keymap{
		Submit:    submit,
		Newline:   newline,
		Complete:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "Complete a /command or an @<path> reference")),
		Prev:      key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", history)),
		Next:      key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", history)),
		Dismiss:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "Dismiss the file menu, or interrupt the model mid-turn")),
		Interrupt: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("Ctrl+C", "Interrupt, or clear the line — press twice to quit")),
		Suspend:   key.NewBinding(key.WithKeys("ctrl+z"), key.WithHelp("Ctrl+Z", "Suspend to the shell (fg to come back)")),
		Clear:     key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("Ctrl+L", "Clear the screen (scrollback is kept)")),
		Quit:      key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("Ctrl+D", "Quit, on an empty prompt")),
	}
}

// helpOrder is the order /help lists the bindings in, including the two the
// textarea itself owns. Those are read from its live keymap rather than
// restated, so the help cannot claim an editing chord the widget does not bind.
func (k keymap) helpOrder(ta textarea.KeyMap) []key.Binding {
	return []key.Binding{
		k.Submit,
		k.Newline,
		k.Complete,
		k.Prev,
		k.Next,
		ta.DeleteBeforeCursor,
		ta.DeleteAfterCursor,
		k.Dismiss,
		k.Interrupt,
		k.Suspend,
		k.Clear,
		k.Quit,
	}
}

// inputPrefixes are the two things you type rather than press. They are not key
// bindings and deliberately are not pretending to be: nothing matches on them,
// because "/" and "@" are ordinary characters that the line is then inspected
// for. Keeping them out of the keymap is what stops the help documenting a
// chord that no code looks for.
var inputPrefixes = [][2]string{
	{"/", "Type a slash to see matching commands"},
	{"@", "Pick a file to mention (Tab cycles, Esc dismisses)"},
}

// keyHelp renders the "Keys:" block of /help from the bindings themselves.
// Bindings that share a description are merged into one row, so ↑ and ↓ read
// as "↑ / ↓" without that row being written out by hand anywhere.
func keyHelp(km keymap, ta textarea.KeyMap) string {
	type row struct{ keys, desc string }
	var rows []row
	for _, p := range inputPrefixes {
		rows = append(rows, row{p[0], p[1]})
	}
	for _, b := range km.helpOrder(ta) {
		h := b.Help()
		if h.Key == "" || h.Desc == "" {
			continue
		}
		desc := capitalise(h.Desc)
		if n := len(rows); n > 0 && rows[n-1].desc == desc {
			rows[n-1].keys += " / " + chordCase(h.Key)
			continue
		}
		rows = append(rows, row{chordCase(h.Key), desc})
	}

	width := 0
	for _, r := range rows {
		if w := lipgloss.Width(r.keys); w > width {
			width = w
		}
	}
	var b strings.Builder
	b.WriteString("Keys:")
	for _, r := range rows {
		b.WriteString("\n  " + r.keys + strings.Repeat(" ", width-lipgloss.Width(r.keys)) + "  " + r.desc)
	}
	return b.String()
}

// chordCase and capitalise exist because two of the rows come from bubbles'
// own textarea keymap, which writes its help in lower case ("ctrl+u", "delete
// before cursor"). Restating those bindings in house style would mean claiming
// chords the widget owns — the drift this file removes — so the presentation
// is normalised at render time instead, for every row alike.
func chordCase(k string) string {
	parts := strings.Split(k, "+")
	for i, p := range parts {
		parts[i] = capitalise(p)
	}
	return strings.Join(parts, "+")
}

func capitalise(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(unicode.ToUpper(r[0])) + string(r[1:])
}

// keymap returns the prompt's bindings for this session's Return mode.
//
// Derived rather than cached on the Model. A cached field is zero-valued in any
// Model built as a struct literal — every test helper in this package does that
// — and a zero keymap matches nothing, so the prompt would silently stop
// responding to Enter. Building seven bindings per keystroke is free at human
// typing speed.
func (m *Model) keymap() keymap {
	return newKeymap(m.sess.EnterInserts)
}

// keyHints renders the key reference for /help from the live bindings.
func (m *Model) keyHints() string {
	return keyHelp(m.keymap(), m.input.KeyMap)
}

// keysDisambiguated is set when the terminal reports key disambiguation (the
// Kitty keyboard protocol), so Shift+Enter arrives as itself. Bubble Tea v2
// parses it; Klaudia's renderer asks for it (inline.go).
var keysDisambiguated bool

// goos is runtime.GOOS, indirected so tests can render either platform's help.
var goos = runtime.GOOS

// altReturnLabel names the Alt+Return chord the way the keyboard in front of
// the user labels it: Option+Return on a Mac, Alt+Enter elsewhere.
func altReturnLabel() string {
	if goos == "darwin" {
		return "Option+Return"
	}
	return "Alt+Enter"
}

// returnKey is what the keyboard calls the key: Return on a Mac.
func returnKey() string {
	if goos == "darwin" {
		return "Return"
	}
	return "Enter"
}
