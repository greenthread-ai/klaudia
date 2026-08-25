package tui

import (
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The README screenshot used to be a photograph of somebody's terminal, and it
// rotted the way photographs do: by the time anyone looked, it was advertising
// /allow and /deny months after they were deprecated, listing 22 commands out of
// 40, and reciting a session line the banner had deliberately stopped printing.
// Nothing in the build had any idea.
//
// So it is generated from the same functions the program runs, and the test that
// generates it also asserts the committed copy is current. Change /help and this
// fails, with the command to fix it. The image cannot drift from the code
// without someone being told.
//
//	go test ./internal/tui -run Screenshot -update-screenshot
var updateScreenshot = flag.Bool("update-screenshot", false, "rewrite docs/screenshot.svg from the current UI")

// screenshotPath is relative to this package's directory.
const screenshotPath = "../../docs/screenshot.svg"

// Pinned so the output is byte-stable: the tagline is normally chosen at random
// and the model comes from config, and neither should make the image churn.
const (
	shotModel   = "claude-opus-5"
	shotBranch  = "main"
	shotTagline = "your overqualified rubber duck"
)

func TestScreenshotIsCurrent(t *testing.T) {
	got := renderScreenshotSVG()

	if *updateScreenshot {
		if err := os.WriteFile(screenshotPath, []byte(got), 0o644); err != nil {
			t.Fatalf("writing screenshot: %v", err)
		}
		abs, _ := filepath.Abs(screenshotPath)
		t.Logf("wrote %s (%d bytes)", abs, len(got))
		return
	}

	want, err := os.ReadFile(screenshotPath)
	if err != nil {
		t.Fatalf("no committed screenshot (%v)\nregenerate: go test ./internal/tui -run Screenshot -update-screenshot", err)
	}
	if string(want) != got {
		t.Errorf("docs/screenshot.svg is stale — the UI changed and the image did not\n"+
			"regenerate: go test ./internal/tui -run Screenshot -update-screenshot\n"+
			"(committed %d bytes, current UI renders %d)", len(want), len(got))
	}
}

// screenshotFrame is what the image shows: the real banner and the real /help,
// produced by the functions the program itself calls. Nothing here is a copy of
// the UI's text, which is the entire point.
func screenshotFrame() string {
	lipgloss.SetColorProfile(termenv.TrueColor)
	applyChromeTheme(defaultChromePalette)
	defer applyChromeTheme(defaultChromePalette)

	var b strings.Builder
	b.WriteString(intro(shotModel, shotBranch, shotTagline))
	b.WriteString("\n")
	b.WriteString(userStyle.Render("› /help"))
	b.WriteString("\n")
	b.WriteString(slashHelp())
	return b.String()
}

// SVG rather than PNG: a tenth the size, diffable in review, crisp on any
// display, and generated without a font file or a windowing system.
const (
	shotFontSize   = 15.0
	shotLineHeight = 20.0
	// Advance width per column. This is not an estimate of any particular font:
	// every line is emitted with textLength, so the renderer is told to make the
	// line occupy exactly its column count at this width, whichever font from
	// the stack it actually has. Guessing the advance instead clipped the
	// longest line off the canvas edge under one renderer and left a ragged
	// margin under another.
	shotCharWidth = 9.0
	shotPadding   = 22.0
)

func renderScreenshotSVG() string {
	frame := screenshotFrame()
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")

	// Spans are computed once, up front, because the canvas has to be sized
	// from the same trimmed text that gets drawn.
	type rendered struct {
		spans []span
		cols  int
	}
	out := make([]rendered, 0, len(lines))
	cols := 0
	for _, line := range lines {
		sp := trimTrailingSpaces(parseSGR(line, defaultChromePalette.fg))
		n := 0
		for _, s := range sp {
			n += len([]rune(s.text))
		}
		if n > cols {
			cols = n
		}
		out = append(out, rendered{sp, n})
	}

	w := float64(cols)*shotCharWidth + 2*shotPadding
	h := float64(len(lines))*shotLineHeight + 2*shotPadding

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f" font-family="ui-monospace,SFMono-Regular,Menlo,Consolas,'DejaVu Sans Mono',monospace" font-size="%.0f">`+"\n",
		w, h, w, h, shotFontSize)
	fmt.Fprintf(&b, `<rect width="100%%" height="100%%" rx="10" fill="%s"/>`+"\n", defaultChromePalette.bg)

	y := shotPadding + shotFontSize
	for _, r := range out {
		if r.cols > 0 {
			// lengthAdjust="spacing" stretches the gaps between glyphs, not the
			// glyphs themselves, so the grid lines up without the letterforms
			// being distorted.
			fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" textLength="%.2f" lengthAdjust="spacing" xml:space="preserve">`,
				shotPadding, y, float64(r.cols)*shotCharWidth)
			for _, s := range r.spans {
				b.WriteString(s.svg())
			}
			b.WriteString("</text>\n")
		}
		y += shotLineHeight
	}
	b.WriteString("</svg>\n")
	return b.String()
}

