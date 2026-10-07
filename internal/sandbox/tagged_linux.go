//go:build linux

package sandbox

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// terminateTagged sends SIGTERM to every process of this user whose
// environment carries tag, then SIGKILL after killGrace to any still alive.
// These are the processes a command started that left its process group.
func terminateTagged(tag string) {
	pids := taggedPIDs(tag)
	if len(pids) == 0 {
		return
	}
	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	go func() {
		time.Sleep(killGrace)
		for _, pid := range taggedPIDs(tag) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}()
}

// taggedPIDs scans /proc for processes whose environment has
// KLAUDIA_PROC_TAG=tag. Only this user's processes are readable, which is
// also the set that may be signalled; Klaudia itself never carries a tag.
func taggedPIDs(tag string) []int {
	want := []byte(procTagVar + "=" + tag)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	self := os.Getpid()
	var out []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		env, err := os.ReadFile(filepath.Join("/proc", e.Name(), "environ"))
		if err != nil {
			continue
		}
		for _, kv := range bytes.Split(env, []byte{0}) {
			if bytes.Equal(kv, want) {
				out = append(out, pid)
				break
			}
		}
	}
	return out
}
