package tui

import (
	"fmt"
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
	if got := fmt.Sprint(logoStyle.GetForeground()); !strings.Contains(got, "88c0d0") {
		t.Errorf("logo foreground = %q, want the themed accent #88c0d0", got)
	}
	if got := fmt.Sprint(suggestStyle.GetForeground()); !strings.Contains(got, "81a1c1") {
		t.Errorf("suggest foreground = %q, want accent2 #81a1c1", got)
	}

	// Errors stay red (ANSI 9) regardless of theme (clarity).
	if got := fmt.Sprint(errStyle.GetForeground()); got != "9" {
		t.Errorf("errStyle foreground = %q, want 9 (red)", got)
	}
}
