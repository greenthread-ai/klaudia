package cli

import "testing"

// MCP tools and the listed built-ins wait for ToolSearch; the Browser tools
// are offered up front on every provider, the model choosing between them
// and any server-side web tools.
func TestDeferTool(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"mcp__github__search", true},
		{"Hover", true},
		{"BrowserSearch", false},
		{"BrowserNavigate", false},
		{"Read", false},
	} {
		if got := deferTool(tc.name); got != tc.want {
			t.Errorf("deferTool(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
