// klaudia screenshot — Terminal text to PNG renderer
//
// Pure Go replacement for resvg WASM as used by Klaudia.
// Renders styled monospace text directly to PNG, bypassing SVG.
//
// Usage:
//   echo '{"lines":[[{"text":"hello","color":{"r":229,"g":229,"b":229}}]],"options":{}}' | klaudia-screenshot output.png
//
// Input (JSON on stdin):
//   {
//     "lines": [[{"text":"hello","color":{"r":229,"g":229,"b":229},"bold":false}]],
//     "options": {"fontSize":14,"lineHeight":22,"paddingX":24,"paddingY":24,
//                 "backgroundColor":{"r":30,"g":30,"b":30},"borderRadius":8,"scale":4}
//   }
//
// Exit codes: 0 = success, 1 = error
package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"runtime"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type ColorRGB struct {
	R uint8 `json:"r"`
	G uint8 `json:"g"`
	B uint8 `json:"b"`
}

type TextSpan struct {
	Text  string   `json:"text"`
	Color ColorRGB `json:"color"`
	Bold  bool     `json:"bold"`
}

type RenderOptions struct {
	FontSize        float64  `json:"fontSize"`
	LineHeight      float64  `json:"lineHeight"`
	PaddingX        float64  `json:"paddingX"`
	PaddingY        float64  `json:"paddingY"`
	BackgroundColor ColorRGB `json:"backgroundColor"`
	BorderRadius    float64  `json:"borderRadius"`
	Scale           float64  `json:"scale"`
}

type RenderInput struct {
	Lines   [][]TextSpan  `json:"lines"`
	Options RenderOptions `json:"options"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: klaudia-screenshot <output.png>")
		os.Exit(1)
	}

	outputPath := os.Args[1]

	var input RenderInput
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing input: %v\n", err)
		os.Exit(1)
	}

	// Apply defaults
	opts := input.Options
	if opts.FontSize == 0 {
		opts.FontSize = 14
	}
	if opts.LineHeight == 0 {
		opts.LineHeight = 22
	}
	if opts.PaddingX == 0 {
		opts.PaddingX = 24
	}
	if opts.PaddingY == 0 {
		opts.PaddingY = 24
	}
	if opts.Scale == 0 {
		opts.Scale = 4
	}

	if err := render(input.Lines, opts, outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error rendering: %v\n", err)
		os.Exit(1)
	}
}

func render(lines [][]TextSpan, opts RenderOptions, outputPath string) error {
	scale := opts.Scale

	// Calculate dimensions (matching the JS $0q function)
	charWidth := opts.FontSize * 0.6
	maxLineLen := 0
	for _, line := range lines {
		lineLen := 0
		for _, span := range line {
			lineLen += len(span.Text)
		}
		if lineLen > maxLineLen {
			maxLineLen = lineLen
		}
	}

	width := int(math.Ceil((float64(maxLineLen)*charWidth + opts.PaddingX*2) * scale))
	height := int(math.Ceil((float64(len(lines))*opts.LineHeight + opts.PaddingY*2) * scale))

	if width <= 0 || height <= 0 {
		width = int(100 * scale)
		height = int(50 * scale)
	}

	// Create image
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Fill background
	bg := color.RGBA{R: opts.BackgroundColor.R, G: opts.BackgroundColor.G, B: opts.BackgroundColor.B, A: 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	// Draw rounded corners (simple approach: draw rects to clip corners)
	if opts.BorderRadius > 0 {
		drawRoundedRect(img, width, height, int(opts.BorderRadius*scale), bg)
	}

	// Load font
	face, err := loadMonoFont(opts.FontSize * scale)
	if err != nil {
		// If font loading fails, render what we can without text
		fmt.Fprintf(os.Stderr, "Warning: font loading failed: %v\n", err)
		return writePNG(img, outputPath)
	}
	defer face.Close()

	// Draw text
	for lineIdx, line := range lines {
		y := opts.PaddingY*scale + float64(lineIdx+1)*opts.LineHeight*scale - (opts.LineHeight-opts.FontSize)/2*scale
		x := opts.PaddingX * scale

		for _, span := range line {
			if span.Text == "" {
				continue
			}

			col := color.RGBA{R: span.Color.R, G: span.Color.G, B: span.Color.B, A: 255}

			d := &font.Drawer{
				Dst:  img,
				Src:  image.NewUniform(col),
				Face: face,
				Dot:  fixed.Point26_6{X: fixed.I(int(x)), Y: fixed.I(int(y))},
			}

			d.DrawString(span.Text)
			// Advance x position using character width (monospace)
			x += float64(len(span.Text)) * charWidth * scale
		}
	}

	return writePNG(img, outputPath)
}

func drawRoundedRect(img *image.RGBA, width, height, radius int, bg color.RGBA) {
	// For corners outside the rounded rect, set to transparent
	transparent := color.RGBA{0, 0, 0, 0}

	for y := 0; y < radius; y++ {
		for x := 0; x < radius; x++ {
			// Top-left corner
			dx := radius - x
			dy := radius - y
			if dx*dx+dy*dy > radius*radius {
				img.Set(x, y, transparent)
			}
		}
		for x := width - radius; x < width; x++ {
			// Top-right corner
			dx := x - (width - radius)
			dy := radius - y
			if dx*dx+dy*dy > radius*radius {
				img.Set(x, y, transparent)
			}
		}
	}
	for y := height - radius; y < height; y++ {
		for x := 0; x < radius; x++ {
			// Bottom-left corner
			dx := radius - x
			dy := y - (height - radius)
			if dx*dx+dy*dy > radius*radius {
				img.Set(x, y, transparent)
			}
		}
		for x := width - radius; x < width; x++ {
			// Bottom-right corner
			dx := x - (width - radius)
			dy := y - (height - radius)
			if dx*dx+dy*dy > radius*radius {
				img.Set(x, y, transparent)
			}
		}
	}
}

func loadMonoFont(size float64) (font.Face, error) {
	// Try system monospace fonts
	var fontPaths []string

	switch runtime.GOOS {
	case "darwin":
		fontPaths = []string{
			"/System/Library/Fonts/Menlo.ttc",
			"/System/Library/Fonts/SFMono-Regular.otf",
			"/System/Library/Fonts/Monaco.dfont",
			"/Library/Fonts/Courier New.ttf",
		}
	case "linux":
		fontPaths = []string{
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
			"/usr/share/fonts/TTF/DejaVuSansMono.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
			"/usr/share/fonts/truetype/ubuntu/UbuntuMono-R.ttf",
		}
	default:
		fontPaths = []string{
			"C:\\Windows\\Fonts\\consola.ttf",
			"C:\\Windows\\Fonts\\cour.ttf",
		}
	}

	for _, fp := range fontPaths {
		face, err := loadFontFile(fp, size)
		if err == nil {
			return face, nil
		}
	}

	return nil, fmt.Errorf("no suitable monospace font found")
}

func loadFontFile(path string, size float64) (font.Face, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Try as TTC (TrueType Collection) first
	collection, err := opentype.ParseCollection(data)
	if err == nil {
		f, err := collection.Font(0)
		if err != nil {
			return nil, err
		}
		return opentype.NewFace(f, &opentype.FaceOptions{
			Size:    size,
			DPI:     72,
			Hinting: font.HintingFull,
		})
	}

	// Try as single font
	f, err := opentype.Parse(data)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}

func writePNG(img *image.RGBA, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return png.Encode(f, img)
}
