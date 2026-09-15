package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func newToolSearch(t *testing.T) *ToolSearch {
	t.Helper()
	ts, err := NewToolSearch([]ToolInfo{
		{Name: "Grep", Description: "search file contents"},
		{Name: "Write", Description: "write a file"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestToolSearchExecute(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		want       string
		wantReveal []string
	}{
		{
			name:       "matches all terms and reveals",
			query:      "search contents",
			want:       "Loaded 1 tool(s):\n- Grep: search file contents",
			wantReveal: []string{"Grep"},
		},
		{
			name:  "no match",
			query: "nonexistent xyz",
			want:  "No tools matched nonexistent xyz.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newToolSearch(t)
			raw, _ := json.Marshal(ToolSearchInput{Query: tt.query})
			var captured []string

			res, err := ts.Execute(context.Background(), Context{
				Reveal: func(names ...string) {
					captured = append(captured, names...)
				},
			}, raw)
			if err != nil {
				t.Fatal(err)
			}
			if len(res) != 1 || res[0].Content != tt.want {
				t.Fatalf("Execute() = %+v, want content %q", res, tt.want)
			}
			if strings.Join(captured, ",") != strings.Join(tt.wantReveal, ",") {
				t.Fatalf("Reveal captured %v, want %v", captured, tt.wantReveal)
			}
		})
	}
}

func TestToolSearchValidateInput(t *testing.T) {
	ts := newToolSearch(t)
	raw, _ := json.Marshal(ToolSearchInput{Query: "   "})
	if ts.ValidateInput(raw) == nil {
		t.Fatal("expected empty query to be rejected")
	}
}

// godotCatalog is a trimmed copy of the real MCP catalog that exposed the AND
// bug: descriptions are long and overlapping, and no single tool contains every
// word of a natural description of what you want.
func godotCatalog() []ToolInfo {
	return []ToolInfo{
		{Name: "mcp__gsol__godot_game_time", Description: "Freeze the running game, then step forward a bounded slice of game time, or step_until a condition holds."},
		{Name: "mcp__gsol__godot_runtime_state", Description: "Observe live game state as structured data. Use digest for a one-shot entity snapshot."},
		{Name: "mcp__gsol__godot_scene", Description: "Manage scenes in the editor: open a scene, save the open scene, or reload an open scene from disk."},
		{Name: "mcp__gsol__godot_docs", Description: "Fetch Godot Engine documentation. Returns clean markdown."},
		{Name: "Grep", Description: "search file contents"},
	}
}

func TestToolSearchRanksInsteadOfRequiringEveryTerm(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantTop  string   // the first tool revealed
		wantSome []string // tools that must appear somewhere in the results
		wantNone []string
	}{
		{
			// The exact query that returned nothing while the bare word
			// "godot" returned all 65 tools, these two among them. Which of
			// them ranks first is not the point and is genuinely arguable —
			// the query names concepts from both — so assert only that the
			// relevant tools arrive and the irrelevant one does not.
			name:     "descriptive multi-term query finds the relevant tools",
			query:    "godot game time freeze runtime state digest",
			wantSome: []string{"mcp__gsol__godot_game_time", "mcp__gsol__godot_runtime_state"},
			wantNone: []string{"Grep"},
		},
		{
			// A query aimed at one tool must put that tool first, rather than
			// merely including it somewhere in the list.
			name:    "a focused query ranks its tool first",
			query:   "freeze step game time",
			wantTop: "mcp__gsol__godot_game_time",
		},
		{
			name:    "a term no tool has does not veto the rest",
			query:   "godot freeze unicorn",
			wantTop: "mcp__gsol__godot_game_time",
		},
		{
			name:    "fuzzy name match survives a missing separator",
			query:   "gametime",
			wantTop: "mcp__gsol__godot_game_time",
		},
		{
			name:    "description-only term still finds its tool",
			query:   "markdown",
			wantTop: "mcp__gsol__godot_docs",
		},
		{
			name:     "an unrelated query matches nothing",
			query:    "kubernetes helm chart",
			wantNone: []string{"Grep", "mcp__gsol__godot_docs"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, err := NewToolSearch(godotCatalog())
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(ToolSearchInput{Query: tt.query})
			var revealed []string
			if _, err := ts.Execute(context.Background(), Context{
				Reveal: func(names ...string) { revealed = append(revealed, names...) },
			}, raw); err != nil {
				t.Fatal(err)
			}

			if tt.wantTop != "" {
				if len(revealed) == 0 {
					t.Fatalf("query %q revealed nothing, want %s first", tt.query, tt.wantTop)
				}
				if revealed[0] != tt.wantTop {
					t.Errorf("query %q ranked %s first, want %s (full order: %v)", tt.query, revealed[0], tt.wantTop, revealed)
				}
			}
			for _, want := range tt.wantSome {
				if !slices.Contains(revealed, want) {
					t.Errorf("query %q did not reveal %s; got %v", tt.query, want, revealed)
				}
			}
			for _, bad := range tt.wantNone {
				if slices.Contains(revealed, bad) {
					t.Errorf("query %q wrongly revealed %s; got %v", tt.query, bad, revealed)
				}
			}
		})
	}
}

// A broad query must not silently load an entire MCP server into the context,
// which is the cost deferred tools exist to avoid.
func TestToolSearchCapsAndReportsTruncation(t *testing.T) {
	catalog := make([]ToolInfo, 0, maxToolSearchResults*2)
	for i := 0; i < maxToolSearchResults*2; i++ {
		catalog = append(catalog, ToolInfo{
			Name:        fmt.Sprintf("mcp__godot__tool_%02d", i),
			Description: "a godot tool",
		})
	}
	ts, err := NewToolSearch(catalog)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(ToolSearchInput{Query: "godot"})
	var revealed []string
	res, err := ts.Execute(context.Background(), Context{
		Reveal: func(names ...string) { revealed = append(revealed, names...) },
	}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(revealed) != maxToolSearchResults {
		t.Errorf("revealed %d tools, want the cap of %d", len(revealed), maxToolSearchResults)
	}
	if !strings.Contains(res[0].Content, fmt.Sprintf("of %d matching", len(catalog))) {
		t.Errorf("truncation was not reported to the model: %q", res[0].Content)
	}
}
