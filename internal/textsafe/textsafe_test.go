package textsafe

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStripInvisible(t *testing.T) {
	// "ignore" spelled in tag characters, a bidi override, a zero-width space
	// and a BOM, around visible text.
	tags := ""
	for _, r := range "ignore" {
		tags += string(rune(0xE0000 + r))
	}
	in := "\ufeffBuild with make." + tags + " Use \u202egnp\u202c go\u200b test."
	if got, want := StripInvisible(in), "Build with make. Use gnp go test."; got != want {
		t.Errorf("StripInvisible = %q, want %q", got, want)
	}
	// ZWJ (emoji sequences) and ZWNJ (Persian and other scripts) survive.
	for _, keep := range []string{"👨\u200d👩\u200d👧", "می\u200cخواهم"} {
		if got := StripInvisible(keep); got != keep {
			t.Errorf("StripInvisible(%q) = %q, want it unchanged", keep, got)
		}
	}
}

func TestTruncate(t *testing.T) {
	s := strings.Repeat("é", 100) // 200 bytes
	got := Truncate(s, 51, "…")
	if len(got) > 51 || !utf8.ValidString(got) || !strings.HasSuffix(got, "…") {
		t.Errorf("Truncate = %q (len %d), want ≤51 valid bytes ending in the marker", got, len(got))
	}
	if Truncate("short", 51, "…") != "short" {
		t.Error("short text changed")
	}
}
