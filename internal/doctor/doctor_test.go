package doctor

import (
	"runtime"
	"strings"
	"testing"
)

func find(checks []Check, name string) (Check, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

func TestRunAuthAndConfig(t *testing.T) {
	checks := Run(Input{AuthOK: true, AuthKind: "oauth", ConfigFound: true, Provider: "openai", Model: "openai/gpt-5.5", MCPServers: 2})

	if c, _ := find(checks, "auth"); c.Status != StatusOK || !strings.Contains(c.Detail, "oauth") {
		t.Errorf("auth = %+v", c)
	}
	if c, _ := find(checks, "config"); c.Status != StatusOK {
		t.Errorf("config = %+v", c)
	}
	if c, _ := find(checks, "provider"); !strings.Contains(c.Detail, "openai") || !strings.Contains(c.Detail, "gpt-5.5") {
		t.Errorf("provider = %+v", c)
	}
	if c, _ := find(checks, "mcp"); c.Status != StatusOK || !strings.Contains(c.Detail, "2") {
		t.Errorf("mcp = %+v", c)
	}
}

func TestRunNoAuthWarns(t *testing.T) {
	checks := Run(Input{AuthOK: false})
	if c, _ := find(checks, "auth"); c.Status != StatusWarn {
		t.Errorf("auth without credential should warn, got %+v", c)
	}
}

func TestSandboxOSMissingBinaryWarns(t *testing.T) {
	orig := lookPath
	defer func() { lookPath = orig }()
	lookPath = func(string) (string, error) { return "", exerrNotFound }

	c := sandboxCheck("os")
	// On darwin/linux the binary is absent → warn; elsewhere → info (unsupported).
	switch runtime.GOOS {
	case "darwin", "linux":
		if c.Status != StatusWarn {
			t.Errorf("expected warn when os-sandbox binary missing, got %+v", c)
		}
	default:
		if c.Status != StatusInfo {
			t.Errorf("expected info on unsupported OS, got %+v", c)
		}
	}
}

func TestFormatIncludesMarks(t *testing.T) {
	out := Format([]Check{{Name: "auth", Status: StatusOK, Detail: "fine"}, {Name: "x", Status: StatusWarn, Detail: "bad"}})
	if !strings.Contains(out, "✓") || !strings.Contains(out, "!") || !strings.Contains(out, "auth") {
		t.Errorf("Format = %q", out)
	}
}

// exerrNotFound is a sentinel "binary not found" error for the lookPath stub.
var exerrNotFound = &exErr{}

type exErr struct{}

func (*exErr) Error() string { return "not found" }
