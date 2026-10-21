package api

import (
	"context"
	"strings"
	"time"
)

// Why this file exists.
//
// The per-model tables in client.go are an allowlist, and an allowlist of model
// ids is wrong the day a model ships. When Claude 5.5 arrived, MaxOutputTokens
// returned the 8192 unknown-model floor for Opus 5.5 — against a real cap of
// 128000 — so a routine large Write hit the output limit mid-tool-call and was
// refused by the truncation guard. Nothing errored; the model just appeared to
// be unable to write a long file.
//
// The fix is not a better table. Both backends can already say what they serve,
// and ListModels was already returning the right number in ModelInfo.MaxTokens
// — it was fetched and thrown away. So the table becomes the offline fallback
// and the endpoint becomes the authority, which is the same policy
// EffortSupported already applies ("the server is the authority on its own
// capabilities").

// limitsLookupTimeout bounds the startup lookup. A wedged or offline endpoint
// must cost a short pause and fall back to the table, never block the run: this
// is an optimisation of a working path, not a prerequisite for it.
const limitsLookupTimeout = 5 * time.Second

// Limit source labels, for /doctor and /stats.
const (
	LimitSourceConfig   = "config override"
	LimitSourceProvider = "provider"
	LimitSourceModel    = "model default"
	LimitSourceFallback = "unknown model — using the safe floor"
)

// ModelLimits is the per-request sizing for one model: how many output tokens
// to ask for, and how much input the model can hold. Both carry the source they
// came from so /doctor can say whether a number is authoritative or a guess.
type ModelLimits struct {
	MaxOutputTokens int
	MaxOutputSource string
	ContextWindow   int
	ContextSource   string
}

// ResolveLimits determines the output cap and context window for model.
//
// Precedence, highest first: an explicit config override, the provider's own
// answer, the offline table, the unknown-model floor.
//
// The provider is only consulted for a model the table does not know. That is
// deliberate on both halves: a known model already has the right answer offline,
// so the usual run pays no network at all; and an unknown model is precisely the
// case that was silently getting the floor. p may be nil or may not implement
// ModelLister, in which case this is pure table lookup.
func ResolveLimits(ctx context.Context, p Provider, model string, cfgMaxTokens, cfgContextWindow int) ModelLimits {
	resolved := string(ResolveModel(model))
	out := ModelLimits{}

	if n, ok := modelMaxOutputTokens[resolved]; ok {
		out.MaxOutputTokens, out.MaxOutputSource = n, LimitSourceModel
	}
	if n, ok := modelContextWindows[resolved]; ok {
		out.ContextWindow, out.ContextSource = n, LimitSourceModel
	}

	// Ask the endpoint only for what the table could not answer.
	if out.MaxOutputTokens == 0 || out.ContextWindow == 0 {
		if info, ok := lookupModel(ctx, p, resolved); ok {
			if out.MaxOutputTokens == 0 && info.MaxTokens > 0 {
				out.MaxOutputTokens, out.MaxOutputSource = info.MaxTokens, LimitSourceProvider
			}
			if out.ContextWindow == 0 && info.ContextWindow > 0 {
				out.ContextWindow, out.ContextSource = info.ContextWindow, LimitSourceProvider
			}
		}
	}

	// Still nothing: an OpenAI-compatible endpoint that reports neither. The
	// output cap must be *some* number to put on the wire, so it takes the
	// conservative floor. The context window stays 0, which ShouldAutocompact
	// reads as "assume DefaultContextWindow" — guessing high there would skip
	// compaction until the request 400s, so unknown deliberately stays unknown.
	if out.MaxOutputTokens == 0 {
		out.MaxOutputTokens, out.MaxOutputSource = DefaultMaxOutputTokens, LimitSourceFallback
	}
	if out.ContextWindow == 0 {
		out.ContextSource = ContextSourceUnknown
	}

	// Config last, because it must beat every discovered value: it is the
	// escape hatch for an endpoint that reports the wrong thing, and an
	// override that could be silently outvoted is not an override.
	if cfgMaxTokens > 0 {
		out.MaxOutputTokens, out.MaxOutputSource = cfgMaxTokens, LimitSourceConfig
	}
	if cfgContextWindow > 0 {
		out.ContextWindow, out.ContextSource = cfgContextWindow, LimitSourceConfig
	}
	return out
}

// lookupModel finds one model in the provider's list. Any failure — no lister,
// no credential, network, timeout, model absent — is reported as "not found",
// because every one of them has the same correct response: use the fallback.
func lookupModel(ctx context.Context, p Provider, model string) (ModelInfo, bool) {
	lister, ok := p.(ModelLister)
	if !ok || lister == nil {
		return ModelInfo{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, limitsLookupTimeout)
	defer cancel()
	models, err := lister.ListModels(ctx)
	if err != nil {
		return ModelInfo{}, false
	}
	for _, m := range models {
		if m.ID == model {
			return m, true
		}
	}
	// A server-side alias can answer under a different id than it is listed by
	// (OpenAI-compatible gateways prefix with the vendor, e.g. "openai/gpt-5.5"
	// listed as "gpt-5.5"), so fall back to a suffix match before giving up.
	for _, m := range models {
		if m.ID != "" && strings.HasSuffix(model, "/"+m.ID) {
			return m, true
		}
	}
	return ModelInfo{}, false
}
