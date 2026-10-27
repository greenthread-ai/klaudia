package tui

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/anthropicauth"
	"github.com/greenthread-ai/klaudia/pkg/api"
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
	out := pickerView(m)
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
	out := pickerView(m)
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
	m.showModelPicker([]api.ModelInfo{{ID: "claude-opus-5-5", DisplayName: "Claude Opus 5.5"}})
	if !strings.Contains(pickerView(m), "(current)") {
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
	m.setModel("claude-haiku-4-5", 0, 0)
	if m.sess.ContextWindow != 200_000 {
		t.Errorf("context window = %d, want the table's 200K fallback", m.sess.ContextWindow)
	}
}

// The output cap has to move with the model. /model used to record only the
// context window, so switching from an 8192-floor model to Opus 5.5 (128k) or
// back left the turn asking for the wrong number — and the 8192 direction is
// what truncated a large Write mid-tool-call.
func TestSelectingModelRecordsOutputCap(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		liveMax    int
		wantMax    int
		wantSource string
		wantInMsg  string
	}{
		{
			name:       "the provider's figure wins",
			id:         "claude-opus-5-5",
			liveMax:    128000,
			wantMax:    128000,
			wantSource: api.LimitSourceProvider,
			wantInMsg:  "128.0k max output",
		},
		{
			name:       "a provider that reports no cap falls back to the table",
			id:         "claude-haiku-4-5",
			liveMax:    0,
			wantMax:    64000,
			wantSource: api.LimitSourceModel,
			wantInMsg:  "64.0k max output",
		},
		{
			name:       "a model nothing can size reports the floor, and says so",
			id:         "some-local-model",
			liveMax:    0,
			wantMax:    api.DefaultMaxOutputTokens,
			wantSource: api.LimitSourceFallback,
			wantInMsg:  "8.2k max output",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := pickerModel(t, nil, nil)
			line := m.setModel(tc.id, 0, tc.liveMax)
			if m.sess.MaxTokens != tc.wantMax {
				t.Errorf("MaxTokens = %d, want %d", m.sess.MaxTokens, tc.wantMax)
			}
			if m.sess.MaxTokensSource != tc.wantSource {
				t.Errorf("MaxTokensSource = %q, want %q", m.sess.MaxTokensSource, tc.wantSource)
			}
			if !strings.Contains(line, tc.wantInMsg) {
				t.Errorf("confirmation %q should state %q", line, tc.wantInMsg)
			}
		})
	}
}

// Switching to a model neither the endpoint nor the table can size must clear
// the previous model's window, not keep reporting pressure against it.
func TestSelectingUnsizedModelClearsStaleWindow(t *testing.T) {
	m := pickerModel(t, nil, nil)
	m.setModel("claude-opus-5-5", 0, 0)
	if m.sess.ContextWindow != 1_000_000 {
		t.Fatalf("setup: window = %d", m.sess.ContextWindow)
	}
	m.setModel("some-local-model", 0, 0)
	if m.sess.ContextWindow != 0 {
		t.Errorf("window = %d, want 0 — the old model's 1M must not be reused", m.sess.ContextWindow)
	}
	if m.sess.ContextWindowSource != api.ContextSourceUnknown {
		t.Errorf("source = %q, want %q", m.sess.ContextWindowSource, api.ContextSourceUnknown)
	}
}

// `/model <id>` computed a confirmation and threw it away, so the one path that
// can land on the 8192 floor was also the only silent one.
func TestModelWithArgumentReportsWhatItSet(t *testing.T) {
	m := pickerModel(t, nil, nil)
	m.handleSlash("/model claude-opus-5-5")
	out := visibleText(m.transcript.String())
	for _, want := range []string{"claude-opus-5-5", "128.0k max output"} {
		if !strings.Contains(out, want) {
			t.Errorf("transcript should mention %q:\n%s", want, out)
		}
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

// The nine-item cap is gone: the picker filters and scrolls, so a long list is
// offered whole. What a short terminal shows is a window onto it, not a
// truncation of it — everything stays reachable by typing.
func TestModelPickerOffersEveryModel(t *testing.T) {
	many := make([]api.ModelInfo, 15)
	for i := range many {
		many[i] = api.ModelInfo{ID: string(rune('a'+i)) + "-model"}
	}
	m := pickerModel(t, nil, nil)
	m.showModelPicker(many)

	if len(m.choiceItems) != 15 {
		t.Errorf("picker offers %d of 15 models", len(m.choiceItems))
	}
	if !strings.Contains(pickerView(m), "(15)") {
		t.Errorf("the title should say how many there are:\n%s", pickerView(m))
	}
	// The fifteenth is off the bottom of the window but one filter away.
	typeInto(m, "o-mo")
	if len(m.choiceHits) != 1 {
		t.Fatalf("filter %q matched %d models, want 1", m.choiceFilter, len(m.choiceHits))
	}
	if !strings.Contains(pickerView(m), "o-model") {
		t.Errorf("filtering should surface the entry the window hid:\n%s", pickerView(m))
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
// Opt-in, like the model-table check in pkg/api: it used to run whenever a
// credential could be found — the Keychain login included — so a plain
// `go test` on a developer's machine called the real API with their account.
func TestModelPickerAgainstLiveProvider(t *testing.T) {
	if os.Getenv("KLAUDIA_LIVE_MODELS") == "" {
		t.Skip("set KLAUDIA_LIVE_MODELS=1 to run against the real provider")
	}
	cred, err := anthropicauth.ResolveCredential()
	if err != nil {
		t.Skip("no credentials")
	}
	client := anthropicauth.NewClient(cred, "", api.Tuning{})

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
	t.Logf("picker:\n%s", pickerView(m))

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
