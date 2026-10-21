package api

import (
	"fmt"
	"strings"
)

// Effort is how many tokens the model is allowed to spend on a response —
// thinking, tool calls and text together. On the current Claude models thinking
// is always on and effort is the control for how deep it goes; budget_tokens is
// the older knob it replaced.
//
// The empty Effort means "unset", and unset sends nothing. That matters twice
// over: it keeps the provider's own default (high on most models, medium on
// Opus 5.5 and Haiku 5.5) rather than this package guessing one, and it keeps
// the field off the wire entirely for OpenAI-compatible endpoints that would
// reject it.
type Effort string

// The five levels, lowest to highest. These are the wire values for both
// providers: Anthropic takes them in output_config.effort and the
// OpenAI-compatible vocabulary is a superset (it adds "none" and "minimal"
// below "low"), so nothing has to be mapped between the two.
const (
	EffortUnset  Effort = ""
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortXHigh  Effort = "xhigh"
	EffortMax    Effort = "max"
)

// EffortLevels lists the settable levels in increasing order, for help text,
// pickers and validation messages.
var EffortLevels = []Effort{EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}

// Describe returns the one-line meaning of a level, for /effort and /doctor.
func (e Effort) Describe() string {
	switch e {
	case EffortLow:
		return "most token-efficient; classification, lookups, high-volume work"
	case EffortMedium:
		return "balanced; agentic work where cost matters"
	case EffortHigh:
		return "spends what the task needs; the default on most models"
	case EffortXHigh:
		return "long-horizon coding and agentic work; the recommended start for coding"
	case EffortMax:
		return "maximum capability, no constraint on token spend"
	}
	return ""
}

func (e Effort) String() string { return string(e) }

// ParseEffort converts user input ("--effort XHigh") into a level. The empty
// string parses to EffortUnset so clearing the setting is expressible.
func ParseEffort(s string) (Effort, error) {
	v := Effort(strings.ToLower(strings.TrimSpace(s)))
	if v == EffortUnset {
		return EffortUnset, nil
	}
	for _, lvl := range EffortLevels {
		if v == lvl {
			return lvl, nil
		}
	}
	return EffortUnset, fmt.Errorf("unknown effort %q (want %s)", s, EffortNames())
}

// EffortNames renders the settable levels for an error or help string.
func EffortNames() string {
	names := make([]string, 0, len(EffortLevels))
	for _, lvl := range EffortLevels {
		names = append(names, string(lvl))
	}
	return strings.Join(names, "|")
}

// modelMaxEffort is the highest level each model accepts. The API rejects an
// unsupported combination with a 400 rather than quietly dropping to the
// nearest level it does support, so a wrong value is a dead session rather than
// a slightly cheaper one — which is why this table exists instead of sending
// whatever the user typed and hoping.
//
// Only three models in the lineup stop short of max: Opus 4.6 and Sonnet
// 4.6 arrived before xhigh existed, and Opus 4.5 (where effort was introduced,
// behind the effort-2025-11-24 beta) has only the original three. The 5.5
// generation takes max — probed live rather than assumed, because a wrong
// ceiling here is a 400 on every turn. Keep this in sync with
// modelContextWindows and modelMaxOutputTokens.
//
// A model that is absent is not "unsupported" — it is unknown, which is the
// normal case for an OpenAI-compatible endpoint. See EffortSupported.
var modelMaxEffort = map[string]Effort{
	"claude-opus-5-5":            EffortMax,
	"claude-sonnet-5-5":          EffortMax,
	"claude-haiku-5-5":           EffortMax,
	"claude-fable-5-1":           EffortMax,
	"claude-opus-5":              EffortMax,
	"claude-sonnet-5":            EffortMax,
	"claude-fable-5":             EffortMax,
	"claude-opus-4-8":            EffortMax,
	"claude-opus-4-7":            EffortMax,
	"claude-opus-4-6":            EffortHigh,
	"claude-sonnet-4-6":          EffortHigh,
	"claude-haiku-4-5":           EffortHigh,
	"claude-haiku-4-5-20251001":  EffortHigh,
	"claude-opus-4-5-20251101":   EffortHigh,
	"claude-sonnet-4-5-20250929": EffortUnset, // effort predates this model entirely
}

// effortRank orders the levels for comparison. Unset ranks below everything so
// a model mapped to EffortUnset supports nothing.
var effortRank = map[Effort]int{
	EffortUnset:  0,
	EffortLow:    1,
	EffortMedium: 2,
	EffortHigh:   3,
	EffortXHigh:  4,
	EffortMax:    5,
}

// EffortSupported reports whether model accepts level, and if not, the highest
// level it does accept. An unknown model — any OpenAI-compatible endpoint, or a
// Claude snapshot newer than this table — is reported as supported: the server
// is the authority on its own capabilities, and refusing locally would be this
// package inventing a limit the provider never stated.
func EffortSupported(model string, level Effort) (ok bool, highest Effort) {
	if level == EffortUnset {
		return true, EffortUnset
	}
	ceiling, known := modelMaxEffort[string(ResolveModel(model))]
	if !known {
		return true, EffortUnset
	}
	return effortRank[level] <= effortRank[ceiling], ceiling
}

// EffortError returns the message for an unsupported model/level pair, or "" if
// the pair is fine. It names the ceiling because "unsupported" on its own
// leaves the user guessing which way to move.
func EffortError(model string, level Effort) string {
	ok, highest := EffortSupported(model, level)
	if ok {
		return ""
	}
	if highest == EffortUnset {
		return fmt.Sprintf("%s does not support the effort parameter", ResolveModel(model))
	}
	return fmt.Sprintf("%s does not support effort %s (highest: %s)", ResolveModel(model), level, highest)
}
