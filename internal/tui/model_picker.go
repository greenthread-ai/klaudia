package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/greenthread-ai/klaudia/pkg/api"
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
func (m *Model) showModelPicker(all []api.ModelInfo) {
	// Only what can hold a conversation. An endpoint that also serves speech,
	// transcription or image models lists them in the same /v1/models, and
	// picking one here used to send the next turn to an endpoint that refuses
	// it.
	models := api.ChatModels(all)
	if len(models) == 0 && len(all) > 0 {
		m.appendLine(bannerStyle.Render(fmt.Sprintf("The provider listed %d models and none of them is a chat model. /model <id> still works.", len(all))))
		return
	}
	if len(models) == 0 {
		m.appendLine(bannerStyle.Render("The provider reported no models. /model <id> still works."))
		return
	}
	// The picker filters and scrolls, so the whole list is offered. It used to
	// be cut to nine — the length of the digit row — which is how an endpoint
	// serving thirty models answered /model with "the rest are reachable by
	// typing an id", the one thing the picker exists to spare you.
	current := api.ResolveModelFor(m.sess.Provider, m.sess.Model)
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

// setModel switches the model for subsequent turns, sized by the same
// resolver as startup (Session.ResolveLimits): config, then the provider's own
// figures (contextWindow and maxTokens here, 0 when the endpoint did not say),
// then the offline table, then the floor.
func (m *Model) setModel(id string, contextWindow, maxTokens int) string {
	id = strings.TrimSpace(id)
	m.sess.Model = id
	m.sess.ResolvedModel = string(api.ResolveModelFor(m.sess.Provider, id))
	aliasWarn := api.AliasWarning(m.sess.Provider, id)

	msg := "Model set to " + id
	if resolved := m.sess.ResolvedModel; resolved != id {
		msg += " (" + resolved + ")"
	}
	known := api.ModelInfo{ID: id, ContextWindow: contextWindow, MaxTokens: maxTokens}
	var l api.ModelLimits
	if m.sess.ResolveLimits != nil {
		l = m.sess.ResolveLimits(id, known)
	} else {
		l = api.Resolve(context.Background(), api.LimitsInput{Model: id, Provider: m.sess.Provider, Known: known, NoNetwork: true})
	}
	// An unknown model the endpoint didn't size gets a window of 0: leaving
	// the previous model's would report context pressure against the wrong
	// number, which is worse than reporting none.
	m.sess.ContextWindow, m.sess.ContextWindowSource = l.ContextWindow, l.ContextSource
	m.sess.MaxTokens, m.sess.MaxTokensSource = l.MaxOutputTokens, l.MaxOutputSource
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
	msg += ". Applies to the next turn."
	if aliasWarn != "" {
		msg += "\n  " + aliasWarn + "."
	}
	return msg
}

// unlistedModelWarning checks a typed /model id against the list the provider
// reported the last time /model fetched it, and returns a warning when the id
// is not in it. It never fetches: a /model <id> that waited on the network
// would be slower than finding out on the next turn, and when nothing has been
// fetched yet there is nothing to say. It warns rather than refuses, because
// an endpoint can serve ids it does not list (aliases, dated snapshots).
// (From PR #79.)
func (m *Model) unlistedModelWarning(id string) string {
	if len(m.knownModels) == 0 {
		return ""
	}
	id = strings.TrimSpace(id)
	resolved := string(api.ResolveModelFor(m.sess.Provider, id))
	for _, mi := range m.knownModels {
		if mi.ID == id || mi.ID == resolved {
			if !mi.Chat() {
				return "  " + mi.ID + " is listed as a " + string(mi.Kind) + " model, not a chat model; the next turn will be refused. Run /model to pick a chat model."
			}
			return ""
		}
	}
	return "  " + id + " is not in the list this endpoint reported; if the next turn says the model was not found, run /model to pick one it lists."
}
