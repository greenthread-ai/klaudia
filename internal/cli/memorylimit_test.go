package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/sandbox"
)

func TestLimitMemory(t *testing.T) {
	probeOK := func(context.Context, int64) error { return nil }
	probeFail := func(context.Context, int64) error { return errors.New("no user bus") }
	probeUnused := func(context.Context, int64) error {
		t.Error("probe must not run")
		return nil
	}

	cases := []struct {
		name      string
		exec      sandbox.Executor
		memoryMax string
		goos      string
		probe     func(context.Context, int64) error
		wantName  string
		wantWarn  string
	}{
		{"unset", sandbox.NewLocal(), "", "linux", probeUnused, "local", ""},
		{"invalid", sandbox.NewLocal(), "2 GB", "linux", probeUnused, "local", "memoryMax ignored"},
		{"linux enforced", sandbox.NewLocal(), "2G", "linux", probeOK, "local+memory", ""},
		{"linux os mode", sandbox.NewBwrap(nil, ""), "2G", "linux", probeOK, "bwrap+memory", ""},
		{"linux not enforceable", sandbox.NewLocal(), "2G", "linux", probeFail, "local", "no user bus"},
		{"macOS", sandbox.NewSeatbelt(nil, ""), "2G", "darwin", probeUnused, "sandbox-exec", "not supported on darwin"},
		{"container", sandbox.NewContainer("docker", "alpine", true, false, ""), "1G", "darwin", probeUnused, "docker:alpine", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var warned []string
			got := limitMemory(context.Background(), tc.exec, tc.memoryMax, tc.goos, tc.probe,
				func(m string) { warned = append(warned, m) })
			if got.Name() != tc.wantName {
				t.Errorf("executor = %q, want %q", got.Name(), tc.wantName)
			}
			w := strings.Join(warned, "\n")
			if (tc.wantWarn == "") != (w == "") || !strings.Contains(w, tc.wantWarn) {
				t.Errorf("warnings = %q, want %q", w, tc.wantWarn)
			}
			if c, ok := got.(*sandbox.Container); ok && c.Memory != 1<<30 {
				t.Errorf("container memory = %d, want %d", c.Memory, 1<<30)
			}
		})
	}
}
