package agent

import "strings"

// A turn can end without an answer, and the user has to be told why.
//
// Three stop reasons produce a turn that completed cleanly at the protocol
// level but said nothing useful: the model refused, it ran out of output
// budget, or the conversation no longer fits its context window. All three
// used to render as a bare "done in 3s" — dead air after the user pressed
// Enter, indistinguishable from a bug. Reported from a real session: a refusal
// loop where three messages in a row got a silent completion.
//
// Refusal is the one that bites hardest, because it compounds. The model's
// safety system can be tripped by earlier context rather than the latest
// message, so once a conversation has drifted into refused territory even "hi"
// keeps refusing — and on an auto-resumed session the tainted history comes
// back with it. The note has to point at the way out, not just name the state.

// TurnNote returns a message to show the user when a turn ended in a way that
// needs explaining, or "" when it ended normally. hadText is whether the model
// produced any visible answer this turn — a truncated turn may have partial
// text worth keeping, a refusal does not.
func TurnNote(stopReason string, hadText bool) string {
	switch stopReason {
	case "refusal":
		return "The model declined to respond. This is its safety system, and in a long or " +
			"resumed conversation it can be set off by earlier context rather than your last " +
			"message — so it can keep happening even to an unrelated prompt. If it does, /clear " +
			"starts a fresh conversation (or re-run with --new-session)."
	case "max_tokens":
		if hadText {
			return "The response was cut off at the output limit. Ask it to continue, or to " +
				"write large output in smaller pieces."
		}
		return "The model reached the output limit before saying anything. Ask for a shorter " +
			"answer, or split the work into smaller steps."
	case "model_context_window_exceeded":
		return "The conversation is too large for the model's context window. /compact to " +
			"summarise it, or /clear to start fresh."
	}
	return ""
}

// TurnEndedEmpty reports whether a turn finished with nothing for the user to
// read — the case where a note is not just helpful but the only feedback there
// is. Used by frontends to decide whether the note replaces the ordinary
// completion line or accompanies it.
func TurnEndedEmpty(text string) bool {
	return strings.TrimSpace(text) == ""
}
