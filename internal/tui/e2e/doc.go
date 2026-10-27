// Package e2e drives the real klaudia binary in a pseudo-terminal against a
// scripted, OpenAI-compatible mock provider, and asserts on what a terminal
// would show. It needs no API key and touches no network beyond 127.0.0.1.
//
// # Running
//
// The tests skip unless KLAUDIA_E2E=1, so `go test ./internal/...` stays fast
// and hermetic:
//
//	KLAUDIA_E2E=1 go test ./internal/tui/e2e/... -count=1 -v
//
// TestMain builds ./cmd/klaudia once (CGO_ENABLED=0) into a temporary
// directory; nothing on $PATH is used. Only darwin and linux are supported
// (the pty is opened with the stdlib syscall package, no cgo, no dependency).
//
// # Isolation
//
// Each test gets a fresh HOME and KLAUDIA_CONFIG_DIR, a temporary project
// directory, and an environment built from scratch rather than inherited, so
// no real provider key or endpoint can leak in. The mock's address is written
// to both the user and the project config.toml, klaudia is started with
// --new-session, and Env.Prompt fails the test if the mock did not receive a
// request after a prompt — a run that quietly talked to a real provider must
// not pass.
//
// # Pieces
//
//   - mock_test.go: the provider. defaultScript picks a reply by keyword in
//     the last user prompt and by how many assistant steps have followed it,
//     so one prompt can play out a multi-step tool turn.
//   - pty_*_test.go: opening a pty with the stdlib only.
//   - screen_test.go: a minimal VT emulator (cursor movement, erase, scrolling
//     into scrollback, wide runes, OSC 8 links) that also answers the
//     terminal queries it sees, in the order it sees them.
//   - term_test.go: Term, the running binary — Type, Press, WaitFor,
//     Snapshot, WaitExit.
//   - scenarios_test.go: the tests.
//
// # Adding a scenario
//
// Add a keyword to scriptKeywords and a case to defaultScript (mock_test.go)
// returning the Reply for each step (text, tool calls, or both), then write a
// test in scenarios_test.go. The emulator's own tests (emulator_test.go) run
// without KLAUDIA_E2E. A scenario looks like:
//
//	func TestThing(t *testing.T) {
//		env := newEnv(t)
//		tm := env.Start(t)
//		env.Prompt(t, tm, "thing please")
//		snap := tm.WaitFor(t, "the done line", 20*time.Second, func(s Snapshot) bool {
//			return s.HasLine(doneLine)
//		})
//		...
//	}
//
// Wait on screen conditions with WaitFor rather than sleeping; on timeout it
// fails with the screen and scrollback dumped. Assert on behaviour that is
// meant to be stable (a tool ran, a link was emitted, the file changed), not
// on incidental wording.
package e2e
