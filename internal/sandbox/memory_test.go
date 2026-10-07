package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestParseMemory(t *testing.T) {
	for in, want := range map[string]int64{
		"1048576": 1 << 20,
		"512B":    512,
		"64k":     64 << 10,
		"512M":    512 << 20,
		"512m":    512 << 20,
		"2G":      2 << 30,
		"2GB":     2 << 30,
		"1536MiB": 1536 << 20,
		" 1T ":    1 << 40,
	} {
		got, err := ParseMemory(in)
		if err != nil || got != want {
			t.Errorf("ParseMemory(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "B", "G", "0", "-1G", "2 GB", "1.5G", "2X", "max", "99999999999T"} {
		if got, err := ParseMemory(in); err == nil {
			t.Errorf("ParseMemory(%q) = %d, want an error", in, got)
		}
	}
}

func TestMemoryScopeArgvWrapsInner(t *testing.T) {
	m := NewMemoryScope(NewBwrap(nil, "none"), 2<<30)
	name, args := m.Argv(Request{Command: "make test", WorkingDir: "/w"})
	if name != "systemd-run" {
		t.Fatalf("program = %q, want systemd-run", name)
	}
	sep := slices.Index(args, "--")
	if sep < 0 || sep+1 >= len(args) || args[sep+1] != "bwrap" {
		t.Fatalf("inner command must follow the first --, got %q", args)
	}
	opts := strings.Join(args[:sep], " ")
	for _, want := range []string{"--user", "--scope", "-p MemoryMax=2147483648", "-p MemorySwapMax=0"} {
		if !strings.Contains(opts, want) {
			t.Errorf("systemd-run options %q missing %q", opts, want)
		}
	}
	if got := args[len(args)-1]; got != "make test" {
		t.Errorf("last arg = %q, want the command", got)
	}
}

// fakeSystemdRun puts a systemd-run on PATH that records its arguments and
// then runs what follows "--", or prints output and exits with code when the
// command after "--" should not run (to imitate a probe's reply).
func fakeSystemdRun(t *testing.T, body string) (argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake")
	}
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "systemd-run"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsFile
}

const execAfterSeparator = `while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do shift; done; shift; exec "$@"`

func TestMemoryScopeRunsTheCommandThroughSystemdRun(t *testing.T) {
	argsFile := fakeSystemdRun(t, execAfterSeparator)
	m := NewMemoryScope(NewLocal(), 256<<20)
	resp, err := m.Run(context.Background(), Request{Command: "echo out; echo err >&2; exit 3", Env: []string{"KLAUDIA_T=yes"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Stdout != "out\n" || resp.Stderr != "err\n" || resp.ExitCode != 3 {
		t.Errorf("got stdout %q stderr %q exit %d", resp.Stdout, resp.Stderr, resp.ExitCode)
	}
	recorded, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(recorded), "MemoryMax=268435456\n") {
		t.Errorf("systemd-run was not given the limit; args:\n%s", recorded)
	}

	// The environment and the background path go through the same argv.
	resp, err = m.Run(context.Background(), Request{Command: `printf %s "$KLAUDIA_T"`, Env: []string{"KLAUDIA_T=yes"}})
	if err != nil || resp.Stdout != "yes" {
		t.Errorf("env not passed through: %q, %v", resp.Stdout, err)
	}
}

func TestProbeMemoryScope(t *testing.T) {
	cases := []struct {
		name, body string
		ok         bool
		msg        string
	}{
		{"enforced", `echo 268435456`, true, ""},
		{"page-rounded", `echo 268431360`, true, ""},
		{"not delegated", `echo max`, false, "not enforced"},
		{"no bus", `echo "Failed to connect to user scope bus" >&2; exit 1`, false, "Failed to connect to user scope bus"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeSystemdRun(t, tc.body)
			err := ProbeMemoryScope(context.Background(), 256<<20)
			if (err == nil) != tc.ok {
				t.Fatalf("ok = %v, want %v (err %v)", err == nil, tc.ok, err)
			}
			if err != nil && !strings.Contains(err.Error(), tc.msg) {
				t.Errorf("error %q does not mention %q", err, tc.msg)
			}
		})
	}
}

func TestProbeMemoryScopeWithoutSystemdRun(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := ProbeMemoryScope(context.Background(), 1<<30); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v, want systemd-run not found", err)
	}
}
