package api

import (
	"errors"
	"fmt"

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
			return "Rate limited (429): the API is busy and retries were exhausted. " +
				"This often happens when your Claude Code OAuth token is in use by another " +
				"session — wait a moment and try again. (Set KLAUDIA_MAX_RETRIES to retry more.)"
		case 401, 403:
			return fmt.Sprintf("Authentication failed (%d): check your API key / sign-in "+
				"(or .klaudia/config.json for a custom provider).", status)
		case 529:
			return "The API is overloaded (529) and retries were exhausted. Try again shortly."
		default:
			if status >= 500 {
				return fmt.Sprintf("API server error (%d) after retries. Try again shortly.", status)
			}
		}
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
