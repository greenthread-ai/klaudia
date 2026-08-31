package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripJSONCommentsKeepsStringContents(t *testing.T) {
	// The case a naive strip gets wrong: slashes inside a URL, and an escaped
	// quote before what looks like a comment.
	in := []byte(`{"url": "https://example.com/v1", "note": "say \" // not a comment"}`)
	var got map[string]string
	if err := json.Unmarshal(stripJSONComments(in), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["url"] != "https://example.com/v1" {
		t.Errorf("url mangled: %q", got["url"])
	}
	if got["note"] != `say " // not a comment` {
		t.Errorf("note mangled: %q", got["note"])
	}
}

func TestStripJSONCommentsPreservesLineNumbers(t *testing.T) {
	in := []byte("{\n // comment\n \"a\": nope\n}")
	err := json.Unmarshal(stripJSONComments(in), &struct{ A int }{})
	if err == nil {
		t.Fatal("want a parse error")
	}
	// The offset must still point into the original text, or the message sends
	// the reader to the wrong line.
	if len(stripJSONComments(in)) != len(in) {
		t.Fatalf("length changed: %d != %d", len(stripJSONComments(in)), len(in))
	}
	if strings.Count(string(stripJSONComments(in)), "\n") != strings.Count(string(in), "\n") {
		t.Fatal("newline count changed")
	}
}

func TestLoadConfigAcceptsComments(t *testing.T) {
	dir := t.TempDir()
	// Exactly the shape klaudia's README documents.
	cfg := `{ "mcpServers": {
  // the local one
  "local":  { "command": "my-server", "args": ["--stdio"] },
  /* the remote one */
  "remote": { "type": "http", "url": "https://mcp.example.com/v1" }
} }`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(got.MCPServers) != 2 {
		t.Fatalf("got %d servers, want 2: %+v", len(got.MCPServers), got.MCPServers)
	}
	if got.MCPServers["remote"].URL != "https://mcp.example.com/v1" {
		t.Errorf("url mangled: %q", got.MCPServers["remote"].URL)
	}
}

func TestLoadConfigReportsMalformedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers": {`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Silence here is what made a broken config look like no config: the
	// session gets no tools and nothing says why.
	if _, err := LoadConfig(dir); err == nil {
		t.Fatal("want an error for a malformed .mcp.json")
	}
}
