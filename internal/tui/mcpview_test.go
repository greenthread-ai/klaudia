package tui

import (
	"strings"
	"testing"
)

func mcpModel(t *testing.T) *Model {
	t.Helper()
	return &Model{sess: &Session{}, height: 40}
}

func TestMCPReloadNotice(t *testing.T) {
	tests := []struct {
		name     string
		event    MCPReloadEvent
		wantAll  []string
		wantNone []string
		silent   bool
	}{
		{
			// The case this exists for. A reload that cannot parse the config
			// applies nothing, so the edit the user just made is not live —
			// and before this, that was indistinguishable from success.
			name:    "a config that does not parse says so",
			event:   MCPReloadEvent{ConfigErr: "/home/n/.klaudia/.mcp.json: invalid character '}'"},
			wantAll: []string{".mcp.json", "invalid character", "still running"},
		},
		{
			name:    "a server that fails to launch is named",
			event:   MCPReloadEvent{ServerErrs: []string{`mcp "godot" connect: exec: "npx": not found`}},
			wantAll: []string{"1 server(s) failed", "godot", "/mcp"},
		},
		{
			// One broken config is one problem; listing twelve servers buries
			// the pointer to /mcp under noise.
			name: "a long list is truncated with a count",
			event: MCPReloadEvent{ServerErrs: []string{
				"mcp \"a\" connect: boom", "mcp \"b\" connect: boom",
				"mcp \"c\" connect: boom", "mcp \"d\" connect: boom",
				"mcp \"e\" connect: boom",
			}},
			wantAll:  []string{"5 server(s) failed", "and 2 more"},
			wantNone: []string{`"e"`},
		},
		{
			// Every save of an unrelated key would otherwise print a line.
			name:   "a clean reload is silent",
			event:  MCPReloadEvent{},
			silent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mcpModel(t)
			m.onMCPReload(tt.event)
			out := stripANSI(m.transcript.String())

			if tt.silent {
				if strings.TrimSpace(out) != "" {
					t.Fatalf("a successful reload printed:\n%s", out)
				}
				return
			}
			if strings.TrimSpace(out) == "" {
				t.Fatal("a failed reload printed nothing")
			}
			for _, want := range tt.wantAll {
				if !strings.Contains(out, want) {
					t.Errorf("notice is missing %q:\n%s", want, out)
				}
			}
			for _, bad := range tt.wantNone {
				if strings.Contains(out, bad) {
					t.Errorf("notice should not contain %q:\n%s", bad, out)
				}
			}
		})
	}
}
