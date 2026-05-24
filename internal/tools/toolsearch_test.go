package tools

import (
	"context"
	"encoding/json"
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
