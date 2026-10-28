package tui

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestParseNotify(t *testing.T) {
	for _, tc := range []struct {
		setting string
		env     map[string]string
		want    NotifyModes
	}{
		{"", nil, NotifyModes{Bell: true}},
		{"auto", map[string]string{"TERM_PROGRAM": "iTerm.app"}, NotifyModes{OSC9: true}},
		{"", map[string]string{"TERM_PROGRAM": "ghostty"}, NotifyModes{OSC9: true}},
		{"", map[string]string{"TERM": "xterm-ghostty"}, NotifyModes{OSC9: true}},
		// tmux: the OSC forms need passthrough; the bell always arrives.
		{"", map[string]string{"TERM_PROGRAM": "iTerm.app", "TMUX": "/tmp/tmux"}, NotifyModes{Bell: true}},
		{"", map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, NotifyModes{Bell: true}},
		{"bell, osc777", nil, NotifyModes{Bell: true, OSC777: true}},
		{"all", nil, NotifyModes{Bell: true, OSC9: true, OSC777: true}},
		{"off", nil, NotifyModes{}},
	} {
		if got := ParseNotify(tc.setting, env(tc.env), nil); got != tc.want {
			t.Errorf("ParseNotify(%q, %v) = %+v, want %+v", tc.setting, tc.env, got, tc.want)
		}
	}
	var warned []string
	ParseNotify("bell,trumpet", nil, func(m string) { warned = append(warned, m) })
	if len(warned) != 1 || !strings.Contains(warned[0], "trumpet") {
		t.Errorf("unknown value not warned about: %q", warned)
	}
}

// A message drawn from the model's text cannot end the OSC string early or
// carry an escape of its own — C1's string terminator included.
func TestNotifySequenceIsSanitised(t *testing.T) {
	got := notifySequence(NotifyModes{OSC9: true}, "rm -rf\a\x1b]9;pwned\x1b\\\u009cend")
	if got != "\x1b]9;rm -rf]9;pwned\\end\a" {
		t.Errorf("sequence = %q", got)
	}
	if notifySequence(NotifyModes{}, "x") != "" {
		t.Error("nothing enabled should send nothing")
	}
}

// Quiet while the terminal says the user is looking; otherwise the escape
// and the hook, including on a terminal that never reports focus.
func TestNotifyAttentionRespectsFocus(t *testing.T) {
	var mu sync.Mutex
	var hooked []string
	m := newTestModel()
	m.sess.Notify = NotifyModes{Bell: true}
	m.sess.OnNotify = func(reason, msg string) {
		mu.Lock()
		hooked = append(hooked, reason)
		mu.Unlock()
	}

	m.notifyAttention("question", "q") // focus never reported
	if m.pendingRaw != "\a" {
		t.Fatalf("unknown focus: raw = %q, want a bell", m.pendingRaw)
	}
	m.pendingRaw = ""
	m.update(tea.FocusMsg{})
	m.notifyAttention("question", "q")
	if m.pendingRaw != "" {
		t.Errorf("focused: raw = %q, want nothing", m.pendingRaw)
	}
	m.update(tea.BlurMsg{})
	m.notifyAttention("plan", "p")
	if m.pendingRaw != "\a" {
		t.Errorf("blurred: raw = %q, want a bell", m.pendingRaw)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(hooked)
		mu.Unlock()
		if n == 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	sort.Strings(hooked) // each runs on its own goroutine
	if strings.Join(hooked, ",") != "plan,question" {
		t.Errorf("hook ran for %v, want plan and question (not while focused)", hooked)
	}
}

// The moments: a permission prompt, a question, a plan, and a long turn
// ending — not a quick one, and not one the user interrupted.
func TestNotifyTriggers(t *testing.T) {
	ask := func(m *Model, msg tea.Msg) string {
		m.pendingRaw = ""
		m.update(msg)
		return m.pendingRaw
	}
	m := newTestModel()
	m.resize(100, 30)
	m.sess.Notify = NotifyModes{OSC9: true}
	reply := make(chan permission.Decision, 1)
	if got := ask(m, permissionMsg{req: agent.ApprovalRequest{ToolName: "Bash", Input: []byte(`{"command":"make"}`)}, reply: reply}); !strings.Contains(got, "needs your permission") {
		t.Errorf("permission prompt: %q", got)
	}
	m = newTestModel()
	m.resize(100, 30)
	m.sess.Notify = NotifyModes{OSC9: true}
	if got := ask(m, askMsg{question: "Which one?", reply: make(chan string, 1)}); !strings.Contains(got, "Which one?") {
		t.Errorf("question: %q", got)
	}

	for _, tc := range []struct {
		elapsed time.Duration
		err     error
		want    string
	}{
		{25 * time.Second, nil, "Klaudia finished"},
		{25 * time.Second, errors.New("boom"), "error"},
		{5 * time.Second, nil, ""},
		{25 * time.Second, context.Canceled, ""},
	} {
		m := newTestModel()
		m.sess.Notify = NotifyModes{OSC9: true}
		m.notifyTurnEnd(tc.elapsed, tc.err)
		if (tc.want == "") != (m.pendingRaw == "") || !strings.Contains(m.pendingRaw, tc.want) {
			t.Errorf("turn of %v ending with %v: raw = %q, want %q", tc.elapsed, tc.err, m.pendingRaw, tc.want)
		}
	}
}
