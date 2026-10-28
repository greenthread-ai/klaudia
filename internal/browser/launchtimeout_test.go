package browser

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A Chrome that starts and never answers on its DevTools pipe used to leave
// the launch, and the web tool call behind it, waiting for good. It now fails
// within launchTimeout.
func TestLaunchGivesUpOnASilentChrome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the browser")
	}
	old := launchTimeout
	launchTimeout = time.Second
	t.Cleanup(func() { launchTimeout = old })

	fake := filepath.Join(t.TempDir(), "silent-chrome")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := New(context.Background(), Options{Mode: ModeLaunch, Headless: true, ChromePath: fake, UserDataDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "no answer from Chrome") {
		t.Fatalf("err = %v, want the launch to time out", err)
	}
	if took := time.Since(start); took > 15*time.Second {
		t.Errorf("gave up after %s; want near the 1s limit (a retry with a temporary profile may double it)", took)
	}
}
