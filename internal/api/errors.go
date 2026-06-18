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
