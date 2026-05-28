package api

import (
	"context"
	"errors"
	"fmt"
	"net"

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
			msg := "Rate limited (429): the API is busy and retries were exhausted. "
			// The OAuth-token-in-use cause is Anthropic-specific (Claude Code OAuth
			// sessions can collide); for the OpenAI-compatible provider, 429 is just
			// the provider throttling — don't mention OAuth.
			var anthropicErr *anthropic.Error
			if errors.As(err, &anthropicErr) {
				msg += "If you signed in via Claude Code OAuth, this often happens when the token is in use by another session. "
			}
			return msg + "Wait a moment and try again. (Set KLAUDIA_MAX_RETRIES to retry more.)"
		case 401, 403:
			return fmt.Sprintf("Authentication failed (%d): check your API key / sign-in "+
				"(or .klaudia/config.toml for a custom provider).", status)
		case 529:
			return "The API is overloaded (529) and retries were exhausted. Try again shortly."
		default:
			if status >= 500 {
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
