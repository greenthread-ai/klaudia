package tools

import "testing"

// The exact string from the session that prompted this, taken from the
// transcript after one correct round of JSON decoding.
const doubleEscaped = `On \"ask whether they want to change the permission model at that point\" \u2014 which did you mean?`

func TestRepairsDoubleEscapedText(t *testing.T) {
	got := unescapeDisplayText(doubleEscaped)
	want := `On "ask whether they want to change the permission model at that point" — which did you mean?`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// The half that matters more: text that is already fine must come back
// byte-identical. A repair that mangles ordinary strings is worse than the bug.
func TestLeavesOrdinaryTextAlone(t *testing.T) {
	for _, s := range []string{
		"",
		"Which approach do you want?",
		`Use "autonomous" or "plan"?`, // real quotes, no escaping
		"Rename the field to user_id?",
		"Keep the em dash — like this?",
		"100% sure?",
		"Path: /usr/local/bin",
	} {
		if got := unescapeDisplayText(s); got != s {
			t.Errorf("unescapeDisplayText(%q) = %q; it should have been untouched", s, got)
		}
	}
}

// A lone backslash is not an escape sequence, and guessing would corrupt it.
func TestLeavesUnparseableBackslashesAlone(t *testing.T) {
	for _, s := range []string{
		`Use the \d regex?`,  // \d is not a valid Go escape
		`C:\Users\nick`,      // \U and \n — \U wants 8 hex digits
		`Escape it with a \`, // trailing backslash
		`Match \p{L} or \w?`, //
	} {
		if got := unescapeDisplayText(s); got != s {
			t.Errorf("unescapeDisplayText(%q) = %q; an unparseable backslash must be left alone", s, got)
		}
	}
}

// Multi-line text is a block — a plan, a diff, a code sample — where a
// backslash is far more likely to be real. Those are never touched.
func TestLeavesBlocksAlone(t *testing.T) {
	block := "Step one\nprintf(\"hi\\n\");\nStep two"
	if got := unescapeDisplayText(block); got != block {
		t.Errorf("a multi-line block was rewritten:\n%q", got)
	}
}

// Something very long is content, not a label, whatever it contains.
func TestLeavesLongTextAlone(t *testing.T) {
	long := make([]byte, maxRepairable+1)
	for i := range long {
		long[i] = 'a'
	}
	s := `\"` + string(long)
	if got := unescapeDisplayText(s); got != s {
		t.Error("text past the length bound was rewritten")
	}
}
