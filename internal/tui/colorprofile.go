package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/colorprofile"
)

// termProfile is the terminal's colour support, detected once at startup
// (it honours NO_COLOR and CLICOLOR).
//
// Lip Gloss v2 styles always render full colour and leave downsampling to
// whoever writes to the terminal. Bubble Tea does it for the live region; it
// does not for tea.Println, which writes its text as is, so scrollback goes
// through downsample. Under v1 the global renderer did both, invisibly.
// charmbracelet/bubbletea#1709 (fix in #1714) covers the Println half.
var termProfile = colorprofile.Detect(os.Stdout, os.Environ())

// noColor reports whether the terminal shows no colour at all, which is
// when Markdown and git switch to their plain styles.
func noColor() bool { return termProfile <= colorprofile.ASCII }

// downsample converts s to what the terminal can show.
func downsample(s string) string {
	if termProfile == colorprofile.TrueColor {
		return s
	}
	var b strings.Builder
	w := &colorprofile.Writer{Forward: &b, Profile: termProfile}
	_, _ = w.WriteString(s)
	return b.String()
}
