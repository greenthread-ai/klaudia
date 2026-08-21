package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/internal/api"
)

// Choosing a model by typing its exact id means knowing the id, and model ids
// change more often than anyone's memory of them does. Both backends Klaudia
// speaks to can enumerate what they serve, so /model asks the endpoint and
// offers the answer as a picker; typing an id still works for pinning something
// the endpoint doesn't list.

// modelFetchTimeout bounds the lookup so a wedged endpoint can't leave /model
// hanging with no way back.
const modelFetchTimeout = 10 * time.Second

// fetchModels asks the provider for its model list off the UI goroutine.
func (m *Model) fetchModels() tea.Cmd {
	list := m.sess.ListModels
	parent := m.ctx
	if parent == nil {
		parent = context.Background()
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, modelFetchTimeout)
		defer cancel()
		models, err := list(ctx)
		return modelsMsg{models: models, err: err}
	}
}

// showModelPicker turns the fetched list into the standard numbered picker.
func (m *Model) showModelPicker(models []api.ModelInfo) {
	if len(models) == 0 {
		m.appendLine(bannerStyle.Render("The provider reported no models. /model <id> still works."))
		return
	}
	// The picker is digit-selected, so it can only offer nine. The list arrives
	// newest-first from Anthropic and sorted from OpenAI-compatible endpoints,
	// so the truncation keeps the most useful end either way — and says so
	// rather than silently hiding the rest.
	shown := models
	truncated := 0
	if len(shown) > 9 {
		truncated = len(shown) - 9
		shown = shown[:9]
	}

	current := api.ResolveModel(m.sess.Model)
	items := make([]choiceItem, 0, len(shown))
	for _, mi := range shown {
		mi := mi
		label := mi.Name()
		if mi.Name() != mi.ID {
			label += "  " + mi.ID
		}
		if mi.ContextWindow > 0 {
			label += fmt.Sprintf("  · %s ctx", humanTokens(int64(mi.ContextWindow)))
		}
		if mi.ID == string(current) {
			label += "  (current)"
		}
		items = append(items, choiceItem{
			label: label,
			apply: func() string { return m.setModel(mi.ID, mi.ContextWindow) },
		})
	}

	title := "Select a model:"
	if truncated > 0 {
		title += fmt.Sprintf("  (%d more — /model <id> to pick one not listed)", truncated)
	}
	m.startChoice(title, items)
}

// setModel switches the model for subsequent turns. A context window from the
// provider is recorded alongside it: that figure is authoritative, unlike the
// static per-model table, so the status bar's `ctx N%` tracks the model the user
// actually chose.
func (m *Model) setModel(id string, contextWindow int) string {
	id = strings.TrimSpace(id)
	m.sess.Model = id
	m.sess.ResolvedModel = string(api.ResolveModel(id))

	msg := "Model set to " + id
	if resolved := m.sess.ResolvedModel; resolved != id {
		msg += " (" + resolved + ")"
	}
	if contextWindow > 0 {
		m.sess.ContextWindow = contextWindow
		m.sess.ContextWindowSource = "provider"
	} else if limit, source := api.ContextWindow(id, 0); limit > 0 {
		m.sess.ContextWindow, m.sess.ContextWindowSource = limit, source
	}
	if m.sess.ContextWindow > 0 {
		msg += fmt.Sprintf(" · %s context", humanTokens(int64(m.sess.ContextWindow)))
	}
	return msg + ". Applies to the next turn."
}
