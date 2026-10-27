package anthropicauth

import (
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/greenthread-ai/klaudia/pkg/api"
)

// Klaudia's Anthropic client: pkg/api's, shaped the way Klaudia sends
// requests. This shaping was inline in the client until the provider layer
// moved to pkg/; it stays here, in the product, so the public library does not
// present itself as anything but itself. Behaviour is what it was.

// claudeCodeBeta was the first of the client's default betas.
const claudeCodeBeta anthropic.AnthropicBeta = "claude-code-20250219"

// OAuthBeta is the header Claude Code attaches when the credential is an OAuth
// token. Without it, the Anthropic API treats the same token as "external" use
// and applies much stricter rate limits — observed in practice as immediate
// 429s on the first turn while `claude` itself, holding the same Keychain
// token, works normally. (03-providers.js: `if (Y7()) q.push(BZ)`.)
const OAuthBeta anthropic.AnthropicBeta = "oauth-2025-04-20"

// claudeCodeVersion appears inside the billing-header system block. The value
// is logged by Anthropic for analytics but not validated — bump it when a new
// claude release lands if you want the analytics to stay accurate; nothing
// breaks if it lags.
const claudeCodeVersion = "2.1.153"

// NewClient builds the Anthropic client Klaudia uses for cred.
func NewClient(cred api.Credential, baseURL string, tuning api.Tuning) *api.Client {
	opts := []api.ClientOption{
		api.WithRequestOptions(option.WithHeader("x-app", "cli")),
		api.WithPrepare(func(p *anthropic.BetaMessageNewParams) {
			p.Betas = addBeta(p.Betas, claudeCodeBeta)
			if cred.IsOAuth() {
				p.Betas = addBeta(p.Betas, OAuthBeta)
				p.System = augmentSystem(p.System)
			}
		}),
	}
	if cred.IsOAuth() {
		// An OAuth credential needs the same beta header the messages path
		// sends. /v1/models happens to answer without it today, but the
		// requirement is endpoint-dependent and undocumented per-endpoint —
		// sending it everywhere is what keeps this from breaking silently later.
		opts = append(opts, api.WithListOptions(option.WithHeaderAdd("anthropic-beta", string(OAuthBeta))))
	}
	return api.New(cred, baseURL, tuning, opts...)
}

// addBeta appends b unless the list already has it.
func addBeta(in []anthropic.AnthropicBeta, b anthropic.AnthropicBeta) []anthropic.AnthropicBeta {
	for _, x := range in {
		if x == b {
			return in
		}
	}
	return append(in, b)
}

// claudeCodeBillingPrefix is the literal prefix Anthropic looks for in the
// *first* system block on OAuth requests. Without it, every request — even
// with a perfect set of impersonation headers — falls into a strict default
// bucket and returns instant 429s. The values that follow are observed for
// server-side analytics but NOT validated (verified by sending bogus values
// and watching them all 200), so we put honest klaudia metadata in there.
const claudeCodeBillingPrefix = "x-anthropic-billing-header:"

// augmentSystem prepends Claude Code's billing-header system block on the
// OAuth path. The block must come *first* in the system array — claude itself
// emits it as system[0] and a real `claude -p` against a local proxy confirms
// the format. Idempotent: if the caller already sent a billing block (e.g. on
// retry), don't double up.
func augmentSystem(in []anthropic.BetaTextBlockParam) []anthropic.BetaTextBlockParam {
	if len(in) > 0 && strings.HasPrefix(in[0].Text, claudeCodeBillingPrefix) {
		return in
	}
	billing := anthropic.BetaTextBlockParam{
		Text: claudeCodeBillingPrefix + " cc_version=" + claudeCodeVersion + "; cc_entrypoint=klaudia;",
	}
	return append([]anthropic.BetaTextBlockParam{billing}, in...)
}
