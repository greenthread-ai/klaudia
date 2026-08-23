//go:build !unix

package sandbox

import (
	"os/exec"
	"time"
)

// Windows has no process groups in the POSIX sense. exec.CommandContext's
// default cancellation (kill the child) applies, which is what happened
// everywhere before this change — so this build is no worse off than it was,
// and the unix build gets the fix.

const killGrace = 2 * time.Second

func applyProcGroup(*exec.Cmd) {}
