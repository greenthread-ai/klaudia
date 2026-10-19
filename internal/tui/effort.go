package tui

import (
	"fmt"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/api"
)

// setEffort backs /effort: no argument reports the levels and which one is
// live, "off" clears the setting, anything else sets it for subsequent turns.
//
// The level is held on the session and read fresh each turn by the CLI's run
// closure, the same path /model uses, so a change lands on the next turn
// without restarting anything.
func (m *Model) setEffort(args []string) {
	if len(args) == 0 {
		m.appendLine(bannerStyle.Render(m.effortStatus()))
		for _, lvl := range api.EffortLevels {
			marker := "  "
			if lvl == m.sess.Effort {
				marker = "→ "
			}
			m.appendLine(toolStyle.Render(fmt.Sprintf("%s%-7s %s", marker, lvl, lvl.Describe())))
		}
		m.appendLine(toolStyle.Render("  /effort off     restore the provider's own default"))
		return
	}

	arg := strings.ToLower(strings.TrimSpace(args[0]))
	if arg == "off" || arg == "default" || arg == "unset" {
		if m.sess.Effort == api.EffortUnset {
			m.appendLine(bannerStyle.Render("Effort is already the provider's default."))
			return
		}
		m.sess.Effort = api.EffortUnset
		m.appendLine(bannerStyle.Render("Effort cleared — the provider's own default applies from the next turn. " + effortCacheNote))
		return
	}

	level, err := api.ParseEffort(arg)
	if err != nil {
		m.appendLine(errStyle.Render(err.Error()))
		return
	}
	// Catch it here rather than on the wire: the API rejects an unsupported
	// level with a 400 instead of dropping to the nearest one it supports, so
	// without this the next turn simply fails.
	if msg := api.EffortError(m.effortModel(), level); msg != "" {
		m.appendLine(errStyle.Render(msg))
		return
	}
	if level == m.sess.Effort {
		m.appendLine(bannerStyle.Render("Effort is already " + string(level) + "."))
		return
	}
	m.sess.Effort = level
	m.appendLine(bannerStyle.Render("Effort set to " + string(level) + " — " + level.Describe() + ". Applies to the next turn. " + effortCacheNote))
}

// effortCacheNote is attached to every change because the cost is real and
// invisible: changing the top-level effort between requests invalidates the
// cached message blocks for the conversation, so the next turn re-reads the
// whole prefix at full input rate. On a long session that is the single most
// expensive thing a slash command can do.
//
// The cache-preserving alternative is per-message effort, which is beta and
// Anthropic-only — adopting it would make the two providers behave differently,
// which is the thing this feature was built to avoid.
const effortCacheNote = "The prompt cache for this conversation is invalidated by the change, so the next turn re-reads it in full."

// effortModel is the model id to validate a level against — the session's
// choice, falling back to the resolved default.
func (m *Model) effortModel() string {
	if m.sess.Model != "" {
		return m.sess.Model
	}
	return m.sess.ResolvedModel
}

// effortStatus is the one-line description of the live setting, shared by
// /effort and /config.
func (m *Model) effortStatus() string {
	if m.sess.Effort == api.EffortUnset {
		return "Effort: provider default (unset)"
	}
	return "Effort: " + string(m.sess.Effort)
}
