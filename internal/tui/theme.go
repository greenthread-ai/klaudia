package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

type renderTheme struct {
	id       string
	name     string
	desc     string
	aliases  []string
	standard string
	palette  themePalette
}

type themePalette struct {
	fg      string
	muted   string
	accent  string
	accent2 string
	accent3 string
	bg      string
	codeBG  string
}

var renderThemes = []renderTheme{
	{
		id:       "dracula",
		name:     "Dracula",
		desc:     "dark purples with high contrast",
		aliases:  []string{"drac"},
		standard: "dracula",
		palette:  themePalette{fg: "#f8f8f2", muted: "#6272a4", accent: "#bd93f9", accent2: "#ff79c6", accent3: "#50fa7b", bg: "#282a36", codeBG: "#44475a"},
	},
	{
		id:      "gruvbox",
		name:    "Gruvbox",
		desc:    "warm retro groove colours",
		aliases: []string{"gruv"},
		palette: themePalette{fg: "#ebdbb2", muted: "#928374", accent: "#fabd2f", accent2: "#fe8019", accent3: "#b8bb26", bg: "#282828", codeBG: "#3c3836"},
	},
	{
		id:       "tokyo-night",
		name:     "Tokyo Night",
		desc:     "deep blues, cyans, and purples",
		aliases:  []string{"tokyo", "tokyonight", "tokyo night"},
		standard: "tokyo-night",
		palette:  themePalette{fg: "#c0caf5", muted: "#565f89", accent: "#7aa2f7", accent2: "#bb9af7", accent3: "#9ece6a", bg: "#1a1b26", codeBG: "#24283b"},
	},
	{
		id:      "nord",
		name:    "Nord",
		desc:    "clean arctic blues",
		palette: themePalette{fg: "#d8dee9", muted: "#4c566a", accent: "#88c0d0", accent2: "#81a1c1", accent3: "#a3be8c", bg: "#2e3440", codeBG: "#3b4252"},
	},
	{
		id:      "catppuccin",
		name:    "Catppuccin Mocha",
		desc:    "soft pastel dark theme",
		aliases: []string{"cat", "catppuccin-mocha", "mocha"},
		palette: themePalette{fg: "#cdd6f4", muted: "#6c7086", accent: "#cba6f7", accent2: "#89b4fa", accent3: "#a6e3a1", bg: "#1e1e2e", codeBG: "#313244"},
	},
}

// defaultChromePalette colours the TUI chrome when no theme is selected
// (currentThemeID == "dark") or a theme lacks a palette.
var defaultChromePalette = themePalette{
	fg: "#d8dee9", muted: "#6c7086", accent: "#88c0d0", accent2: "#81a1c1", accent3: "#a3be8c", bg: "#2e3440", codeBG: "#3b4252",
}

// chromePaletteFor returns the palette used to colour the TUI chrome (banner,
// pickers, prompts, type-ahead) for a theme id, falling back to the default.
func chromePaletteFor(id string) themePalette {
	if theme, ok := lookupTheme(id); ok && theme.palette.accent != "" {
		return theme.palette
	}
	return defaultChromePalette
}

func lookupTheme(s string) (renderTheme, bool) {
	want := normalizeThemeName(s)
	for _, theme := range renderThemes {
		if normalizeThemeName(theme.id) == want || normalizeThemeName(theme.name) == want {
			return theme, true
		}
		for _, alias := range theme.aliases {
			if normalizeThemeName(alias) == want {
				return theme, true
			}
		}
	}
	return renderTheme{}, false
}

func normalizeThemeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

// IsTheme reports whether name resolves to a known theme (id/name/alias).
func IsTheme(name string) bool {
	_, ok := lookupTheme(name)
	return ok
}

// ThemeNames returns the available theme ids, comma-separated.
func ThemeNames() string { return themeNames() }

func themeNames() string {
	names := make([]string, 0, len(renderThemes))
	for _, theme := range renderThemes {
		names = append(names, theme.id)
	}
	return strings.Join(names, ", ")
}

func (m *Model) currentThemeID() string {
	if m.sess != nil && m.sess.Theme != "" {
		return m.sess.Theme
	}
	return "dark"
}

func (m *Model) setTheme(id string) {
	if m.sess != nil {
		m.sess.Theme = id
	}
	applyChromeTheme(chromePaletteFor(id)) // recolour banner/pickers/prompts too
	m.glam = nil
	if m.ready {
		m.buildGlamour(m.width)
		m.rerenderTranscript()
		m.syncViewport()
	}
}

