package cli

import "testing"

// The Browser tools are deferred only where server-side web tools stand in
// for them; on another provider they are the only web access. (PR #94.)
func TestBrowserToolsDeferredOnlyOnAnthropic(t *testing.T) {
	for _, tc := range []struct {
		name         string
		deferBrowser bool
		want         bool
	}{
		{"BrowserSearch", true, true},
		{"BrowserFetch", true, true},
		{"BrowserSearch", false, false},
		{"mcp__github__search", false, true},
		{"Hover", false, true},
		{"Read", true, false},
	} {
		if got := deferTool(tc.name, tc.deferBrowser); got != tc.want {
			t.Errorf("deferTool(%q, %v) = %v, want %v", tc.name, tc.deferBrowser, got, tc.want)
		}
	}
}
