package api

import (
	"context"
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
			// OpenAI returns 429 for transient throttling AND for "insufficient_quota"
			// (a billing problem). The two need very different advice — and when the
			// provider gives us a useful message in the body, prefer it verbatim
			// over our guess.
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
			msg := "Rate limited (429): the API is busy and retries were exhausted. "
			// The OAuth-token-in-use cause is Anthropic-specific (Claude Code OAuth
			// sessions can collide); generic OpenAI 429s without a parseable body
			// fall through here too.
			var anthropicErr *anthropic.Error
			if errors.As(err, &anthropicErr) {
				msg += "If you signed in via Claude Code OAuth, this often happens when the token is in use by another session. "
			}
			return msg + "Wait a moment and try again. (Set KLAUDIA_MAX_RETRIES to retry more.)"
		case 401, 403:
			// OpenAI-compatible providers send specific messages here ("Incorrect
			// API key provided", "You don't have access to …"); surface them and
			// keep the generic where-to-fix-it hint.
			if detail := openAIDetail(err); detail != "" {
				return fmt.Sprintf("Authentication failed (%d): %s. Check your API key / sign-in (or .klaudia/config.toml for a custom provider).", status, detail)
			}
			return fmt.Sprintf("Authentication failed (%d): check your API key / sign-in "+
				"(or .klaudia/config.toml for a custom provider).", status)
		case 529:
			if detail := openAIDetail(err); detail != "" {
				return fmt.Sprintf("The API is overloaded (529): %s Try again shortly.", detail)
			}
			return "The API is overloaded (529) and retries were exhausted. Try again shortly."
		default:
			if status >= 500 {
				if detail := openAIDetail(err); detail != "" {
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

// openAIDetail returns the provider's own message for err (trimmed) if one is
// available, else "". Used to splice provider wording into our status-specific
// templates so the user sees what the upstream actually said.
func openAIDetail(err error) string {
	if p := openAIPayload(err); p != nil {
		return strings.TrimSpace(p.Message)
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
