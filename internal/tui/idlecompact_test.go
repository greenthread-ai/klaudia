package tui

import (
	"context"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

func idleModel(t *testing.T, resident int) (*Model, chan bool) {
	t.Helper()
	m := newTestModel()
	m.resize(100, 30)
	m.ctx = t.Context()
	m.sess.ContextWindow = 200_000 // compacts above 167k
	m.sess.IdleCompact = true
	m.history = []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))}
	m.residentTokens = resident
	ran := make(chan bool, 1)
	m.sess.Compact = func(context.Context, []anthropic.BetaMessageParam) ([]anthropic.BetaMessageParam, string, error) {
		ran <- true
		return nil, "summary", nil
	}
	return m, ran
}

// Close to the threshold and idle: compacted now, rather than mid-turn later.
func TestIdleCompactionRunsWhenDue(t *testing.T) {
	m, ran := idleModel(t, 150_000)
	if m.scheduleIdleCompact() == nil {
		t.Fatal("not scheduled")
	}
	if cmd := m.onIdleCompact(idleCompactMsg{seq: m.idleSeq}); cmd == nil {
		t.Fatal("did not compact")
	}
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("compaction never ran")
	}
}

// Not when it would not happen soon anyway, not with a draft typed, not once
// another turn has started, and not when turned off.
func TestIdleCompactionHoldsBack(t *testing.T) {
	for name, setup := range map[string]func(*Model) idleCompactMsg{
		"far from the threshold": func(m *Model) idleCompactMsg {
			m.residentTokens = 50_000
			m.scheduleIdleCompact()
			return idleCompactMsg{seq: m.idleSeq}
		},
		"a draft is typed": func(m *Model) idleCompactMsg {
			m.scheduleIdleCompact()
			m.input.SetValue("half a thought")
			return idleCompactMsg{seq: m.idleSeq}
		},
		"a turn started since": func(m *Model) idleCompactMsg {
			m.scheduleIdleCompact()
			msg := idleCompactMsg{seq: m.idleSeq}
			m.idleSeq++ // what startTurn does
			return msg
		},
	} {
		m, _ := idleModel(t, 150_000)
		if cmd := m.onIdleCompact(setup(m)); cmd != nil {
			t.Errorf("%s: compacted", name)
		}
	}
	m, _ := idleModel(t, 150_000)
	m.sess.IdleCompact = false
	if m.scheduleIdleCompact() != nil {
		t.Error("scheduled while turned off")
	}
}
