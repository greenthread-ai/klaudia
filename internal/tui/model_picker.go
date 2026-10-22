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
	// The picker filters and scrolls, so the whole list is offered. It used to
	// be cut to nine — the length of the digit row — which is how an endpoint
	// serving thirty models answered /model with "the rest are reachable by
	// typing an id", the one thing the picker exists to spare you.
	current := api.ResolveModel(m.sess.Model)
	items := make([]choiceItem, 0, len(models))
	for _, mi := range models {
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
			apply: func() string { return m.setModel(mi.ID, mi.ContextWindow, mi.MaxTokens) },
		})
	}

	m.startChoice(fmt.Sprintf("Select a model (%d):", len(items)), items)
}

// setModel switches the model for subsequent turns. The provider's own figures
// for the chosen model are recorded alongside it: those are authoritative,
// unlike the static per-model tables, so the status bar's `ctx N%` tracks the
// model the user actually chose and the next turn asks for as many output
// tokens as that model really allows.
//
// contextWindow and maxTokens are 0 when the endpoint did not say — every
// OpenAI-compatible server, and the `/model <id>` path, which has no listing to
// read. Both then fall back to the offline table.
func (m *Model) setModel(id string, contextWindow, maxTokens int) string {
	id = strings.TrimSpace(id)
	m.sess.Model = id
	m.sess.ResolvedModel = string(api.ResolveModel(id))

	msg := "Model set to " + id
	if resolved := m.sess.ResolvedModel; resolved != id {
		msg += " (" + resolved + ")"
	}
	if contextWindow > 0 {
		m.sess.ContextWindow = contextWindow
		m.sess.ContextWindowSource = api.LimitSourceProvider
	} else if limit, source := api.ContextWindow(id, 0); limit > 0 {
		m.sess.ContextWindow, m.sess.ContextWindowSource = limit, source
	} else {
		// A model the table doesn't know and the endpoint didn't size. Leaving
		// the previous model's window would report context pressure against
		// the wrong number, which is worse than reporting none.
		m.sess.ContextWindow, m.sess.ContextWindowSource = 0, api.ContextSourceUnknown
	}
	// The output cap has no "unknown" state — some number goes on the wire — so
	// it always ends up set, and the floor is the last resort.
	if maxTokens > 0 {
		m.sess.MaxTokens, m.sess.MaxTokensSource = maxTokens, api.LimitSourceProvider
	} else if n := api.MaxOutputTokens(id); n != api.DefaultMaxOutputTokens {
		m.sess.MaxTokens, m.sess.MaxTokensSource = n, api.LimitSourceModel
	} else {
		m.sess.MaxTokens, m.sess.MaxTokensSource = api.DefaultMaxOutputTokens, api.LimitSourceFallback
	}
	if m.sess.ContextWindow > 0 {
		msg += fmt.Sprintf(" · %s context", humanTokens(int64(m.sess.ContextWindow)))
	}
	msg += fmt.Sprintf(" · %s max output", humanTokens(int64(m.sess.MaxTokens)))
	// A model switch can strand the live effort level: xhigh is fine on Opus 5
	// and a 400 on Opus 4.6. Drop to the ceiling and say so, rather than
	// letting the next turn fail on a setting the user made before the switch.
	if emsg := api.EffortError(id, m.sess.Effort); emsg != "" {
		_, highest := api.EffortSupported(id, m.sess.Effort)
		m.sess.Effort = highest
		msg += ". " + emsg + " — effort lowered to " + string(highest)
	}
	return msg + ". Applies to the next turn."
}
