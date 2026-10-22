package tui

import (
	"testing"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// summaryKey is keyed by tool name and names an input field; a stale key or a
// mistyped field silently drops the summary from the tool header. See the
// matching test in internal/acp, which found dead WebSearch/WebFetch entries.
func TestSummaryKeyNamesRealToolsAndFields(t *testing.T) {
	schemas, err := tools.BuiltinSchemas()
	if err != nil {
		t.Fatal(err)
	}
	for tool, field := range summaryKey {
		s, ok := schemas[tool]
		if !ok {
			t.Errorf("summaryKey[%q]: no tool has that name", tool)
			continue
		}
		if !tools.SchemaHasField(s, field) {
			t.Errorf("summaryKey[%q] = %q: %s's schema has no such field", tool, field, tool)
		}
	}
}
