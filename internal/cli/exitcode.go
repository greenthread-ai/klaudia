package cli

import "errors"

// Exit codes an automation can branch on.
//
// §20 asks that exit codes be meaningful. "0 or 1" is not: a CI job that fails
// cannot tell "the model could not do it" from "it needed to install a package
// and nobody said it could", and those want completely different responses —
// the first is a bug report, the second is a flag.
//
// The set is deliberately small. Every code here answers a question someone
// would actually branch on; inventing a code per failure mode would produce a
// table nobody reads and a contract nobody can rely on.
const (
	// ExitOK — the run completed.
	ExitOK = 0
	// ExitError — the run failed. The reason is in the result payload.
	ExitError = 1
	// ExitUsage — Klaudia was invoked wrongly: an unknown flag, an invalid
	// permission mode, a config that does not parse. Nothing ran.
	ExitUsage = 2
	// ExitMaxTurns — the turn limit was reached with work still outstanding.
	// Distinct from failure: what was done is done, and re-running with a
	// higher --max-turns is a reasonable response.
	ExitMaxTurns = 3
	// ExitHostChangeBlocked — the task needed a change to this machine and had
	// no way to get agreement. Re-running with --allow-host-changes is the
	// answer, and an automation can decide that for itself.
	ExitHostChangeBlocked = 4
	// ExitInterrupted — SIGINT or SIGTERM. 130 is the shell convention for
	// "terminated by SIGINT" and tools that read exit codes already know it.
	ExitInterrupted = 130
)

// exitError carries an exit code up through cobra. On its own it prints
// nothing — the reason has already been rendered into the result payload — but
// it wraps cleanly, so usageErrorf can attach a message that does get printed.
type exitError struct{ code int }

func (e exitError) Error() string { return "" }

// exitCodeFor maps an error to the code to exit with.
func exitCodeFor(err error) int {
	if err == nil {
		return ExitOK
	}
	var ee exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	if errors.Is(err, errRendered) {
		return ExitError
	}
	return ExitError
}
