package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// Hints are a host's own words for the settings and situations the core's
// messages point a user at — "raise maxRetries in .klaudia/config.toml" is
// right in Klaudia and wrong in anything else that embeds the loop. Each field
// is a phrase dropped into a sentence; an empty one leaves the advice out or
// words it neutrally, so the zero value is correct for any host.
type Hints struct {
	Retries       string // the retry budget setting, e.g. "maxRetries in .klaudia/config.toml"
	IdleTimeout   string // the stream idle timeout setting (seconds, 0 disables)
	ContextWindow string // the context-window setting compaction is sized against
	MaxTokens     string // the output-token cap setting
	Endpoint      string // where the endpoint URL is configured
	Credentials   string // where a custom provider's credentials are configured
	Compact       string // the command that compacts the conversation now, e.g. "/compact"
	// SharedLogin names the sign-in whose token is commonly shared between
	// sessions — a frequent cause of 429s — e.g. "you signed in via Claude
	// Code OAuth". Empty words the cause neutrally.
	SharedLogin string
}

// FriendlyError is FriendlyError with neutral wording (the zero Hints).
func FriendlyError(err error) string { return Hints{}.FriendlyError(err) }

func (h Hints) retryNote() string {
	if h.Retries == "" {
		return ""
	}
	return " (Raise " + h.Retries + " to retry more.)"
}

func (h Hints) sharedCause(capital bool) string {
	if h.SharedLogin == "" {
		if capital {
			return "This often happens when the same credential is in use by another session"
		}
		return "this often happens when the same credential is in use by another session"
	}
	if capital {
		return "If " + h.SharedLogin + " this often happens when the token is in use by another session"
	}
	return "if " + h.SharedLogin + " this often happens when the token is in use by another session"
}

func (h Hints) idleNote() string {
	if h.IdleTimeout == "" {
		return " Send your message again."
	}
	return " Send your message again; tune the window with " + h.IdleTimeout + " (seconds, 0 disables)."
}

