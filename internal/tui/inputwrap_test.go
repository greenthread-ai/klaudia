package tui

import "testing"

func TestWrappedRowCount(t *testing.T) {
	tests := []struct {
		name  string
		value string
		width int
		want  int
	}{
		{"empty", "", 10, 1},
		{"short fits", "hello", 10, 1},
		{"two logical lines", "one\ntwo", 10, 2},
		{"word wraps to two rows", "aaa bbb ccc ddd", 10, 2},
		{"line exactly fills width gets soft-wrap row", "1234567890", 10, 2},
		{"long word hard-wraps", "abcdefghijklmnop", 10, 2},
		{"nonpositive width falls back to logical lines", "a\nb\nc", 0, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := wrappedRowCount(tc.value, tc.width); got != tc.want {
				t.Errorf("wrappedRowCount(%q, %d) = %d, want %d", tc.value, tc.width, got, tc.want)
			}
		})
	}
}
