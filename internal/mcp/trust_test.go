package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// projectWithServers writes a global server and a project one, and returns the
// project dir and its loaded config.
func projectWithServers(t *testing.T, projectJSON string) (string, Config) {
	t.Helper()
	root := isolateConfigRoot(t)
	os.WriteFile(filepath.Join(root, ".mcp.json"),
		[]byte(`{"mcpServers":{"mine":{"command":"my-mcp"}}}`), 0o644)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(projectJSON), 0o644)
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, cfg
}

const repoServers = `{"mcpServers":{"repo":{"command":"./run-me","args":["--stdio"],"env":{"TOKEN":"s3cret"}}}}`

func asking(answer bool) (Confirm, *[]string) {
	var shown []string
	return func(_ context.Context, lines, files []string) bool {
		shown = append(shown, lines...)
		shown = append(shown, files...)
		return answer
	}, &shown
}

// A clone used to launch whatever its .mcp.json said on start. Project
// servers are now held until approved; the global file's are the user's own.
func TestProjectServersWaitForApproval(t *testing.T) {
	dir, cfg := projectWithServers(t, repoServers)
	tr := &ProjectTrust{Dir: dir, Path: filepath.Join(t.TempDir(), "a.json")}

	run, held := tr.Admit(cfg)
	if _, ok := run.MCPServers["repo"]; ok || len(held) != 1 || held[0] != "repo" {
		t.Fatalf("project server not held: run=%v held=%v", run.MCPServers, held)
	}
	if _, ok := run.MCPServers["mine"]; !ok {
		t.Fatal("the global server was held; it is the user's own")
	}

	confirm, shown := asking(true)
	ok, notice := tr.Decide(context.Background(), cfg, confirm)
	if !ok || notice != "" {
		t.Fatalf("Decide = %v, %q; want admitted quietly", ok, notice)
	}
	text := strings.Join(*shown, "\n")
	for _, want := range []string{"repo: ./run-me --stdio", "env: TOKEN", filepath.Join(dir, ".mcp.json")} {
		if !strings.Contains(text, want) {
			t.Errorf("the prompt does not show %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "s3cret") {
		t.Error("the prompt shows an env value; names only, values are often tokens")
	}
	if _, held := tr.Admit(cfg); len(held) != 0 {
		t.Errorf("still held after approval: %v", held)
	}

	// Remembered: a later session admits the same set without asking.
	next := &ProjectTrust{Dir: dir, Path: tr.Path}
	if _, held := next.Admit(cfg); len(held) != 0 {
		t.Errorf("approval did not survive the session: %v", held)
	}
	never, _ := asking(false)
	if ok, _ := next.Decide(context.Background(), cfg, never); ok {
		t.Error("an approved set was asked about again")
	}
}

// Editing an approved server is a new question: the dangerous repository is
// the one that asks for something harmless and changes it in a later pull.
func TestEditingAProjectServerAsksAgain(t *testing.T) {
	dir, cfg := projectWithServers(t, repoServers)
	tr := &ProjectTrust{Dir: dir, Path: filepath.Join(t.TempDir(), "a.json")}
	yes, _ := asking(true)
	tr.Decide(context.Background(), cfg, yes)

	os.WriteFile(filepath.Join(dir, ".mcp.json"),
		[]byte(`{"mcpServers":{"repo":{"command":"./something-else"}}}`), 0o644)
	edited, _ := LoadConfig(dir)
	if _, held := tr.Admit(edited); len(held) != 1 {
		t.Fatalf("an edited server started without asking: held=%v", held)
	}
	confirm, shown := asking(true)
	if ok, _ := tr.Decide(context.Background(), edited, confirm); !ok || len(*shown) == 0 {
		t.Error("the edited set was not put to the user")
	}
}

// A no lasts the session: the user is not asked on every turn.
func TestRefusalIsAskedOncePerSession(t *testing.T) {
	dir, cfg := projectWithServers(t, repoServers)
	tr := &ProjectTrust{Dir: dir, Path: filepath.Join(t.TempDir(), "a.json")}
	no, shown := asking(false)
	ok, notice := tr.Decide(context.Background(), cfg, no)
	if ok || !strings.Contains(notice, "will not start this session") {
		t.Fatalf("Decide = %v, %q", ok, notice)
	}
	asked := len(*shown)
	if ok, notice := tr.Decide(context.Background(), cfg, no); ok || notice != "" || len(*shown) != asked {
		t.Error("a refused set was asked about again in the same session")
	}
	if _, held := tr.Admit(cfg); len(held) != 1 {
		t.Error("a refused server was admitted")
	}
}

// Headless: nobody to ask, so nothing starts, and the notice says why.
func TestNoConfirmMeansProjectServersDoNotStart(t *testing.T) {
	dir, cfg := projectWithServers(t, repoServers)
	tr := &ProjectTrust{Dir: dir, Path: filepath.Join(t.TempDir(), "a.json")}
	ok, notice := tr.Decide(context.Background(), cfg, nil)
	if ok || !strings.Contains(notice, "nobody is here to approve") {
		t.Errorf("Decide = %v, %q", ok, notice)
	}
}

// A project file that redefines a global server makes it the project's: that
// is the config that would run.
func TestProjectOverrideOfAGlobalServerIsHeld(t *testing.T) {
	dir, cfg := projectWithServers(t, `{"mcpServers":{"mine":{"command":"evil"}}}`)
	tr := &ProjectTrust{Dir: dir, Path: filepath.Join(t.TempDir(), "a.json")}
	if _, held := tr.Admit(cfg); len(held) != 1 || held[0] != "mine" {
		t.Errorf("held = %v, want the overridden global server", held)
	}
}

// No project servers, nothing to ask.
func TestGlobalOnlyNeedsNoApproval(t *testing.T) {
	dir, cfg := projectWithServers(t, `{"mcpServers":{}}`)
	tr := &ProjectTrust{Dir: dir, Path: filepath.Join(t.TempDir(), "a.json")}
	if ok, notice := tr.Decide(context.Background(), cfg, nil); ok || notice != "" {
		t.Errorf("Decide = %v, %q; want nothing to decide", ok, notice)
	}
}
