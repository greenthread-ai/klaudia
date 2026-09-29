package tui

import (
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/memory"
)

func TestParseNotify(t *testing.T) {
	tests := []struct {
		in       string
		want     NotifyModes
		wantWarn bool
	}{
		{"", NotifyModes{Bell: true}, false}, // unset default
		{"bell", NotifyModes{Bell: true}, false},
		{"osc9", NotifyModes{OSC9: true}, false},
		{"osc777", NotifyModes{OSC777: true}, false},
		{"bell,osc9", NotifyModes{Bell: true, OSC9: true}, false},
		{" BELL , OSC777 ", NotifyModes{Bell: true, OSC777: true}, false}, // case/space tolerant
		{"all", NotifyModes{Bell: true, OSC9: true, OSC777: true}, false},
		{"true", NotifyModes{Bell: true, OSC9: true, OSC777: true}, false},
		{"off", NotifyModes{}, false},
		{"none", NotifyModes{}, false},
		{"false", NotifyModes{}, false},
		{"bell,off", NotifyModes{}, false}, // off wins as soon as it is seen
		{"bell,", NotifyModes{Bell: true}, false},
		{"bell,bogus", NotifyModes{Bell: true}, true}, // unknown token warns, keeps the rest
	}
	for _, tc := range tests {
		var warned bool
		got := ParseNotify(tc.in, func(string) { warned = true })
		if got != tc.want {
			t.Errorf("ParseNotify(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
		if warned != tc.wantWarn {
			t.Errorf("ParseNotify(%q) warned = %v, want %v", tc.in, warned, tc.wantWarn)
		}
	}
}

func TestNotifySequenceBytes(t *testing.T) {
	tests := []struct {
		name string
		mode NotifyModes
		msg  string
		want string
	}{
		{"disabled", NotifyModes{}, "hi", ""},
		{"bell only", NotifyModes{Bell: true}, "hi", "\a"},
		{"osc9", NotifyModes{OSC9: true}, "done", "\x1b]9;done\a"},
		{"osc777", NotifyModes{OSC777: true}, "done", "\x1b]777;notify;Klaudia;done\a"},
		{"all in order", NotifyModes{Bell: true, OSC9: true, OSC777: true}, "x",
			"\a" + "\x1b]9;x\a" + "\x1b]777;notify;Klaudia;x\a"},
	}
	for _, tc := range tests {
		if got := notifySequence(tc.mode, tc.msg); got != tc.want {
			t.Errorf("%s: notifySequence = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSanitizeNotifyStripsTerminators(t *testing.T) {
	// A crafted message must not be able to close the OSC string early (BEL) or
	// start a second escape (ESC), and other control bytes are dropped too.
	in := "run \a\x1b]0;evil\a tool\nnext"
	got := sanitizeNotify(in)
	if strings.ContainsAny(got, "\a\x1b\n") {
		t.Fatalf("sanitizeNotify left a control byte: %q", got)
	}
	if got != "run ]0;evil toolnext" {
		t.Fatalf("sanitizeNotify = %q", got)
	}
}

func notifyTestModel(mode NotifyModes) *Model {
	return &Model{sess: &Session{Memory: memory.Disabled(), Notify: mode}, focused: true}
}

func TestNotifyAttentionQueues(t *testing.T) {
	m := notifyTestModel(NotifyModes{Bell: true})
	// Focus unknown (terminal hasn't reported): notify regardless.
	m.notifyAttention("done")
	if m.pendingNotify != "\a" {
		t.Fatalf("expected queued bell, got %q", m.pendingNotify)
	}
}

func TestNotifyAttentionDisabled(t *testing.T) {
	m := notifyTestModel(NotifyModes{})
	m.notifyAttention("done")
	if m.pendingNotify != "" {
		t.Fatalf("expected nothing queued when disabled, got %q", m.pendingNotify)
	}
}

func TestNotifyAttentionFocusGating(t *testing.T) {
	m := notifyTestModel(NotifyModes{Bell: true})
	// Terminal has reported focus and the window is focused: stay silent.
	m.focused, m.focusKnown = true, true
	m.notifyAttention("done")
	if m.pendingNotify != "" {
		t.Fatalf("focused window should suppress notification, got %q", m.pendingNotify)
	}
	// Window is now unfocused: notify.
	m.focused = false
	m.notifyAttention("done")
	if m.pendingNotify != "\a" {
		t.Fatalf("unfocused window should notify, got %q", m.pendingNotify)
	}
}