// trimTrailingSpaces removes the padding lipgloss adds to square off a
// multi-line block.
//
// It is invisible in a terminal, which is why nobody notices it, but it is not
// invisible on a canvas sized from the longest line: the banner carries 114
// trailing spaces, which made the image twelve columns wider than anything
// drawn in it and forced the visible text to be stretched across the excess.
func trimTrailingSpaces(spans []span) []span {
	for len(spans) > 0 {
		last := len(spans) - 1
		if t := strings.TrimRight(spans[last].text, " "); t != "" {
			spans[last].text = t
			break
		}
		spans = spans[:last]
	}
	return spans
}

// span is a run of text sharing one set of attributes.
type span struct {
	text   string
	fg     string
	bold   bool
	faint  bool
	italic bool
}

func (s span) svg() string {
	var attr strings.Builder
	if s.fg != "" {
		fmt.Fprintf(&attr, ` fill="%s"`, s.fg)
	}
	if s.bold {
		attr.WriteString(` font-weight="700"`)
	}
	if s.italic {
		attr.WriteString(` font-style="italic"`)
	}
	// Terminals render "faint" by dimming rather than by switching colour, and
	// the banner and hint lines rely on it heavily. Opacity is the honest
	// equivalent; picking a second, darker palette colour would invent a shade
	// the program never emits.
	if s.faint {
		attr.WriteString(` opacity="0.62"`)
	}
	return "<tspan" + attr.String() + ">" + html.EscapeString(s.text) + "</tspan>"
}

var sgrRe = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// stripSGR removes colour escapes, for measuring printable width.
func stripSGR(s string) string { return sgrRe.ReplaceAllString(s, "") }

// parseSGR splits an ANSI line into styled spans. It handles exactly what
// lipgloss emits under a TrueColor profile — reset, bold, faint, italic, and
// 24-bit foreground — and ignores anything else rather than guessing.
func parseSGR(line, defaultFG string) []span {
	cur := span{fg: defaultFG}
	var out []span
	emit := func(text string) {
		if text == "" {
			return
		}
		s := cur
		s.text = text
		out = append(out, s)
	}

	last := 0
	for _, m := range sgrRe.FindAllStringSubmatchIndex(line, -1) {
		emit(line[last:m[0]])
		last = m[1]
		params := line[m[2]:m[3]]
		if params == "" {
			params = "0"
		}
		fields := strings.Split(params, ";")
		for i := 0; i < len(fields); i++ {
			switch fields[i] {
			case "0", "":
				cur = span{fg: defaultFG}
			case "1":
				cur.bold = true
			case "2":
				cur.faint = true
			case "3":
				cur.italic = true
			case "22":
				cur.bold, cur.faint = false, false
			case "23":
				cur.italic = false
			case "39":
				cur.fg = defaultFG
			case "38":
				// 38;2;R;G;B — true colour. Anything else (256-colour 38;5;n)
				// is not emitted under this profile, so it is skipped rather
				// than approximated.
				if i+4 < len(fields) && fields[i+1] == "2" {
					r, _ := strconv.Atoi(fields[i+2])
					g, _ := strconv.Atoi(fields[i+3])
					bl, _ := strconv.Atoi(fields[i+4])
					cur.fg = fmt.Sprintf("#%02x%02x%02x", r, g, bl)
					i += 4
				}
			}
		}
	}
	emit(line[last:])
	return out
}
