package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Klaudia works in the foreground for minutes at a time: a turn runs, or it
// stops to ask permission, and the user has looked away in the meantime. A
// terminal can raise a hand for exactly this — the bell, or an OSC desktop
// notification — but which escape a given terminal honours is not uniform.
// "auto", the default, picks per terminal; a list chooses outright.
//
// It fires only when the terminal is not known to be focused: Klaudia asks
// for focus reports (inline.go), and a terminal that never sends one is
// treated as unfocused, so it still gets told rather than silently never.
// The same moments run the Notification hook, for a phone or a chat.
//
// The escapes go out through the renderer's raw queue (inline.go), never
// into a frame: frames are fitted, downsampled and superseded before they are
// drawn, and any of those would lose or mangle an escape.
//
// Ported from PR #96, which chose the moments, the parsing and the
// sanitising; the output path, focus mode, "auto" and the hook are new.

// NotifyModes is the set of terminal-attention mechanisms enabled for a
// session. The zero value emits nothing.
type NotifyModes struct {
	Bell   bool // the terminal bell, \a (BEL) — the most widely supported.
	OSC9   bool // OSC 9 desktop notification (iTerm2, WezTerm, Ghostty, …).
	OSC777 bool // OSC 777 desktop notification (rxvt/urxvt, foot and others).
}

// Enabled reports whether any mechanism is on.
func (n NotifyModes) Enabled() bool { return n.Bell || n.OSC9 || n.OSC777 }

// ParseNotify turns the config `[tui] notify` string into the enabled
// mechanisms. Unset or "auto" picks per terminal (autoNotify); otherwise it is
// a comma-separated list of "bell", "osc9" and "osc777", or "all", or "off".
// An unknown token is warned about and ignored rather than left to silently
// mean nothing.
func ParseNotify(setting string, getenv func(string) string, warn func(string)) NotifyModes {
	s := strings.ToLower(strings.TrimSpace(setting))
	if s == "" || s == "auto" {
		return autoNotify(getenv)
	}
	var n NotifyModes
	for _, tok := range strings.Split(s, ",") {
		switch strings.TrimSpace(tok) {
		case "":
			// A stray comma ("bell,") is not worth a warning.
		case "off", "none", "false", "no":
			return NotifyModes{}
		case "all", "true", "yes", "on":
			n.Bell, n.OSC9, n.OSC777 = true, true, true
		case "bell":
			n.Bell = true
		case "osc9", "9":
			n.OSC9 = true
		case "osc777", "777":
			n.OSC777 = true
		default:
			if warn != nil {
				warn(fmt.Sprintf("unknown tui.notify value %q in config; ignoring (valid: auto, bell, osc9, osc777, all, off)", strings.TrimSpace(tok)))
			}
		}
	}
	return n
}

// autoNotify picks the mechanism a terminal is known to turn into a desktop
// notification, and the bell everywhere else. Inside tmux the OSC forms need
// passthrough the user may not have enabled, while the bell always arrives
// (and marks the window), so tmux gets the bell.
func autoNotify(getenv func(string) string) NotifyModes {
	if getenv == nil || getenv("TMUX") != "" {
		return NotifyModes{Bell: true}
	}
	switch getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm", "ghostty":
		return NotifyModes{OSC9: true}
	}
	if getenv("TERM") == "xterm-ghostty" {
		return NotifyModes{OSC9: true}
	}
	return NotifyModes{Bell: true}
}

// notifySequence builds the escape bytes for the enabled mechanisms carrying
// message. It is empty when nothing is enabled. The OSC forms end with BEL,
// which every target terminal accepts.
func notifySequence(n NotifyModes, message string) string {
	if !n.Enabled() {
		return ""
	}
	body := sanitizeNotify(message)
	var b strings.Builder
	if n.Bell {
		b.WriteByte('\a')
	}
	if n.OSC9 {
		b.WriteString("\x1b]9;" + body + "\a")
	}
	if n.OSC777 {
		b.WriteString("\x1b]777;notify;Klaudia;" + body + "\a")
	}
	return b.String()
}

// sanitizeNotify strips control characters — C0 including BEL and ESC, DEL,
// and C1 — so a message drawn from arbitrary text (a tool name, a question
// the model asked) cannot end the OSC string early or smuggle in an escape
// of its own. C1 matters as well as C0: U+009C is a string terminator on
// terminals that read 8-bit controls.
func sanitizeNotify(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}

// String renders the enabled mechanisms for /config, e.g. "bell, osc9" or
// "off".
func (n NotifyModes) String() string {
	var parts []string
	if n.Bell {
		parts = append(parts, "bell")
	}
	if n.OSC9 {
		parts = append(parts, "osc9")
	}
	if n.OSC777 {
		parts = append(parts, "osc777")
	}
	if len(parts) == 0 {
		return "off"
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// notifyAfter is how long a turn must have run for its end to be news. A
// quick exchange is one the user is watching; one that took this long is
// one they may well have walked away from.
const notifyAfter = 20 * time.Second

// notifyTurnEnd notifies that a turn has finished, if it is news: one that
// ran at least notifyAfter, and not one the user interrupted themselves.
func (m *Model) notifyTurnEnd(elapsed time.Duration, err error) {
	switch {
	case elapsed < notifyAfter || errors.Is(err, context.Canceled):
	case err != nil:
		m.notifyAttention("error", "Klaudia stopped on an error")
	default:
		m.notifyAttention("done", "Klaudia finished")
	}
}

// notifyAttention tells the user Klaudia wants them: the terminal's
// notification (per the session's modes) and the Notification hook, both
// only when the terminal is not known to be focused. reason is a short slug
// for the hook ("permission", "question", "plan", "done", "error").
func (m *Model) notifyAttention(reason, message string) {
	if m.sess == nil || (m.focusKnown && m.focused) {
		return
	}
	if seq := notifySequence(m.sess.Notify, message); seq != "" {
		m.emitRaw(seq)
	}
	if m.sess.OnNotify != nil {
		go m.sess.OnNotify(reason, message)
	}
}
