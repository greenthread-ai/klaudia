package api

import (
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

// FriendlyError turns an API error into a concise, human-readable message.
// Transient/auth failures (which the SDK has already retried for 429/5xx) get
// an actionable explanation instead of a raw POST dump. Non-API errors are
// returned as-is.
func FriendlyError(err error) string {
	if err == nil {
		return ""
	}
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 429:
			return "Rate limited (429): the API is busy and retries were exhausted. " +
				"This often happens when your Claude Code OAuth token is in use by another " +
				"session — wait a moment and try again. (Set KLAUDIA_MAX_RETRIES to retry more.)"
		case 401, 403:
			return fmt.Sprintf("Authentication failed (%d): check ANTHROPIC_API_KEY or your "+
				"Claude Code sign-in.", apiErr.StatusCode)
		case 529:
			return "The API is overloaded (529) and retries were exhausted. Try again shortly."
		default:
			if apiErr.StatusCode >= 500 {
				return fmt.Sprintf("API server error (%d) after retries. Try again shortly.", apiErr.StatusCode)
			}
		}
	}
	return err.Error()
}
