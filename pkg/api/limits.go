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
	// LimitSourceRejected is set mid-run, not by ResolveLimits: the provider
	// refused the cap we sent and named its own, and the loop adopted it. The
	// startup label is wrong from that point, so it cannot be kept.
	LimitSourceRejected = "provider rejected the requested cap"
)

// ModelLimits is the per-request sizing for one model: how many output tokens
// to ask for, and how much input the model can hold. Both carry the source they
// came from so /doctor can say whether a number is authoritative or a guess.
type ModelLimits struct {
	MaxOutputTokens int
	MaxOutputSource string
	ContextWindow   int
	ContextSource   string
	// TableMaxOutputTokens and TableContextWindow are what the offline table
	// said when a provider figure replaced it with a different number; 0 when
	// they agree or the table has no entry. A non-zero value means the table
	// is wrong for this model, which /doctor reports so it gets fixed.
	TableMaxOutputTokens int
	TableContextWindow   int
}

// LimitsInput is everything Resolve may consult.
type LimitsInput struct {
	Model string
	// Provider is the configured provider name ("" = Anthropic). Off
	// Anthropic, Model is not alias-expanded before the table lookup, so
	// "sonnet" on another endpoint is not sized as claude-sonnet.
	Provider string
	// Lister is the provider's model list, or nil. A *ModelCache is read from
	// disk first; anything else is only called for a model the table lacks.
	Lister ModelLister
	// Known is what the caller already holds about this model from the
	// provider — the /model picker has just listed it. Beats the table.
	Known ModelInfo
	// NoNetwork skips the synchronous lookup. For a caller on the UI
	// goroutine, which must not stall on a 5s timeout when offline.
	NoNetwork bool
	// ConfigMaxTokens and ConfigContextWindow are .klaudia/config.toml
	// overrides; they beat everything.
	ConfigMaxTokens     int
	ConfigContextWindow int
}

// cachedLister is implemented by *ModelCache.
type cachedLister interface {
	Cached() ([]ModelInfo, bool)
}

// Resolve determines the output cap and context window for a model. It is the
// one sizing resolver: startup and /model both call it.
//
// Precedence, highest first: config, the provider (figures the caller already
// holds, then a fresh cached list, then — only for a model the table lacks — a
// live lookup), the offline table, the unknown-model floor.
//
// The provider used to be consulted only for models the table did not know,
// so a wrong table entry was never contradicted. And /model resolved sizing on
// its own with the opposite precedence and no config, so a maxTokens override
// silently stopped applying after any model switch.
func Resolve(ctx context.Context, in LimitsInput) ModelLimits {
	resolved := string(ResolveModelFor(in.Provider, in.Model))
	out := ModelLimits{}

	tableMax, tableCtx := modelMaxOutputTokens[resolved], modelContextWindows[resolved]
	if tableMax > 0 {
		out.MaxOutputTokens, out.MaxOutputSource = tableMax, LimitSourceModel
	}
	if tableCtx > 0 {
		out.ContextWindow, out.ContextSource = tableCtx, LimitSourceModel
	}

	info, have := in.Known, in.Known.MaxTokens > 0 || in.Known.ContextWindow > 0
	if !have {
		if c, ok := in.Lister.(cachedLister); ok {
			if models, fresh := c.Cached(); fresh {
				info, have = findModel(models, resolved)
			}
		}
	}
	// The network only for what nothing else could answer, so a known model
	// on a stale cache costs no startup latency; the caller refreshes the
	// cache in the background instead.
	if !have && !in.NoNetwork && (tableMax == 0 || tableCtx == 0) {
		info, have = lookupModel(ctx, in.Lister, resolved)
	}
	if have {
		if info.MaxTokens > 0 {
			if tableMax > 0 && tableMax != info.MaxTokens {
				out.TableMaxOutputTokens = tableMax
			}
			out.MaxOutputTokens, out.MaxOutputSource = info.MaxTokens, LimitSourceProvider
		}
		if info.ContextWindow > 0 {
			if tableCtx > 0 && tableCtx != info.ContextWindow {
				out.TableContextWindow = tableCtx
			}
			out.ContextWindow, out.ContextSource = info.ContextWindow, LimitSourceProvider
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
	if in.ConfigMaxTokens > 0 {
		out.MaxOutputTokens, out.MaxOutputSource = in.ConfigMaxTokens, LimitSourceConfig
	}
	if in.ConfigContextWindow > 0 {
		out.ContextWindow, out.ContextSource = in.ConfigContextWindow, LimitSourceConfig
	}
	return out
}

// ResolveLimits is Resolve for a Provider, which is a ModelLister when it can
// list models.
func ResolveLimits(ctx context.Context, p Provider, model string, cfgMaxTokens, cfgContextWindow int) ModelLimits {
	lister, _ := p.(ModelLister)
	return Resolve(ctx, LimitsInput{Model: model, Lister: lister, ConfigMaxTokens: cfgMaxTokens, ConfigContextWindow: cfgContextWindow})
}

// lookupModel finds one model in the provider's list. Any failure — no lister,
// no credential, network, timeout, model absent — is reported as "not found",
// because every one of them has the same correct response: use the fallback.
func lookupModel(ctx context.Context, lister ModelLister, model string) (ModelInfo, bool) {
	if lister == nil {
		return ModelInfo{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, limitsLookupTimeout)
	defer cancel()
	models, err := lister.ListModels(ctx)
	if err != nil {
		return ModelInfo{}, false
	}
	return findModel(models, model)
}

// FindModel is findModel for callers outside the package.
func FindModel(models []ModelInfo, model string) (ModelInfo, bool) { return findModel(models, model) }

// findModel matches an id exactly, then by suffix: a server-side alias can
// answer under a different id than it is listed by (OpenAI-compatible
// gateways prefix with the vendor, e.g. "openai/gpt-5.5" listed as "gpt-5.5").
func findModel(models []ModelInfo, model string) (ModelInfo, bool) {
	for _, m := range models {
		if m.ID == model {
			return m, true
		}
	}
	for _, m := range models {
		if m.ID != "" && strings.HasSuffix(model, "/"+m.ID) {
			return m, true
		}
	}
	return ModelInfo{}, false
}
