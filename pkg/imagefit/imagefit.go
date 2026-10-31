// Package imagefit keeps images inside the sizes the API will accept.
//
// The limit that bites is not the documented 8000 px per side. Once a single
// request carries more than 20 image or document blocks, a stricter per-image
// limit applies to every image in it — the old ones in the history included.
// So a screenshot that was fine when it was read becomes, twenty images later,
// a 400 on every request for the rest of the session: nothing in the history
// shrinks. Measured: a goshapp session with 21 screenshots of 2634×2042 and
// 2748×1954 was rejected with "exceed max allowed size for many-image
// requests: 3000 pixels" although none of them exceeds 3000, and could not
// continue. Anthropic's docs give 2000 px as the size that is safe on every
// platform, and that is the target here.
//
// Pure Go and stdlib only (PNG, JPEG, GIF). WebP cannot be decoded without a
// dependency, so it is measured and reported but never resized.
package imagefit

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	_ "image/gif" // registers GIF for Decode and DecodeConfig
	"image/jpeg"
	"image/png"
	"math"
)

// MaxSide is the longest side an image is kept to. See the package comment.
const MaxSide = 2000

// ManyImageThreshold is the count of image and document blocks in one request
// above which the stricter per-image limit applies.
const ManyImageThreshold = 20

// ErrUnsupported is returned for an image that cannot be decoded here.
var ErrUnsupported = errors.New("imagefit: cannot decode this image format")

// Size reports an image's dimensions without decoding its pixels.
func Size(data []byte) (w, h int, ok bool) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

// Fitted is the outcome of Fit.
type Fitted struct {
	Data      []byte
	MediaType string
	From, To  image.Point // dimensions before and after
	Changed   bool
}

// Fit returns the image scaled down, keeping its aspect ratio, so neither side
// exceeds maxSide. An image already inside the limit comes back unchanged and
// byte-for-byte identical. JPEG stays JPEG; PNG and GIF become PNG.
func Fit(data []byte, mediaType string, maxSide int) (Fitted, error) {
	w, h, ok := Size(data)
	if !ok {
		return Fitted{}, ErrUnsupported
	}
	out := Fitted{Data: data, MediaType: mediaType, From: image.Pt(w, h), To: image.Pt(w, h)}
	if w <= maxSide && h <= maxSide {
		return out, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Fitted{}, ErrUnsupported
	}
	scale := float64(maxSide) / float64(max(w, h))
	tw := max(1, int(math.Round(float64(w)*scale)))
	th := max(1, int(math.Round(float64(h)*scale)))
	dst := shrink(src, tw, th)

	var buf bytes.Buffer
	if mediaType == "image/jpeg" {
		err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 90})
	} else {
		mediaType = "image/png"
		err = (&png.Encoder{CompressionLevel: png.DefaultCompression}).Encode(&buf, dst)
	}
	if err != nil {
		return Fitted{}, err
	}
	return Fitted{Data: buf.Bytes(), MediaType: mediaType, From: out.From, To: image.Pt(tw, th), Changed: true}, nil
}

// shrink downsamples src to tw×th by averaging the source pixels each
// destination pixel covers — a box filter, which for screenshots keeps text
// legible where nearest-neighbour drops strokes. It works on premultiplied
// RGBA, so averaging stays correct across transparency.
func shrink(src image.Image, tw, th int) *image.RGBA {
	b := src.Bounds()
	rgba, ok := src.(*image.RGBA)
	if !ok || b.Min != (image.Point{}) {
		rgba = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	}
	sw, sh := rgba.Bounds().Dx(), rgba.Bounds().Dy()
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	for dy := 0; dy < th; dy++ {
		y0, y1 := dy*sh/th, max(dy*sh/th+1, (dy+1)*sh/th)
		for dx := 0; dx < tw; dx++ {
			x0, x1 := dx*sw/tw, max(dx*sw/tw+1, (dx+1)*sw/tw)
			var r, g, bl, a, n uint32
			for y := y0; y < y1; y++ {
				row := rgba.Pix[y*rgba.Stride:]
				for x := x0; x < x1; x++ {
					p := row[x*4 : x*4+4]
					r, g, bl, a = r+uint32(p[0]), g+uint32(p[1]), bl+uint32(p[2]), a+uint32(p[3])
					n++
				}
			}
			o := dst.Pix[dy*dst.Stride+dx*4:]
			o[0], o[1], o[2], o[3] = uint8(r/n), uint8(g/n), uint8(bl/n), uint8(a/n)
		}
	}
	return dst
}
