package doctor

import (
	"encoding/json"
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

func TestCriticalOnlyForMissingCredential(t *testing.T) {
	// No credential → critical (klaudia can't reach a model at all).
	if !Critical(Run(Input{AuthOK: false})) {
		t.Error("missing credential should be critical")
	}
	// A credential resolves → not critical, even with other warnings present
	// (e.g. an OS-sandbox binary missing on this platform, or no LSPs).
	checks := Run(Input{AuthOK: true, AuthKind: "api-key", SandboxMode: "os"})
	if Critical(checks) {
		t.Errorf("a resolved credential should not be critical: %+v", checks)
	}
}

func TestNewReportOKMirrorsCritical(t *testing.T) {
	if r := NewReport(Run(Input{AuthOK: false})); r.OK {
		t.Error("report OK should be false when a critical check failed")
	}
	if r := NewReport(Run(Input{AuthOK: true, AuthKind: "oauth"})); !r.OK {
		t.Error("report OK should be true when no critical check failed")
	}
}

func TestReportJSONMarshaling(t *testing.T) {
	report := NewReport([]Check{
		{Name: "auth", Status: StatusOK, Detail: "credential resolved (oauth)"},
		{Name: "lsp", Status: StatusWarn, Detail: "install gopls"},
	})
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Round-trip and check the lowercased json keys are present and stable.
	var got struct {
		Checks []map[string]string `json:"checks"`
		OK     bool                `json:"ok"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK {
		t.Errorf("ok = false, want true; json: %s", data)
	}
	if len(got.Checks) != 2 {
		t.Fatalf("checks = %d, want 2; json: %s", len(got.Checks), data)
	}
	first := got.Checks[0]
	if first["name"] != "auth" || first["status"] != StatusOK || !strings.Contains(first["detail"], "oauth") {
		t.Errorf("first check = %v", first)
	}
	// Field names must be the lowercased json tags, not the Go field names.
	for _, k := range []string{"name", "status", "detail"} {
		if _, ok := first[k]; !ok {
			t.Errorf("json check missing key %q: %s", k, data)
		}
	}
}

// exerrNotFound is a sentinel "binary not found" error for the lookPath stub.
var exerrNotFound = &exErr{}

type exErr struct{}

func (*exErr) Error() string { return "not found" }

func TestSkillsCheckDistinguishesEmptyFromBroken(t *testing.T) {
	// The whole point of this check: with no skills the Skill tool is never
	// registered, so the model answers "I have no such ability" — which reads
	// as a broken feature. /doctor is the only place that can say otherwise.
	got, ok := find(Run(Input{}), "skills")
	if !ok {
		t.Fatal("no skills check in report")
	}
	if got.Status != StatusInfo {
		t.Errorf("empty: status = %q, want %q", got.Status, StatusInfo)
	}
	if !strings.Contains(got.Detail, ".klaudia/skills") {
		t.Errorf("empty: detail should name the directories, got %q", got.Detail)
	}

	got, _ = find(Run(Input{Skills: []Skill{
		{Name: "policy", Scope: "project"},
		{Name: "review", Scope: "user"},
	}}), "skills")
	if got.Status != StatusOK {
		t.Errorf("loaded: status = %q, want %q", got.Status, StatusOK)
	}
	for _, want := range []string{"2 loaded", "policy (project)", "review (user)"} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("loaded: detail %q missing %q", got.Detail, want)
		}
	}
}
