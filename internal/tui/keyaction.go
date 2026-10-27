package tui

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// inputAction is what a keypress means at the prompt.
type inputAction int

const (
	actionNone inputAction = iota
	actionNewline
	actionSubmit
)

// keyAction decides whether a keypress submits the prompt or inserts a newline.
//
// The thing to know before changing this: a terminal sends the same byte, CR
// (0x0D), for Return, Shift+Return and Ctrl+Return — unless it implements the
// Kitty keyboard protocol or xterm's modifyOtherKeys, which Klaudia asks for
// (inline.go) and Bubble Tea v2 parses. Where one is on, Shift+Enter and
// Ctrl+Enter arrive as themselves and are bound; where neither is (Apple
// Terminal), they arrive as Enter, so the bindings that work everywhere stay.
//
// Those are alt+Return, which every terminal can produce (ESC CR), and Ctrl+J
// (LF, 0x0A), the traditional newline chord, which needs no terminal support
// whatsoever — so a user who cannot produce alt+Return in "newline" mode is
// never left unable to submit. Apple Terminal sends ESC CR for Option+Return
// when "Use Option as Meta key" is on.
//
// The chords themselves live in newKeymap, so the binding that matches here and
// the help text that documents it are the same object.
func keyAction(msg tea.KeyPressMsg, enterInserts bool) inputAction {
	return newKeymap(enterInserts).action(msg)
}

// action is keyAction against an already-built keymap, for the hot path where
// the Model holds one.
func (k keymap) action(msg tea.KeyPressMsg) inputAction {
	switch {
	case key.Matches(msg, k.Submit):
		return actionSubmit
	case key.Matches(msg, k.Newline):
		return actionNewline
	}
	return actionNone
}

// EnterInserts reports whether config asked for Return to insert a newline.
// Anything other than "newline" or "send" is warned about and treated as
// "send": a mistyped key here must not leave the prompt unable to send.
func EnterInserts(setting string, warn func(string)) bool {
	switch setting {
	case "", "send":
		return false
	case "newline":
		return true
	default:
		if warn != nil {
			warn(fmt.Sprintf("unknown input.enter %q in config; using \"send\" (valid: send, newline)", setting))
		}
		return false
	}
}
