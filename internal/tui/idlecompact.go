package tui

import (
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/pkg/compaction"
)

// Idle compaction: a conversation close to the compaction point will be
// compacted on its next turn anyway, mid-turn, with the user waiting. Doing it
// while the session sits idle costs the same summary and nobody waits — and
// done before the provider's prompt cache expires (five minutes by default),
// the summary request reads the conversation from cache.
//
// Only what would be compacted soon anyway: at idleCompactShare of the
// threshold and up. Never with a draft in the input box, never mid-turn, and
// never once the user has started another turn. idleCompact = false in config
// turns it off.

// idleCompactAfter is how long the session must sit idle: under the cache's
// five minutes, long enough that someone reading the answer is not
// interrupted by a compaction they did not ask for.
var idleCompactAfter = 4 * time.Minute

// idleCompactShare is the share of the compaction threshold that qualifies.
const idleCompactShare = 0.85

// idleCompactMsg fires after idleCompactAfter; seq ties it to the idle spell
// that scheduled it.
type idleCompactMsg struct{ seq int }

// scheduleIdleCompact arms the timer for this idle spell, if it could apply.
func (m *Model) scheduleIdleCompact() tea.Cmd {
	if m.sess == nil || !m.sess.IdleCompact || m.sess.Compact == nil {
		return nil
	}
	m.idleSeq++
	seq := m.idleSeq
	return tea.Tick(idleCompactAfter, func(time.Time) tea.Msg { return idleCompactMsg{seq: seq} })
}

// idleCompactDue reports whether the conversation is close enough to the
// compaction point to compact now.
func (m *Model) idleCompactDue() bool {
	if m.sess == nil || m.sess.ContextWindow <= 0 || len(m.history) == 0 {
		return false
	}
	limit := compaction.ComputeThresholds(m.sess.ContextWindow).CompactThreshold
	return limit > 0 && float64(m.residentTokens) >= idleCompactShare*float64(limit)
}

// onIdleCompact compacts if the session is still in the same idle spell, with
// nothing typed, and close to the threshold.
func (m *Model) onIdleCompact(msg idleCompactMsg) tea.Cmd {
	if msg.seq != m.idleSeq || m.state != stateIdle || m.input.Value() != "" || !m.idleCompactDue() {
		return nil
	}
	m.appendLine(bannerStyle.Render("Compacting while idle — the context is near the compaction point, " +
		"so the next turn will not stop for it (idleCompact = false turns this off)…"))
	m.setState(stateRunning)
	go func(hist []anthropic.BetaMessageParam) {
		newHist, summary, err := m.sess.Compact(m.ctx, hist)
		m.events <- compactDoneMsg{history: newHist, summary: summary, err: err}
	}(m.history)
	return m.spin.Tick
}
