package tui

import (
	"charm.land/lipgloss/v2"
	"fmt"
	"image/color"
	"strings"
	"testing"
)

func TestChromePaletteFor(t *testing.T) {
	if got := chromePaletteFor("nord").accent; got != "#88c0d0" {
		t.Errorf("nord accent = %q, want #88c0d0", got)
	}
	// Themes that render Markdown via a standard glamour style still get a chrome palette.
	if chromePaletteFor("dracula").accent == "" {
		t.Error("dracula should have a chrome palette")
	}
	// Unknown id and the default "dark" fall back to the default palette.
	if chromePaletteFor("dark").accent != defaultChromePalette.accent {
		t.Error("dark should use the default chrome palette")
	}
	if chromePaletteFor("nope").accent != defaultChromePalette.accent {
		t.Error("unknown theme should use the default chrome palette")
	}
}

// The default has to be a theme you can name, or the startup colours are the
// one scheme /theme cannot switch back to.
func TestDefaultThemeIsSelectable(t *testing.T) {
	theme, ok := lookupTheme(defaultThemeID)
	if !ok {
		t.Fatalf("default theme %q is not in renderThemes", defaultThemeID)
	}
	if theme.palette != defaultChromePalette {
		t.Errorf("default theme palette %+v != defaultChromePalette %+v", theme.palette, defaultChromePalette)
	}
	if !strings.Contains(themeNames(), defaultThemeID) {
		t.Errorf("themeNames() = %q, missing the default %q", themeNames(), defaultThemeID)
	}
	// A session that has chosen nothing renders in the default, and /theme
	// marks it current rather than showing no selection at all.
	m := &Model{sess: &Session{}}
	if got := m.currentThemeID(); got != defaultThemeID {
		t.Errorf("currentThemeID() with no session theme = %q, want %q", got, defaultThemeID)
	}
	var current int
	for _, item := range m.themeChoices() {
		if strings.Contains(item.label, "(current)") {
			current++
		}
	}
	if current != 1 {
		t.Errorf("theme picker marked %d entries current, want exactly 1", current)
	}
}

func TestApplyChromeThemeRecolours(t *testing.T) {
	defer applyChromeTheme(defaultChromePalette) // don't leak global state to other tests

	applyChromeTheme(chromePaletteFor("nord"))
	nord := fmt.Sprint(logoStyle.GetForeground())
	applyChromeTheme(chromePaletteFor("gruvbox"))
	gruv := fmt.Sprint(logoStyle.GetForeground())
	if nord == gruv {
		t.Errorf("logo accent should differ between themes (got %q both)", nord)
	}

	// The configured accent matches the theme palette (profile-independent).
	applyChromeTheme(themePalette{accent: "#88c0d0", accent2: "#81a1c1", muted: "#4c566a"})
	if got := logoStyle.GetForeground(); !sameColor(got, lipgloss.Color("#88c0d0")) {
		t.Errorf("logo foreground = %v, want the themed accent #88c0d0", got)
	}
	if got := suggestStyle.GetForeground(); !sameColor(got, lipgloss.Color("#81a1c1")) {
		t.Errorf("suggest foreground = %v, want accent2 #81a1c1", got)
	}

	// Errors stay red (ANSI 9) regardless of theme (clarity).
	if got := fmt.Sprint(errStyle.GetForeground()); got != "9" {
		t.Errorf("errStyle foreground = %q, want 9 (red)", got)
	}
}

// sameColor compares colours by value: Lip Gloss v2 colours are image/color
// values, which print as RGBA tuples rather than the hex they were made from.
func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}
