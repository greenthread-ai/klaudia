package api

import (
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// EffortLevels are the values output_config.effort accepts, lowest first.
// "" (unset) sends no effort, which the API treats as the model's default.
var EffortLevels = []string{"low", "medium", "high", "xhigh", "max"}

// Thinking modes. ThinkingDefault sends no thinking parameter at all, so each
// model runs its own default: adaptive on Claude Opus 5, Sonnet 5 and the
// Fable line, no thinking on Opus 4.8/4.7 and older.
const (
	ThinkingDefault  = ""
	ThinkingAdaptive = "adaptive"
	ThinkingDisabled = "disabled"
)

// ParseEffort normalises an effort setting. "", "default" and "auto" all mean
// "send nothing". Anything else must be one of EffortLevels.
func ParseEffort(s string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	switch v {
	case "", "default", "auto":
		return "", nil
	}
	for _, l := range EffortLevels {
		if v == l {
			return v, nil
		}
	}
	return "", fmt.Errorf("unknown effort %q (want one of %s, or default)", s, strings.Join(EffortLevels, ", "))
}

// ParseThinking normalises a thinking setting: "" / "default" leaves it to the
// model, "adaptive" (or "on") asks for adaptive thinking, "disabled" (or "off")
// turns it off where the model allows that.
func ParseThinking(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "default", "auto":
		return ThinkingDefault, nil
	case "adaptive", "on":
		return ThinkingAdaptive, nil
	case "disabled", "off":
		return ThinkingDisabled, nil
	}
	return "", fmt.Errorf("unknown thinking mode %q (want adaptive, disabled, or default)", s)
}

// ApplyReasoning sets output_config.effort and the thinking config on params
// from the user's settings, adjusted to what params.Model is known to accept.
// The settings follow the session, not the model, so a /model switch must not
// turn them into a 400 on every turn:
//
//   - Haiku 4.5, Sonnet 4.5 and Claude 3.x reject effort outright: it is dropped.
//   - xhigh arrived with Opus 4.7, and Opus 4.5 stops at high: a level the model
//     lacks is lowered to the highest one it has.
//   - Thinking cannot be disabled on the Fable/Mythos line or Opus 5.5, nor on
//     Opus 5 at xhigh/max: "disabled" is dropped there and the model's
//     default (thinking on) applies.
//
// Models this table does not know — newer Claude models, OpenAI-compatible
// ids — get the settings unchanged; the API's error is clearer than a guess.
func ApplyReasoning(params *anthropic.BetaMessageNewParams, effort, thinking string) {
	model := strings.ToLower(string(params.Model))
	effort = effortFor(model, effort)
	if effort != "" {
		params.OutputConfig.Effort = anthropic.BetaOutputConfigEffort(effort)
	}
	switch thinking {
	case ThinkingAdaptive:
		params.Thinking = anthropic.BetaThinkingConfigParamUnion{OfAdaptive: &anthropic.BetaThinkingConfigAdaptiveParam{}}
	case ThinkingDisabled:
		if thinkingCanBeDisabled(model, effort) {
			d := anthropic.NewBetaThinkingConfigDisabledParam()
			params.Thinking = anthropic.BetaThinkingConfigParamUnion{OfDisabled: &d}
		}
	}
}

// effortFor returns the effort level to send for model, or "" for none.
func effortFor(model, effort string) string {
	if effort == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(model, "claude-haiku-4-5"),
		strings.HasPrefix(model, "claude-sonnet-4-5"),
		strings.HasPrefix(model, "claude-3"):
		return ""
	case strings.HasPrefix(model, "claude-opus-4-5"):
		if effort == "xhigh" || effort == "max" {
			return "high"
		}
	case strings.HasPrefix(model, "claude-opus-4-6"),
		strings.HasPrefix(model, "claude-sonnet-4-6"):
		if effort == "xhigh" {
			return "high"
		}
	}
	return effort
}

// thinkingCanBeDisabled reports whether {type: "disabled"} is accepted.
func thinkingCanBeDisabled(model, effort string) bool {
	switch {
	case strings.HasPrefix(model, "claude-fable-"),
		strings.HasPrefix(model, "claude-mythos-"),
		strings.HasPrefix(model, "claude-opus-5-5"):
		return false
	case model == "claude-opus-5":
		// Opus 5 accepts disabled only at high or below.
		return effort != "xhigh" && effort != "max"
	}
	return true
}

// openAIReasoningEffort maps an effort level onto Chat Completions'
// reasoning_effort. low/medium/high are the values OpenAI-compatible servers
// agree on; xhigh and max have no portable equivalent, so they ask for high
// rather than risk a 400 from a server that does not know them.
func openAIReasoningEffort(effort anthropic.BetaOutputConfigEffort) string {
	switch effort {
	case "":
		return ""
	case "xhigh", "max":
		return "high"
	}
	return string(effort)
}
