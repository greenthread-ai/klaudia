package memory

import "errors"

// ErrEmpty is returned when an Add operation is given whitespace-only text.
// Preserves the original message ("memory text is empty") so the LLM-facing
// tool surface doesn't change shape, while enabling errors.Is checks.
var ErrEmpty = errors.New("memory text is empty")
