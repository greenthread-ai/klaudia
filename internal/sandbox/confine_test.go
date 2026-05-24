package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSeatbeltConfinesWrites(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skipf("sandbox-exec not found: %v", err)
	}

	base, err := os.MkdirTemp(".", ".confine-test-")
	if err != nil {
		t.Fatalf("create test dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	base, err = filepath.Abs(base)
	if err != nil {
		t.Fatalf("abs test dir: %v", err)
	}
	wd := filepath.Join(base, "wd")
	forbidden := filepath.Join(base, "forbidden")
	if err := os.Mkdir(wd, 0o755); err != nil {
		t.Fatalf("create working dir: %v", err)
	}
	if err := os.Mkdir(forbidden, 0o755); err != nil {
		t.Fatalf("create forbidden dir: %v", err)
	}
	okPath := filepath.Join(wd, "ok.txt")
	blockedPath := filepath.Join(forbidden, "blocked.txt")

	s := NewSeatbelt(nil, "none")
	resp, err := s.Run(context.Background(), Request{
		Command:    "echo hi > ok.txt",
		WorkingDir: wd,
	})
	if err != nil {
		t.Fatalf("allowed write Run: %v", err)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("allowed write exit = %d, stderr = %q", resp.ExitCode, resp.Stderr)
	}
	if _, err := os.Stat(okPath); err != nil {
		t.Fatalf("allowed write did not create %s: %v", okPath, err)
	}

	resp, err = s.Run(context.Background(), Request{
		Command:    "echo hi > " + shellQuote(blockedPath),
		WorkingDir: wd,
	})
	if err != nil {
		t.Fatalf("denied write Run: %v", err)
	}
	_, statErr := os.Stat(blockedPath)
	if resp.ExitCode == 0 && statErr == nil {
		t.Fatalf("write outside working dir succeeded and created %s", blockedPath)
	}
}

func TestBwrapAvailable(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skipf("bwrap not found: %v", err)
	}
}

func TestSeatbeltProfile(t *testing.T) {
	s := NewSeatbelt([]string{"/extra"}, "none")
	profile := s.profile(Request{WorkingDir: "/work/proj"})

	for _, want := range []string{
		"(version 1)",
		"(allow default)",
		"(deny file-write*)",
		"(allow file-write*\n  (subpath \"/work/proj\")",
		"(subpath \"/extra\")",
		"(literal \"/dev/null\") (literal \"/dev/stdout\") (literal \"/dev/stderr\") (literal \"/dev/zero\")",
		"(regex #\"^/dev/tty\"))",
		"(deny network*)",
	} {
		if !strings.Contains(profile, want) {
			t.Errorf("profile missing %q\n%s", want, profile)
		}
	}
}

func TestBwrapBuildArgs(t *testing.T) {
	b := NewBwrap([]string{"/extra"}, "none")
	args := b.buildArgs(Request{Command: "echo hi", WorkingDir: "/work/proj"})
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"--ro-bind / /",
		"--dev /dev",
		"--proc /proc",
		"--tmpfs /tmp",
		"--bind /work/proj /work/proj",
		"--bind /extra /extra",
		"--unshare-net",
		"-- " + shellPath + " -c echo hi",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q\n%v", want, args)
		}
	}
	if !slices.Contains(args, "/work/proj") {
		t.Errorf("args missing working dir: %v", args)
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
