//go:build linux

package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func alive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	// A zombie still answers kill(0); it has exited.
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	return err == nil && !strings.Contains(string(stat), ") Z ")
}

// A process that leaves the command's group with setsid escaped the group
// kill and outlived Esc, /stopjob and the session.
func TestCancelKillsSetsidChild(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("no setsid")
	}
	pidfile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		NewLocal().Run(ctx, Request{Command: "setsid sh -c 'echo $$ > " + pidfile + "; exec sleep 300' & sleep 60"})
		close(done)
	}()
	var pid int
	for i := 0; i < 100 && pid == 0; i++ {
		time.Sleep(50 * time.Millisecond)
		if b, err := os.ReadFile(pidfile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
	}
	if pid == 0 {
		t.Fatal("the setsid child never started")
	}
	if pgid, _ := syscall.Getpgid(pid); pgid != pid {
		t.Fatalf("precondition: child should lead its own group (pgid %d, pid %d)", pgid, pid)
	}
	cancel()
	<-done
	deadline := time.Now().Add(killGrace + 2*time.Second)
	for time.Now().Before(deadline) && alive(pid) {
		time.Sleep(100 * time.Millisecond)
	}
	if alive(pid) {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("setsid child %d survived the cancel", pid)
	}
}