// FriendlyError turns an API error into a concise, human-readable message.
// Transient/auth failures (already retried for 429/5xx) get an actionable
// explanation instead of a raw POST dump. Handles both the Anthropic and the
// OpenAI-compatible providers. Non-API errors are returned as-is.
func (h Hints) FriendlyError(err error) string {
	if err == nil {
		return ""
	}
	if status, ok := apiStatus(err); ok {
		switch status {
		case 429:
			// OpenAI: 429 is BOTH transient throttling and "insufficient_quota"
			// (billing). They need very different advice and the provider's own
			// message is usually clearer than ours.
			if oai := openAIPayload(err); oai != nil {
				if codeIs(oai.Code, "insufficient_quota") || oai.Type == "insufficient_quota" {
					return "Insufficient quota: " + strings.TrimSpace(oai.Message) +
						" (Retries won't help — top up your plan/billing or switch to a different key.)"
				}
				if oai.Message != "" {
					return "Rate limited (429): " + strings.TrimSpace(oai.Message) + h.retryNote()
				}
			}
			// Anthropic: pass through whatever the API actually said when it's
			// useful ("5-hour usage limit reached", quota-detail messages, etc.);
			// otherwise demote the upstream noise and promote the OAuth-sharing
			// hint to the primary explanation, since that's the common cause for
			// Claude Code users.
			if errType, msg := anthropicPayload(err); errType != "" || msg != "" {
				out := "Rate limited (429)"
				if errType != "" {
					out += " [" + errType + "]"
				}
				if detail := meaningfulMessage(msg, errType); detail != "" {
					// Claude Max session-limit ("You've hit your session limit,
					// resets at …") doesn't recover within any reasonable retry
					// window, so the generic OAuth-sharing hint is misleading.
					// Tell the user it's a quota wait, not concurrent use.
					if isSessionLimit(detail) {
						return out + ": " + detail + " (Retries won't help — wait for the reset time shown, switch to a key-based provider, or sign in with a different account.)"
					}
					// Long-context-tier credits gate ("Usage credits are required
					// for long context requests"): an entitlement/billing wall, not
					// a transient throttle, so retrying is futile and the
					// OAuth-sharing hint is wrong. Point at the two real fixes —
					// add credits, or shrink the request below the long-context
					// threshold via earlier autocompaction / /compact.
					if isLongContextCredits(detail) {
						reduce := "lower the context window compaction is sized against so it triggers earlier"
						if h.ContextWindow != "" {
							reduce = "lower " + h.ContextWindow + " so autocompaction triggers earlier"
						}
						if h.Compact != "" {
							reduce += ", and run " + h.Compact
						}
						return out + ": " + detail + " (Retries won't help — this needs pay-as-you-go usage credits for the long-context tier. Add credits, or reduce context: " + reduce + ".)"
					}
					note := " (" + h.sharedCause(true) + "."
					if h.Retries != "" {
						note += " Raise " + h.Retries + " to retry more."
					}
					return out + ": " + detail + note + ")"
				}
				return out + ": " + h.sharedCause(false) + " — try closing other sessions and waiting a moment." + h.retryNote()
			}
			// Fallback (no parseable provider payload). The OAuth-token-sharing
			// cause is Anthropic-specific, so still include it when the error
			// came from that provider.
			out := "Rate limited (429): the API is busy and retries were exhausted. "
			var anthErr *anthropic.Error
			if errors.As(err, &anthErr) {
				if h.SharedLogin != "" {
					out += "If " + h.SharedLogin + ", this often happens when the token is in use by another session. "
				} else {
					out += h.sharedCause(true) + ". "
				}
			}
			return out + "Wait a moment and try again." + h.retryNote()
		case 400:
			// A 400 is usually the model rejecting an input shape, but two
			// patterns are really "your conversation outgrew the model's context
			// window": some OpenAI-compatible providers compute
			// max_tokens = ctx_window - input_tokens server-side and then reject
			// when it goes negative ("max_tokens must be at least 1, got -71"),
			// and "context length exceeded" messages mean the same thing. Tell
			// the user clearly so they can set contextWindow (which drives
			// autocompaction) and /compact to recover the current session.
			if detail := providerDetail(err); detail != "" {
				if isContextOverflow(detail) {
					fix := " Lower the context window compaction is sized against to match this model so it triggers earlier"
					if h.ContextWindow != "" {
						fix = " Set " + h.ContextWindow + " to match this model so autocompaction triggers earlier"
					}
					if h.Compact != "" {
						fix += ", and run " + h.Compact + " to summarise the current session"
					}
					return "Conversation outgrew the model's context window — the provider returned: " + detail + fix + "."
				}
				return fmt.Sprintf("Bad request (400): %s", detail)
			}
			return "Bad request (400) — the model rejected the request shape; check the input."
		case 401, 403:
			// OpenAI-compatible providers send specific messages here ("Incorrect
			// API key provided", "You don't have access to …"); surface them and
			// keep the generic where-to-fix-it hint.
			if detail := providerDetail(err); detail != "" {
				return fmt.Sprintf("Authentication failed (%d): %s. Check your API key / sign-in%s.", status, detail, h.credentialsNote())
			}
			return fmt.Sprintf("Authentication failed (%d): check your API key / sign-in%s.", status, h.credentialsNote())
		case 529:
			if detail := providerDetail(err); detail != "" {
				return fmt.Sprintf("The API is overloaded (529): %s Try again shortly.", detail)
			}
			return "The API is overloaded (529) and retries were exhausted. Try again shortly."
		default:
			if status >= 500 {
				if detail := providerDetail(err); detail != "" {
					return fmt.Sprintf("API server error (%d): %s Try again shortly.", status, detail)
				}
				return fmt.Sprintf("API server error (%d) after retries. Try again shortly.", status)
			}
		}
	}
	// Connection-level failures (wrong/placeholder baseURL, no network, endpoint
	// down) surface as raw dial/DNS errors — point the user at the likely cause.
	var dnsErr *net.DNSError
	var netErr *net.OpError
	if errors.As(err, &dnsErr) || errors.As(err, &netErr) {
		where := "the endpoint URL"
		if h.Endpoint != "" {
			where = h.Endpoint
		}
		return "Could not reach the model endpoint (network error). Check your internet " +
			"connection and " + where + ".\n" +
			"Details: " + err.Error()
	}
	// The stall wraps DeadlineExceeded, so check it first: the connection went
	// idle mid-stream (flaky network/proxy/VPN), which is unrelated to the
	// baseURL the generic timeout branch below points at.
	if errors.Is(err, ErrStreamStalled) {
		// Two shapes, and saying "auto-retried" for both was a lie: a stall
		// after output has been delivered is never retried, because a fresh
		// request would re-emit what you already saw.
		if errors.Is(err, errStallMidStream) {
			return "The model connection went quiet mid-reply (no data before the idle timeout), so the " +
				"partial answer above is all that arrived. It was not retried automatically — that would " +
				"repeat what you already saw." + h.idleNote()
		}
		return "The model connection stalled (no data received before the idle timeout) and was " +
			"auto-retried without success — usually a flaky network, proxy, or VPN dropping the " +
			"streaming connection." + h.idleNote()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "The request to the model endpoint timed out. Check the endpoint is reachable " +
			"and the baseURL is correct.\nDetails: " + err.Error()
	}
	return err.Error()
}

// openAIPayload returns the parsed structured payload from an OpenAI-compatible
// error, if one is wrapped inside err. Returns nil for other providers or when
// the body isn't a recognized error envelope.
func openAIPayload(err error) *OpenAIErrorPayload {
	var oai *OpenAIError
	if errors.As(err, &oai) {
		return oai.Payload()
	}
	return nil
}

