package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// FriendlyError turns an API error into a concise, human-readable message.
// Transient/auth failures (already retried for 429/5xx) get an actionable
// explanation instead of a raw POST dump. Handles both the Anthropic and the
// OpenAI-compatible providers. Non-API errors are returned as-is.
func FriendlyError(err error) string {
	if err == nil {
		return ""
	}
	if status, ok := apiStatus(err); ok {
		// Before the status switch: an unknown model arrives as a 404 from
		// most hosts and as a 400 carrying the model_not_found code from
		// some, and either way the raw body ("openai endpoint 404:
		// {…model_not_found…}") says nothing about how to recover. Other 400s
		// that merely mention a model ("invalid model parameter") keep the
		// Bad request message below, which already quotes the provider: they
		// can be a malformed value rather than an unknown one, and "not
		// found" would be a guess.
		if IsModelNotFound(err) && (status == 404 || hasModelNotFoundCode(err)) {
			return modelNotFoundMessage(err)
		}
		switch status {
		case 404:
			// A 404 that does not mention a model is the endpoint not serving
			// the path at all — almost always a baseURL missing or doubling
			// its /v1 suffix.
			out := "Not found (404)"
			if _, endpoint := requestContext(err); endpoint != "" {
				out += " at " + endpoint
			}
			if detail := notFoundDetail(err); detail != "" {
				out += ": " + detail + "."
			} else {
				out += "."
			}
			return out + " Check the baseURL in ~/.klaudia/config.toml or ./.klaudia/config.toml " +
				"(OpenAI-compatible endpoints usually end in /v1)."
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
					return "Rate limited (429): " + strings.TrimSpace(oai.Message) +
						" (Set KLAUDIA_MAX_RETRIES to retry more.)"
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
						return out + ": " + detail + " (Retries won't help — this needs pay-as-you-go usage credits for the long-context tier. Add credits, or reduce context: lower `contextWindow` in .klaudia/config.toml so autocompaction triggers earlier, and run /compact.)"
					}
					return out + ": " + detail + " (If you signed in via Claude Code OAuth this often happens when the token is in use by another session. Set KLAUDIA_MAX_RETRIES to retry more.)"
				}
				return out + ": if you signed in via Claude Code OAuth this often happens when the token is in use by another session — try closing other sessions and waiting a moment. (Set KLAUDIA_MAX_RETRIES to retry more.)"
			}
			// Fallback (no parseable provider payload). The OAuth-token-sharing
			// cause is Anthropic-specific, so still include it when the error
			// came from that provider.
			out := "Rate limited (429): the API is busy and retries were exhausted. "
			var anthErr *anthropic.Error
			if errors.As(err, &anthErr) {
				out += "If you signed in via Claude Code OAuth, this often happens when the token is in use by another session. "
			}
			return out + "Wait a moment and try again. (Set KLAUDIA_MAX_RETRIES to retry more.)"
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
					return "Conversation outgrew the model's context window — the provider returned: " + detail +
						" Set `contextWindow` in .klaudia/config.toml to match this model so autocompaction triggers earlier, and run /compact to summarise the current session."
				}
				return fmt.Sprintf("Bad request (400): %s", detail)
			}
			return "Bad request (400) — the model rejected the request shape; check the input."
		case 401, 403:
			// OpenAI-compatible providers send specific messages here ("Incorrect
			// API key provided", "You don't have access to …"); surface them and
			// keep the generic where-to-fix-it hint.
			if detail := providerDetail(err); detail != "" {
				return fmt.Sprintf("Authentication failed (%d): %s. Check your API key / sign-in (or .klaudia/config.toml for a custom provider).", status, detail)
			}
			return fmt.Sprintf("Authentication failed (%d): check your API key / sign-in "+
				"(or .klaudia/config.toml for a custom provider).", status)
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
		return "Could not reach the model endpoint (network error). Check your internet " +
			"connection and the baseURL in ~/.klaudia/config.toml or ./.klaudia/config.toml.\n" +
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
				"repeat what you already saw. Send your message again; tune the window with " +
				"KLAUDIA_STREAM_IDLE_TIMEOUT (seconds, 0 disables)."
		}
		return "The model connection stalled (no data received before the idle timeout) and was " +
			"auto-retried without success — usually a flaky network, proxy, or VPN dropping the " +
			"streaming connection. Send your message again; tune the window with KLAUDIA_STREAM_IDLE_TIMEOUT " +
			"(seconds, 0 disables)."
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

