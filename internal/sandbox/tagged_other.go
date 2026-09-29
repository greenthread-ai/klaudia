//go:build unix && !linux

package sandbox

// terminateTagged is a no-op where processes cannot be found by their
// environment without /proc: a process that left the group is not reached.
func terminateTagged(string) {}