func (m *Model) themeChoices() []choiceItem {
	cur := m.currentThemeID()
	items := make([]choiceItem, 0, len(renderThemes))
	for _, theme := range renderThemes {
		theme := theme
		label := fmt.Sprintf("%s — %s", theme.name, theme.desc)
		if theme.id == cur {
			label += "  (current)"
		}
		items = append(items, choiceItem{
			label: label,
			apply: func() string {
				m.setTheme(theme.id)
				return "Theme: " + theme.name
			},
		})
	}
	return items
}

func (m *Model) glamourThemeOption() glamour.TermRendererOption {
	if theme, ok := lookupTheme(m.currentThemeID()); ok {
		if theme.standard != "" {
			return glamour.WithStandardStyle(theme.standard)
		}
		return glamour.WithStyles(themeStyle(theme.palette))
	}
	return glamour.WithStandardStyle("dark")
}

func themeStyle(p themePalette) ansi.StyleConfig {
	style := styles.DarkStyleConfig
	if style.CodeBlock.Chroma != nil {
		chroma := *style.CodeBlock.Chroma
		style.CodeBlock.Chroma = &chroma
	}
	style.Document.StylePrimitive.Color = strPtr(p.fg)
	style.BlockQuote.StylePrimitive.Color = strPtr(p.accent2)
	style.BlockQuote.StylePrimitive.Italic = boolPtr(true)
	style.List.StyleBlock.StylePrimitive.Color = strPtr(p.fg)
	style.Heading.StylePrimitive.Color = strPtr(p.accent)
	style.H1.StylePrimitive.Color = strPtr(p.bg)
	style.H1.StylePrimitive.BackgroundColor = strPtr(p.accent)
	style.H6.StylePrimitive.Color = strPtr(p.accent2)
	style.HorizontalRule.Color = strPtr(p.muted)
	style.Enumeration.Color = strPtr(p.accent2)
	style.Link.Color = strPtr(p.accent2)
	style.LinkText.Color = strPtr(p.accent)
	style.Image.Color = strPtr(p.accent2)
	style.ImageText.Color = strPtr(p.accent)
	style.Code.StylePrimitive.Color = strPtr(p.accent3)
	style.Code.StylePrimitive.BackgroundColor = strPtr(p.codeBG)
	style.CodeBlock.StyleBlock.StylePrimitive.Color = strPtr(p.fg)
	style.CodeBlock.StyleBlock.StylePrimitive.BackgroundColor = strPtr(p.bg)
	if style.CodeBlock.Chroma != nil {
		style.CodeBlock.Chroma.Text.Color = strPtr(p.fg)
		style.CodeBlock.Chroma.Error.Color = strPtr(p.fg)
		style.CodeBlock.Chroma.Error.BackgroundColor = strPtr("#bf616a")
		style.CodeBlock.Chroma.Comment.Color = strPtr(p.muted)
		style.CodeBlock.Chroma.CommentPreproc.Color = strPtr(p.accent2)
		style.CodeBlock.Chroma.Keyword.Color = strPtr(p.accent)
		style.CodeBlock.Chroma.KeywordReserved.Color = strPtr(p.accent)
		style.CodeBlock.Chroma.KeywordNamespace.Color = strPtr(p.accent)
		style.CodeBlock.Chroma.KeywordType.Color = strPtr(p.accent2)
		style.CodeBlock.Chroma.Operator.Color = strPtr(p.accent)
		style.CodeBlock.Chroma.Punctuation.Color = strPtr(p.fg)
		style.CodeBlock.Chroma.Name.Color = strPtr(p.fg)
		style.CodeBlock.Chroma.NameBuiltin.Color = strPtr(p.accent2)
		style.CodeBlock.Chroma.NameTag.Color = strPtr(p.accent)
		style.CodeBlock.Chroma.NameAttribute.Color = strPtr(p.accent3)
		style.CodeBlock.Chroma.NameClass.Color = strPtr(p.accent2)
		style.CodeBlock.Chroma.NameDecorator.Color = strPtr(p.accent3)
		style.CodeBlock.Chroma.NameFunction.Color = strPtr(p.accent3)
		style.CodeBlock.Chroma.LiteralNumber.Color = strPtr(p.accent2)
		style.CodeBlock.Chroma.LiteralString.Color = strPtr(p.accent3)
		style.CodeBlock.Chroma.LiteralStringEscape.Color = strPtr(p.accent)
		style.CodeBlock.Chroma.GenericDeleted.Color = strPtr("#bf616a")
		style.CodeBlock.Chroma.GenericInserted.Color = strPtr(p.accent3)
		style.CodeBlock.Chroma.GenericSubheading.Color = strPtr(p.accent)
		style.CodeBlock.Chroma.Background.BackgroundColor = strPtr(p.bg)
	}
	style.DefinitionDescription.BlockPrefix = "\n🠶 "
	return style
}

func strPtr(s string) *string { return &s }

func boolPtr(b bool) *bool { return &b }
