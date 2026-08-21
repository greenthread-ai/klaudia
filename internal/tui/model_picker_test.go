package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/api"
)

func pickerModel(t *testing.T, models []api.ModelInfo, err error) *Model {
	t.Helper()
	m := newTestModel()
	m.resize(100, 40)
	m.ctx = context.Background()
	m.sess.ListModels = func(context.Context) ([]api.ModelInfo, error) { return models, err }
	return m
}

func TestModelNoArgOpensPicker(t *testing.T) {
	m := pickerModel(t, []api.ModelInfo{
		{ID: "claude-opus-5", DisplayName: "Claude Opus 5", ContextWindow: 1_000_000},
		{ID: "claude-sonnet-5", DisplayName: "Claude Sonnet 5", ContextWindow: 1_000_000},
	}, nil)

	_, cmd := m.handleSlash("/model")
	if cmd == nil {
		t.Fatal("/model with no argument should fetch the model list")
	}
	msg, ok := cmd().(modelsMsg)
	if !ok {
		t.Fatalf("expected a modelsMsg, got %T", cmd())
	}
	m.showModelPicker(msg.models)

	if m.state != stateAwaitingChoice {
		t.Fatalf("state = %v, want a picker", m.state)
	}
	out := visibleText(m.transcript.String())
	for _, want := range []string{"Claude Opus 5", "claude-sonnet-5", "1.0M ctx"} {
		if !strings.Contains(out, want) {
			t.Errorf("picker missing %q:\n%s", want, out)
		}
	}
}

func TestModelPickerMarksCurrent(t *testing.T) {
	m := pickerModel(t, nil, nil)
	m.sess.Model = "claude-sonnet-5"
	m.showModelPicker([]api.ModelInfo{
		{ID: "claude-opus-5", DisplayName: "Claude Opus 5"},
		{ID: "claude-sonnet-5", DisplayName: "Claude Sonnet 5"},
	})
	out := visibleText(m.transcript.String())
	sonnet := strings.Index(out, "Claude Sonnet 5")
	current := strings.Index(out, "(current)")
	if sonnet < 0 || current < sonnet {
		t.Errorf("the active model should be marked current:\n%s", out)
	}
}

// An alias must resolve before comparison, or "opus" never matches its id.
func TestModelPickerMarksCurrentThroughAlias(t *testing.T) {
	m := pickerModel(t, nil, nil)
	m.sess.Model = "opus"
	m.showModelPicker([]api.ModelInfo{{ID: "claude-opus-5", DisplayName: "Claude Opus 5"}})
	if !strings.Contains(visibleText(m.transcript.String()), "(current)") {
		t.Error("an aliased current model should still be marked")
	}
}

func TestSelectingModelRecordsProviderContextWindow(t *testing.T) {
	m := pickerModel(t, nil, nil)
	m.showModelPicker([]api.ModelInfo{
		{ID: "claude-opus-5", DisplayName: "Claude Opus 5", ContextWindow: 1_000_000},
	})
	if len(m.choiceItems) != 1 {
		t.Fatalf("expected one choice, got %d", len(m.choiceItems))
	}
	line := m.choiceItems[0].apply()

	if m.sess.Model != "claude-opus-5" {
		t.Errorf("model = %q", m.sess.Model)
	}
	if m.sess.ContextWindow != 1_000_000 {
		t.Errorf("context window = %d, want the provider's figure", m.sess.ContextWindow)
	}
	if m.sess.ContextWindowSource != "provider" {
		t.Errorf("source = %q, want provider", m.sess.ContextWindowSource)
	}
	if !strings.Contains(line, "1.0M context") {
		t.Errorf("confirmation should state the window: %q", line)
	}
}

// A provider that reports no window (OpenAI-compatible servers don't) must fall
// back to the static table rather than zeroing the status bar.
func TestSelectingModelFallsBackToStaticWindow(t *testing.T) {
	m := pickerModel(t, nil, nil)
	m.setModel("claude-haiku-4-5", 0)
	if m.sess.ContextWindow != 200_000 {
		t.Errorf("context window = %d, want the table's 200K fallback", m.sess.ContextWindow)
	}
}

func TestModelWithArgumentStillSetsDirectly(t *testing.T) {
	m := pickerModel(t, nil, nil)
	if _, cmd := m.handleSlash("/model claude-fable-5"); cmd != nil {
		t.Error("/model <id> should apply immediately, not fetch")
	}
	if m.sess.Model != "claude-fable-5" {
		t.Errorf("model = %q", m.sess.Model)
	}
}

func TestModelPickerCapsAtNineAndSaysSo(t *testing.T) {
	many := make([]api.ModelInfo, 15)
	for i := range many {
		many[i] = api.ModelInfo{ID: string(rune('a'+i)) + "-model"}
	}
	m := pickerModel(t, nil, nil)
	m.showModelPicker(many)

	if len(m.choiceItems) != 9 {
		t.Errorf("picker offers %d items; digit selection caps at 9", len(m.choiceItems))
	}
	if !strings.Contains(visibleText(m.transcript.String()), "6 more") {
		t.Error("truncation must be stated, not silent")
	}
}

func TestModelFetchFailureKeepsTypingAvailable(t *testing.T) {
	m := pickerModel(t, nil, errors.New("connection refused"))
	_, cmd := m.handleSlash("/model")
	msg := cmd().(modelsMsg)
	if msg.err == nil {
		t.Fatal("expected the error to propagate")
	}
	model, _ := m.Update(msg)
	m = model.(*Model)
	out := visibleText(m.transcript.String())
	if !strings.Contains(out, "/model <id>") {
		t.Errorf("a failed lookup should point at the manual route:\n%s", out)
	}
	if m.state == stateAwaitingChoice {
		t.Error("a failed lookup should not open an empty picker")
	}
}

// A provider with no enumeration must keep the old report-the-current-model
// behaviour rather than erroring.
func TestModelWithoutListerReportsCurrent(t *testing.T) {
	m := newTestModel()
	m.resize(100, 40)
	m.sess.ResolvedModel = "claude-opus-5"
	if _, cmd := m.handleSlash("/model"); cmd != nil {
		t.Error("no lister means no fetch")
	}
	if !strings.Contains(visibleText(m.transcript.String()), "claude-opus-5") {
		t.Error("should still report the current model")
	}
}

// End to end through the real provider: /model → live endpoint → picker.
// Skipped without credentials.
func TestModelPickerAgainstLiveProvider(t *testing.T) {
	cred, err := api.ResolveCredential()
	if err != nil {
		t.Skip("no credentials")
	}
	client := api.New(cred, "")

	m := newTestModel()
	m.resize(100, 40)
	m.ctx = context.Background()
	m.sess.ListModels = client.ListModels

	_, cmd := m.handleSlash("/model")
	if cmd == nil {
		t.Fatal("/model should fetch")
	}
	msg, ok := cmd().(modelsMsg)
	if !ok {
		t.Fatalf("got %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("live fetch failed: %v", msg.err)
	}
	model, _ := m.Update(msg)
	m = model.(*Model)

	if m.state != stateAwaitingChoice {
		t.Fatalf("state = %v, want a picker", m.state)
	}
	if len(m.choiceItems) == 0 {
		t.Fatal("picker is empty")
	}
	out := visibleText(m.transcript.String())
	t.Logf("picker:\n%s", out)

	// Selecting the first entry must set both the model and its window.
	line := m.choiceItems[0].apply()
	if m.sess.Model == "" {
		t.Error("selection did not set a model")
	}
	if m.sess.ContextWindow <= 0 {
		t.Error("selection did not record a context window")
	}
	t.Logf("selected: %s", line)
}
