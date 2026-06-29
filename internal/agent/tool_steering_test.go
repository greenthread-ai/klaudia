package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

func TestUnknownToolMsgListsAvailableTools(t *testing.T) {
	// Real-world case: the model invented a "Find" tool reaching for Glob/Grep.
	// Listing the actual tool names lets it pick the right one without us
	// maintaining a per-typo fuzzy-match table.
	read, _ := tools.NewRead()
	glob, _ := tools.NewGlob()
	grep, _ := tools.NewGrep()
	l := New(nil, tools.NewRegistry(read, glob, grep))

	msg := l.unknownToolMsg("Find")
	for _, want := range []string{"No such tool available: Find", "Available tools:", "Glob", "Grep", "Read"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in %q", want, msg)
		}
	}
	// And tools should be alphabetised for stable scanning.
	got := strings.Index(msg, "Glob")
	if got < 0 || got > strings.Index(msg, "Grep") || strings.Index(msg, "Grep") > strings.Index(msg, "Read") {
		t.Errorf("tools not alphabetised: %q", msg)
	}
}

func TestSchemaFieldListNamesRequiredAndOptional(t *testing.T) {
	// Mirrors how the validation-error path renders Read's schema. Read takes
	// file_path (required), offset (optional), limit (optional).
	read, _ := tools.NewRead()
	got := schemaFieldList(read.InputSchema())
	for _, want := range []string{"file_path (required)", "limit", "offset"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	// A schema with no properties returns "" (don't dangle "accepts: ." onto
	// errors for parameterless tools).
	if got := schemaFieldList(json.RawMessage(`{"type":"object","properties":{}}`)); got != "" {
		t.Errorf("empty-properties schema = %q, want \"\"", got)
	}
}
