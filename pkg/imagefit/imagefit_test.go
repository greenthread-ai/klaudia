package imagefit

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func pngOf(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestFit(t *testing.T) {
	grey := color.RGBA{120, 130, 140, 255}
	jpg := func(w, h int) []byte {
		var b bytes.Buffer
		_ = jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)), nil)
		return b.Bytes()
	}
	tests := []struct {
		name     string
		data     []byte
		mt       string
		want     image.Point
		changed  bool
		wantType string
	}{
		// The reported screenshots: 2634×2042 and 2748×1954.
		{name: "a wide screenshot", data: pngOf(t, 2634, 2042, grey), mt: "image/png", want: image.Pt(2000, 1550), changed: true, wantType: "image/png"},
		{name: "another", data: pngOf(t, 2748, 1954, grey), mt: "image/png", want: image.Pt(2000, 1422), changed: true, wantType: "image/png"},
		{name: "a tall one", data: pngOf(t, 900, 4500, grey), mt: "image/png", want: image.Pt(400, 2000), changed: true, wantType: "image/png"},
		{name: "already inside is untouched", data: pngOf(t, 1600, 900, grey), mt: "image/png", want: image.Pt(1600, 900), wantType: "image/png"},
		{name: "exactly the limit is untouched", data: pngOf(t, 2000, 2000, grey), mt: "image/png", want: image.Pt(2000, 2000), wantType: "image/png"},
		{name: "a JPEG stays JPEG", data: jpg(3000, 2000), mt: "image/jpeg", want: image.Pt(2000, 1333), changed: true, wantType: "image/jpeg"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Fit(tc.data, tc.mt, MaxSide)
			if err != nil {
				t.Fatal(err)
			}
			if got.To != tc.want || got.Changed != tc.changed || got.MediaType != tc.wantType {
				t.Errorf("Fit = to %v changed %v type %s; want %v %v %s", got.To, got.Changed, got.MediaType, tc.want, tc.changed, tc.wantType)
			}
			if w, h, ok := Size(got.Data); !ok || image.Pt(w, h) != tc.want {
				t.Errorf("the encoded image is %dx%d (ok=%v), want %v", w, h, ok, tc.want)
			}
			if !tc.changed && !bytes.Equal(got.Data, tc.data) {
				t.Error("an image inside the limit was re-encoded")
			}
		})
	}
}

// Averaging must keep colour, not just geometry: a uniform image stays that
// colour, and a half-transparent one stays half-transparent.
func TestFitKeepsColour(t *testing.T) {
	for _, c := range []color.RGBA{{120, 130, 140, 255}, {0, 0, 0, 128}} {
		got, err := Fit(pngOf(t, 3000, 1000, c), "image/png", MaxSide)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(got.Data))
		if err != nil {
			t.Fatal(err)
		}
		r, g, b, a := img.At(1000, 300).RGBA()
		wr, wg, wb, wa := c.RGBA()
		if r>>8 != wr>>8 || g>>8 != wg>>8 || b>>8 != wb>>8 || a>>8 != wa>>8 {
			t.Errorf("colour %v became %d,%d,%d,%d", c, r>>8, g>>8, b>>8, a>>8)
		}
	}
}

func TestFitRejectsWhatItCannotDecode(t *testing.T) {
	if _, err := Fit([]byte("RIFF....WEBPVP8 not really"), "image/webp", MaxSide); err != ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}
