package memory

import "errors"

// ErrEmpty is returned when an Add operation is given whitespace-only text.
// Preserves the original message ("memory text is empty") so the LLM-facing
// tool surface doesn't change shape, while enabling errors.Is checks.
var ErrEmpty = errors.New("memory text is empty")

// ErrNotFound is returned by Promote / Supersede when the named detail note
// doesn't exist under memory/. Callers can errors.Is check to differentiate
// "user typo" from "permission / I/O".
var ErrNotFound = errors.New("memory: note not found")

// ErrDisabled is returned by Disabled() Store's write methods to signal
// that the caller is running headless without a writable memory backend.
// Reads on Disabled() return zero values, not this error — the read path
// is intentionally branchless.
var ErrDisabled = errors.New("memory: backend disabled")
