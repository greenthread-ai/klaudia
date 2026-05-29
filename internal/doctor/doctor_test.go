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

func TestRunLSPDetection(t *testing.T) {
	input := Input{
		LSPServers: []LSPServer{
			{Name: "gopls", Language: "go", Version: "v0.15.2"},
			{Name: "pyright", Language: "python", Version: "v1.1.773"},
		},
		MissingLSPHints: []string{"install gopls for Go support"},
	}

	checks := Run(input)

	// Check for individual LSP entries
	if c, ok := find(checks, "lsp:go"); !ok || c.Detail != "gopls (v0.15.2)" {
		t.Errorf("lsp:go = %+v (expected gopls (v0.15.2))", c)
	}
	if c, ok := find(checks, "lsp:python"); !ok || c.Detail != "pyright (v1.1.773)" {
		t.Errorf("lsp:python = %+v (expected pyright (v1.1.773))", c)
	}

	// Check for missing LSP hints
	if c, ok := find(checks, "lsp"); !ok || c.Status != StatusWarn || !strings.Contains(c.Detail, "install gopls") {
		t.Errorf("lsp hint = %+v (expected warn with hint)", c)
	}
}

func TestRunNoLSPs(t *testing.T) {
	checks := Run(Input{})
	if c, ok := find(checks, "lsp"); !ok || c.Status != StatusInfo {
		t.Errorf("lsp without servers should be info, got %+v", c)
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

func TestRunContextWindowLine(t *testing.T) {
	// Known limit: rendered with K/M suffix and the source label so users can
	// tell whether their cfg.contextWindow override is in effect.
	t.Run("known limit and source render", func(t *testing.T) {
		got, ok := find(Run(Input{ContextWindow: 200_000, ContextSource: "model default"}), "context")
		if !ok || !strings.Contains(got.Detail, "200K") || !strings.Contains(got.Detail, "model default") {
			t.Errorf("context check = %+v ok=%v", got, ok)
		}
		if got.Status != StatusInfo {
			t.Errorf("known limit should be informational, got %q", got.Status)
		}
	})
	t.Run("override formatted", func(t *testing.T) {
		got, ok := find(Run(Input{ContextWindow: 8192, ContextSource: "config override"}), "context")
		if !ok || !strings.Contains(got.Detail, "8.2K") || !strings.Contains(got.Detail, "config override") {
			t.Errorf("override check = %+v ok=%v", got, ok)
		}
	})
	t.Run("unknown limit surfaces warning", func(t *testing.T) {
		got, ok := find(Run(Input{ContextWindow: 0, ContextSource: "unknown — using compaction fallback"}), "context")
		if !ok || got.Status != StatusWarn || !strings.Contains(got.Detail, "fallback") {
			t.Errorf("unknown limit = %+v ok=%v", got, ok)
		}
	})
	t.Run("absent when neither set", func(t *testing.T) {
		if _, ok := find(Run(Input{}), "context"); ok {
			t.Error("no context check expected when ContextWindow=0 and ContextSource=\"\"")
		}
	})
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{200_000, "200K"},
		{1_000_000, "1M"},
		{1_500_000, "1.5M"},
		{8192, "8.2K"},
		{900, "900"},
	}
	for _, tc := range tests {
		if got := formatTokens(tc.in); got != tc.want {
			t.Errorf("formatTokens(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// exerrNotFound is a sentinel "binary not found" error for the lookPath stub.
var exerrNotFound = &exErr{}

type exErr struct{}

func (*exErr) Error() string { return "not found" }
