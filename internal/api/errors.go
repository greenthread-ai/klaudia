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
				if oai.Code == "insufficient_quota" || oai.Type == "insufficient_quota" {
					return "Insufficient quota: " + strings.TrimSpace(oai.Message) +
						" (Retries won't help — top up your plan/billing or switch to a different key.)"
				}
				if oai.Message != "" {
					return "Rate limited (429): " + strings.TrimSpace(oai.Message) +
						" (Set KLAUDIA_MAX_RETRIES to retry more.)"
				}
			}
			// Anthropic: pass through whatever the API actually said
			// ("rate_limit_error", "5-hour usage limit reached", concurrent-OAuth
			// hints, etc.) and only mention the OAuth-token-sharing cause as one
			// possibility when the upstream doesn't already explain it.
			if errType, msg := anthropicPayload(err); errType != "" || msg != "" {
				out := "Rate limited (429)"
				if errType != "" {
					out += " [" + errType + "]"
				}
				out += ": "
				if msg != "" {
					out += strings.TrimSpace(msg) + " "
				} else {
					out += "the API is busy and retries were exhausted. "
				}
				out += "(If you signed in via Claude Code OAuth this often happens when the token is in use by another session. Set KLAUDIA_MAX_RETRIES to retry more.)"
				return out
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
// what the provider actually said. Empty when no provider payload is parseable.
func providerDetail(err error) string {
	if p := openAIPayload(err); p != nil {
		if m := strings.TrimSpace(p.Message); m != "" {
			return m
		}
	}
	if _, m := anthropicPayload(err); strings.TrimSpace(m) != "" {
		return strings.TrimSpace(m)
	}
	return ""
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
