package api

import "time"

// Tuning is the transport behaviour a host chooses for a provider: how hard to
// retry, how long a quiet stream may stay open, whether to mark cache
// breakpoints. It is passed to the constructor rather than read from the
// environment, so two providers in one process can differ and an embedder
// decides — the KLAUDIA_* variables these replaced were process-wide.
//
// The zero value is the default for every field.
type Tuning struct {
	// MaxRetries for transient failures (connection errors, 429, 5xx). nil uses
	// defaultMaxRetries; 0 disables retrying.
	MaxRetries *int
	// StreamIdleTimeout is how long a streamed turn may go without an event
	// before it is treated as stalled. nil uses defaultStreamIdleTimeout; zero
	// or negative disables the watchdog.
	StreamIdleTimeout *time.Duration
	// NoPromptCache leaves cache_control breakpoints off the request
	// (Anthropic only). For measuring what caching saves, not for everyday use.
	NoPromptCache bool
}

func (t Tuning) maxRetries() int {
	if t.MaxRetries != nil && *t.MaxRetries >= 0 {
		return *t.MaxRetries
	}
	return defaultMaxRetries
}

func (t Tuning) streamIdleTimeout() time.Duration {
	if t.StreamIdleTimeout != nil {
		return *t.StreamIdleTimeout
	}
	return defaultStreamIdleTimeout
}
