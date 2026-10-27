package tool

// Concurrency safety is opt-in, by implementing ConcurrencySafe.
//
// The agent loop runs a batch of tool calls one at a time by default, and that
// default is the right one: most tools touch the filesystem, a shared browser
// engine, a job table or the user's attention, and a tool that was never
// written with a sibling running beside it is not safe just because it looks
// read-only. Reading a file while another call rewrites it is the mild case;
// two calls racing for the single prompt channel is the one that hangs.
//
// So the marker says more than "does not write". It asserts that the tool:
//
//   - never prompts the user (no Ask, Plan or HostChange use), because two
//     prompts at once have nowhere to go;
//   - holds no process-wide resource that assumes one caller at a time;
//   - produces a result that does not depend on when it ran relative to the
//     other calls in the batch.
//
// Dispatch adds its own condition on top: a call whose permission check would
// ask is run serially whatever this reports, so an approval prompt is never
// raced either.

// ConcurrencySafe is implemented by tools that may execute at the same time as
// other concurrency-safe tools in the same batch. A tool that does not
// implement it, or returns false, runs on its own.
type ConcurrencySafe interface {
	ConcurrencySafe() bool
}

// IsConcurrencySafe reports whether t has opted in. The default is false:
// adding a tool is not meant to require thinking about parallelism, and the
// cost of forgetting should be a slow batch rather than a race.
func IsConcurrencySafe(t Tool) bool {
	cs, ok := t.(ConcurrencySafe)
	return ok && cs.ConcurrencySafe()
}
