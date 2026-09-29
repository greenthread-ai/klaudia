package tui

import (
	"fmt"
	"sort"
	"strings"
)

// Klaudia works in the foreground for minutes at a time: a turn runs, or it
// stops to ask permission, and the user has looked away in the meantime. A
// terminal can raise a hand for exactly this — the bell, or an OSC desktop
// notification — but which escape a given terminal honours is not uniform, so
// the mechanisms are opt-in and can be combined. The sequences are emitted
// through View like the OSC 52 clipboard escape (see clipboard.go), never
// written to a file descriptor directly, so they stay ordered with the frame
// the renderer is drawing.

// NotifyModes is the set of terminal-attention mechanisms enabled for a
// session. The zero value emits nothing.
type NotifyModes struct {
	Bell   bool // the terminal bell, \a (BEL) — the most widely supported.
	OSC9   bool // OSC 9 desktop notification (iTerm2, kitty, WezTerm, …).
	OSC777 bool // OSC 777 desktop notification (rxvt/urxvt and others).
}

// Enabled reports whether any mechanism is on.
func (n NotifyModes) Enabled() bool { return n.Bell || n.OSC9 || n.OSC777 }

// ParseNotify turns the config `tui.notify` string into the enabled mechanisms.
// It is a comma-separated list of "bell", "osc9" and "osc777"; "all" enables
// every mechanism and "off"/"none"/"false" disables all of them. Unset ("")
// defaults to the bell alone: the most portable choice and the least likely to
// nag someone whose terminal turns the bell into a visual flash. An unknown
// token is warned about and ignored rather than left to silently mean nothing.
func ParseNotify(setting string, warn func(string)) NotifyModes {
	s := strings.TrimSpace(setting)
	if s == "" {
		return NotifyModes{Bell: true}
	}
	var n NotifyModes
	for _, tok := range strings.Split(s, ",") {
		switch strings.ToLower(strings.TrimSpace(tok)) {
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
				warn(fmt.Sprintf("unknown tui.notify value %q in config; ignoring (valid: bell, osc9, osc777, all, off)", strings.TrimSpace(tok)))
			}
		}
	}
	return n
}

// notifySequence builds the escape bytes for the enabled mechanisms carrying
// message. It is empty when nothing is enabled, so a caller can concatenate it
// unconditionally. The OSC forms use BEL (\a) as the string terminator, which
// every target terminal accepts and which keeps the payload byte-for-byte
// comparable in tests (no ST/ESC-backslash variability).
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
		// OSC 9 ; <text> BEL
		b.WriteString("\x1b]9;")
		b.WriteString(body)
		b.WriteByte('\a')
	}
	if n.OSC777 {
		// OSC 777 ; notify ; <title> ; <body> BEL
		b.WriteString("\x1b]777;notify;Klaudia;")
		b.WriteString(body)
		b.WriteByte('\a')
	}
	return b.String()
}

// notifyAttention queues an attention notification for the next frame, if the
// session enabled any mechanism and the terminal is not known to be focused.
// message is the body shown by the OSC desktop-notification forms.
func (m *Model) notifyAttention(message string) {
	if !m.sess.Notify.Enabled() {
		return
	}
	// Suppress only when we positively know the window is focused. A terminal
	// that never reports focus leaves focusKnown false, so it still gets
	// notified rather than silently never.
	if m.focusKnown && m.focused {
		return
	}
	m.pendingNotify += notifySequence(m.sess.Notify, message)
}

// sanitizeNotify strips the control bytes that would terminate an OSC string
// early (BEL and ESC) so an attention message drawn from arbitrary text — a
// tool name in a permission prompt, say — cannot break out of the sequence or
// smuggle a second escape after it.
func sanitizeNotify(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\a' || r == '\x1b' || r < 0x20 {
			return -1
		}
		return r
	}, s)
}

// String renders the enabled mechanisms for /config-style display, e.g.
// "bell, osc9" or "off".
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