// IsModelNotFound reports whether err is the provider rejecting the request's
// model as unknown. Anthropic answers a 404 not_found_error with the message
// "model: <id>"; OpenAI-compatible hosts send a 404 (sometimes a 400) with a
// model_not_found code or "The model `<id>` does not exist". A 404 that does
// not mention a model — a wrong baseURL path, say — is not matched: switching
// models would not fix it.
//
// The body is the same as the detection in the fallback-model change (#170),
// which declares this function in fallback.go: whichever of the two lands
// second deletes one copy, and the duplicate declaration fails the build
// until it does.
func IsModelNotFound(err error) bool {
	if err == nil {
		return false
	}
	status, ok := apiStatus(err)
	if !ok || (status != 404 && status != 400) {
		return false
	}
	if oai := openAIPayload(err); oai != nil && codeIs(oai.Code, "model_not_found") {
		return true
	}
	detail := strings.ToLower(providerDetail(err))
	if detail == "" {
		var oai *OpenAIError
		if errors.As(err, &oai) {
			detail = strings.ToLower(oai.Body) // plain-text error bodies
		}
	}
	if !strings.Contains(detail, "model") {
		return false
	}
	if errType, _ := anthropicPayload(err); errType == "not_found_error" {
		return true
	}
	for _, s := range []string{"model_not_found", "not found", "not_found", "does not exist", "invalid model", "unknown model"} {
		if strings.Contains(detail, s) {
			return true
		}
	}
	return false
}

// hasModelNotFoundCode reports whether an OpenAI-compatible error carries the
// explicit model_not_found code, the one unambiguous signal a 400 can give.
func hasModelNotFoundCode(err error) bool {
	oai := openAIPayload(err)
	return oai != nil && codeIs(oai.Code, "model_not_found")
}

// notFoundDetail is providerDetail, falling back to an OpenAI-compatible
// host's plain-text body ("model not found", "404 page not found") when there
// is no JSON envelope to read a message from.
func notFoundDetail(err error) string {
	if d := providerDetail(err); d != "" {
		return d
	}
	var oai *OpenAIError
	if errors.As(err, &oai) {
		return strings.TrimSpace(oai.Body)
	}
	return ""
}

// modelNotFoundMessage names the model and the endpoint that refused it and
// the two ways to choose another: /model in the TUI, --model on the command
// line. The provider's own wording is kept at the end, since some hosts add
// something useful ("…or you do not have access to it").
func modelNotFoundMessage(err error) string {
	model, endpoint := requestContext(err)
	if model == "" {
		model = anthropicNotFoundModel(err)
	}
	out := "Model not found: "
	if model != "" {
		out += model + " isn't served by "
	} else {
		out += "the model isn't served by "
	}
	if endpoint != "" {
		out += endpoint
	} else {
		out += "this endpoint"
	}
	out += ". Run /model to pick from the models it lists, or pass --model <id> (or set `model` in .klaudia/config.toml)."
	if detail := notFoundDetail(err); detail != "" {
		out += " The provider said: " + detail
	}
	return out
}

// anthropicNotFoundModel reads the model id out of Anthropic's 404 body,
// whose message is exactly "model: <id>". Used only when the error was not
// annotated with the request's model.
func anthropicNotFoundModel(err error) string {
	_, msg := anthropicPayload(err)
	if id, ok := strings.CutPrefix(strings.TrimSpace(msg), "model:"); ok {
		return strings.TrimSpace(id)
	}
	return ""
}

// requestError annotates a provider error with the model and endpoint the
// request went to. Neither the Anthropic SDK's error nor OpenAIError carries
// the model, and a "model not found" that does not say which model, or where,
// leaves the user guessing whether the typo is in the id or the baseURL.
// Error() is the inner error's text unchanged, and Unwrap keeps every
// errors.As classification working through it.
type requestError struct {
	model    string
	endpoint string
	err      error
}

func (e *requestError) Error() string { return e.err.Error() }
func (e *requestError) Unwrap() error { return e.err }

// annotateNotFound wraps err with the request's model and endpoint when it is
// a 404 or a model-not-found rejection — the errors whose message needs them.
// Every other error is returned unchanged.
func annotateNotFound(err error, model, endpoint string) error {
	if err == nil {
		return nil
	}
	status, ok := apiStatus(err)
	if !ok || (status != 404 && !IsModelNotFound(err)) {
		return err
	}
	return &requestError{model: model, endpoint: endpoint, err: err}
}

// requestContext returns the model and endpoint annotateNotFound attached to
// err, or empty strings when it was not annotated.
func requestContext(err error) (model, endpoint string) {
	var re *requestError
	if errors.As(err, &re) {
		return re.model, re.endpoint
	}
	return "", ""
}
