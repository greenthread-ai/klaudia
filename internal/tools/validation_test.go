package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// The browser tools drive a real Chrome with a persistent profile, so what
// they will navigate to is checked before a permission prompt is ever shown:
// only absolute http(s) URLs, and only the search engines the scraper knows.
func TestBrowserToolsValidateTheirTarget(t *testing.T) {
	nav, _ := NewBrowserNavigate(nil)
	fetch, _ := NewBrowserFetch(nil)
	search, _ := NewBrowserSearch(nil)
	snap, _ := NewBrowserSnapshot(nil)

	for _, tool := range []Tool{nav, fetch} {
		for _, u := range []string{"https://example.com", "http://localhost:3000/x", "  https://example.com/  "} {
			raw, _ := json.Marshal(map[string]string{"url": u})
			if err := tool.ValidateInput(raw); err != nil {
				t.Errorf("%s(%q): unexpected error %v", tool.Name(), u, err)
			}
		}
		for _, u := range []string{"file:///etc/passwd", "javascript:alert(1)", "example.com", "/relative", "ftp://host/x", "https://"} {
			raw, _ := json.Marshal(map[string]string{"url": u})
			if err := tool.ValidateInput(raw); err == nil || !strings.Contains(err.Error(), "absolute http or https URL") {
				t.Errorf("%s(%q): err = %v, want a rejection", tool.Name(), u, err)
			}
		}
	}

	searches := []struct {
		raw   string
		valid bool
	}{
		{`{"query":"go"}`, true},
		{`{"query":"go","engine":"Google"}`, true},
		{`{"query":"go","engine":"duckduckgo-html"}`, true},
		{`{"query":"go","engine":"bing"}`, false},
		{`{"query":"  "}`, false},
	}
	for _, c := range searches {
		if err := search.ValidateInput(json.RawMessage(c.raw)); (err == nil) != c.valid {
			t.Errorf("BrowserSearch %s: err = %v, want valid=%v", c.raw, err, c.valid)
		}
	}

	if err := snap.ValidateInput(json.RawMessage(`{"include_markdown":true}`)); err != nil {
		t.Errorf("BrowserSnapshot rejected a valid request: %v", err)
	}
}

func TestMemoryEmptyAndFailingOperations(t *testing.T) {
	mt := newMemTool(t)
	cases := []struct {
		name    string
		in      MemoryInput
		wantErr bool
		want    string
	}{
		{"view of an empty store", MemoryInput{Operation: "view"}, false, "Memory is empty."},
		{"project add without a project", MemoryInput{Operation: "add", Scope: "project", Content: "x"}, true, "project cwd is unavailable"},
		{"promote of a missing note", MemoryInput{Operation: "promote", Name: "nope"}, true, "Promote failed:"},
		{"supersede of a missing note", MemoryInput{Operation: "supersede", Name: "nope", Replacement: "other"}, true, "Supersede failed:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := runTool(t, mt, Context{}, c.in)
			if res.IsError != c.wantErr || !strings.Contains(res.Content, c.want) {
				t.Errorf("res = %+v, want IsError=%v containing %q", res, c.wantErr, c.want)
			}
		})
	}
	if mt.Name() != "Memory" {
		t.Errorf("name = %q", mt.Name())
	}
}
