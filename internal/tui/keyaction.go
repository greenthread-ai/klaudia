package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
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
// The thing to know before changing this: a terminal cannot distinguish
// Ctrl+Return from Return. Both send CR (0x0D); Ctrl+J is LF (0x0A), which is
// why it is the traditional "newline" chord. The only ways out are the Kitty
// keyboard protocol — which bubbletea v1 does not parse, and which would arrive
// here as literal "13;5u" text in the prompt — or a terminal that is configured
// to send a different byte sequence for the chord.
//
// So the internal contract is alt+Return, which every terminal can produce
// (ESC CR) and bubbletea already parses. A user who wants Ctrl+Return maps it
// to ESC CR in their terminal; kitty, Ghostty, WezTerm and iTerm2 can all do
// that. Apple Terminal cannot map Return at all, but sends ESC CR for
// Option+Return when "Use Option as Meta key" is on.
//
// Ctrl+J is kept in both modes as the escape hatch: it needs no terminal
// support whatsoever, so a user who cannot produce alt+Return in "newline"
// mode is never left unable to submit.
func keyAction(msg tea.KeyMsg, enterInserts bool) inputAction {
	switch {
	case msg.Type == tea.KeyEnter && msg.Alt:
		if enterInserts {
			return actionSubmit
		}
		return actionNewline
	case msg.Type == tea.KeyCtrlJ:
		if enterInserts {
			return actionSubmit
		}
		return actionNewline
	case msg.Type == tea.KeyEnter:
		if enterInserts {
			return actionNewline
		}
		return actionSubmit
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