// anthropicPayload parses the {"error":{"type","message"}} body the Anthropic
// SDK preserves via RawJSON(). Returns ("", "") when err isn't an
// *anthropic.Error, or when the body doesn't match.
func anthropicPayload(err error) (errType, message string) {
	var anthErr *anthropic.Error
	if !errors.As(err, &anthErr) {
		return "", ""
	}
	t := string(anthErr.Type()) // already extracted by the SDK from the envelope
	raw := anthErr.RawJSON()
	if raw == "" {
		return t, ""
	}
	var env struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(raw), &env) != nil {
		return t, ""
	}
	if env.Error.Type != "" {
		t = env.Error.Type
	}
	return t, env.Error.Message
}

// providerDetail returns the upstream-provider's own message for err (trimmed)
// — OpenAI envelope or Anthropic body — so our error templates can splice in
// what the provider actually said. Empty when no provider payload is parseable
// OR the payload's message is uselessly generic (literal "Error", just the type
// name); the caller should fall back to descriptive text in those cases.
func providerDetail(err error) string {
	if p := openAIPayload(err); p != nil {
		if m := meaningfulMessage(p.Message, p.Type); m != "" {
			return m
		}
	}
	if errType, m := anthropicPayload(err); m != "" || errType != "" {
		if c := meaningfulMessage(m, errType); c != "" {
			return c
		}
	}
	return ""
}

// codeIs reports whether the raw "code" JSON field equals want. Providers
// inconsistently send code as a string ("insufficient_quota") or a bare
// number (400) — RawMessage lets us accept either and compare here.
func codeIs(raw json.RawMessage, want string) bool {
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	return s == want
}

// isSessionLimit recognises the Claude Max / Claude Code "you've hit your
// session limit" 429 — a daily/period quota wait rather than the per-minute
// throttle or the OAuth concurrent-session contention we usually warn about.
// Captured shape from a real `claude` rejection: "You've hit your session limit
// · resets <time>". Matching either substring is enough; both are too specific
// to appear in benign messages.
func isSessionLimit(detail string) bool {
	d := strings.ToLower(detail)
	return strings.Contains(d, "session limit") || strings.Contains(d, "session_limit")
}

// isLongContextCredits recognises the Anthropic 429 returned when a request
// crosses into the long-context (1M-token) tier that requires pay-as-you-go
// usage credits. Unlike the per-minute throttle, retrying never clears it — it's
// a billing/entitlement gate — so FriendlyError must drop the "retry more"
// advice. Captured shape: "Usage credits are required for long context
// requests." Both substrings are too specific to appear in benign messages.
func isLongContextCredits(detail string) bool {
	d := strings.ToLower(detail)
	return strings.Contains(d, "usage credits") && strings.Contains(d, "long context")
}

// isContextOverflow recognises provider error messages that actually mean
// "your conversation outgrew the model's context window", so FriendlyError can
// suggest setting contextWindow + /compact instead of leaving the raw error.
// Two real-world shapes: (a) negative max_tokens after the provider's
// server-side `max_tokens = ctx - input` arithmetic ("max_tokens must be at
// least 1, got -71"); (b) explicit "context length exceeded" / "too many
// tokens" phrases.
func isContextOverflow(detail string) bool {
	d := strings.ToLower(detail)
	switch {
	case strings.Contains(d, "max_tokens") && (strings.Contains(d, "at least 1") || strings.Contains(d, "must be positive")),
		// Anthropic's own wording, which this list did not cover: the 400 reads
		// "prompt is too long: 1000464 tokens > 1000000 maximum". It was
		// reported as a bare "Bad request" with no hint that /compact is the
		// way out.
		strings.Contains(d, "prompt is too long"),
		strings.Contains(d, "prompt too long"),
		strings.Contains(d, "context length"),
		strings.Contains(d, "context window"),
		strings.Contains(d, "maximum context"),
		strings.Contains(d, "too many tokens"):
		return true
	}
	return false
}

// meaningfulMessage trims msg and returns "" when the result is empty, the
// literal word "error" / "unknown", or just the error type repeated — patterns
// providers sometimes emit instead of an actual explanation, which splice into
// our templates as confusing "...: Error (..." gibberish.
func meaningfulMessage(msg, errType string) string {
	t := strings.TrimSpace(msg)
	switch {
	case t == "",
		strings.EqualFold(t, "error"),
		strings.EqualFold(t, "unknown"),
		errType != "" && strings.EqualFold(t, errType):
		return ""
	}
	return t
}

