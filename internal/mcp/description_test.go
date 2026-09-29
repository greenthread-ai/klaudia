package mcp

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestToolDescriptionCappedAndCleaned(t *testing.T) {
	long := strings.Repeat("é", maxToolDescription)
	got := toolDescription(long)
	if len(got) > maxToolDescription || !utf8.ValidString(got) || !strings.HasSuffix(got, "truncated]") {
		t.Errorf("len %d, valid %v; want ≤%d bytes, marked truncated", len(got), utf8.ValidString(got), maxToolDescription)
	}
	if got := toolDescription("Query logs.\u200b\u2066"); got != "Query logs." {
		t.Errorf("toolDescription = %q, want invisible characters removed", got)
	}
}