// apiStatus extracts an HTTP status code from a provider error (Anthropic or
// OpenAI-compatible), if present.
func apiStatus(err error) (int, bool) {
	var anthropicErr *anthropic.Error
	if errors.As(err, &anthropicErr) {
		return anthropicErr.StatusCode, true
	}
	var openaiErr *OpenAIError
	if errors.As(err, &openaiErr) {
		return openaiErr.StatusCode, true
	}
	return 0, false
}

// IsUnavailable reports whether err is the provider failing rather than the
// request: overloaded (529) or a server error (5xx), already retried by the
// client. That is when trying another model can help — a 4xx would fail the
// same way on any model, and a 429 is a quota, not a model's capacity.
func IsUnavailable(err error) bool {
	status, ok := apiStatus(err)
	return ok && (status == 529 || status >= 500)
}

// IsContextOverflow reports whether err is the provider refusing a request for
// being larger than the model's context window. The agent loop uses this to
// compact and retry rather than surfacing a dead end: once a session is over
// the limit, every resend fails the same way, so without recovery the
// transcript is unusable.
func IsContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	if detail := providerDetail(err); detail != "" && isContextOverflow(detail) {
		return true
	}
	return isContextOverflow(err.Error())
}

// maxTokensAnthropicRE matches the Anthropic 400 that rejects max_tokens for
// exceeding the model's output cap, capturing the cap. Measured 2026-10 with
// claude-sonnet-4-5-20250929 at max_tokens=128000:
//
//	max_tokens: 128000 > 64000, which is the maximum allowed number of output
//	tokens for claude-sonnet-4-5-20250929
//
// TestLiveMaxTokensTooLargeWording re-measures it, so a reworded 400 is a test
// failure rather than a silent loss of the retry below.
var maxTokensAnthropicRE = regexp.MustCompile(`max_tokens:\s*\d+\s*>\s*(\d+)`)

// maxTokensOpenAIRE matches OpenAI's documented phrasing for the same refusal:
//
//	max_tokens is too large: 128000. This model supports at most 16384
//	completion tokens, whereas you provided 128000.
//
// Unlike the Anthropic pattern this one is NOT measured — we have no
// OpenAI-compatible endpoint under test that caps lower than we ask — so treat
// it as best-effort. It is guarded by requiring the message to mention a
// max-tokens field, because "at most N" on its own appears in unrelated 400s
// (per-request item limits, for one) and a wrong number here would lower the
// cap for the rest of the run.
var maxTokensOpenAIRE = regexp.MustCompile(`at most\s*(\d+)`)

// MaxTokensTooLarge reports the model's real output cap when err is the
// provider refusing the request for asking above it.
//
// This exists because the cap comes from an offline table (modelMaxOutputTokens)
// that goes stale: an entry that is too high makes every turn fail identically,
// which ends the session. The 400 names the real limit, so the agent loop can
// lower max_tokens to it and retake the turn — one wasted round-trip instead of
// a dead end. The reverse staleness (an entry too low, or a model absent from
// the table and floored at DefaultMaxOutputTokens) is not an error at all and
// is handled by ResolveLimits, not here.
func MaxTokensTooLarge(err error) (limit int, ok bool) {
	if err == nil {
		return 0, false
	}
	// Only a 400 means "your request was wrong"; a 500 whose body happens to
	// quote these words should be retried as-is, not with a lowered cap.
	if status, found := apiStatus(err); !found || status != 400 {
		return 0, false
	}
	detail := providerDetail(err)
	if detail == "" {
		detail = err.Error()
	}
	d := strings.ToLower(detail)
	if m := maxTokensAnthropicRE.FindStringSubmatch(d); m != nil {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil && n > 0 {
			return n, true
		}
	}
	if !strings.Contains(d, "max_tokens") && !strings.Contains(d, "max_completion_tokens") {
		return 0, false
	}
	if m := maxTokensOpenAIRE.FindStringSubmatch(d); m != nil {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil && n > 0 {
			return n, true
		}
	}
	return 0, false
}

// IsImmutableThinking reports whether err is the API refusing a request because
// the latest assistant message's thinking blocks were altered:
//
//	messages.1.content.42: `thinking` or `redacted_thinking` blocks in the
//	latest assistant message cannot be modified.
//
// Klaudia alters that message when it repairs an interrupted server-tool call,
// and the alteration cannot be undone — the original blocks are gone. The agent
// loop recovers by abandoning the turn rather than resending it forever.
func IsImmutableThinking(err error) bool {
	if err == nil {
		return false
	}
	d := strings.ToLower(providerDetail(err))
	if d == "" {
		d = strings.ToLower(err.Error())
	}
	return strings.Contains(d, "cannot be modified") &&
		(strings.Contains(d, "thinking") || strings.Contains(d, "redacted_thinking"))
}

func (h Hints) credentialsNote() string {
	if h.Credentials == "" {
		return ""
	}
	return " (or " + h.Credentials + ")"
}
