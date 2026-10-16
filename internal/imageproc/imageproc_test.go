package imageproc_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"medsend-api/internal/imageproc"
)

func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 120, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func size(t *testing.T, b []byte) (int, int) {
	t.Helper()
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if format != "jpeg" {
		t.Fatalf("output format = %s, want jpeg", format)
	}
	return cfg.Width, cfg.Height
}

func TestLargePhotoIsShrunkAndThumbnailed(t *testing.T) {
	res, err := imageproc.Process(bytes.NewReader(encodePNG(t, 4000, 3000)))
	if err != nil {
		t.Fatal(err)
	}
	if w, h := size(t, res.Photo); w != 1600 || h != 1200 {
		t.Errorf("photo = %dx%d, want 1600x1200", w, h)
	}
	if w, h := size(t, res.Thumb); w != 400 || h != 400 {
		t.Errorf("thumb = %dx%d, want 400x400", w, h)
	}
}

func TestSmallPhotoKeepsItsSize(t *testing.T) {
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 800, 600)), nil)
	res, err := imageproc.Process(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := size(t, res.Photo); w != 800 || h != 600 {
		t.Errorf("photo = %dx%d, want unchanged 800x600", w, h)
	}
}

func TestNotAnImageIsRejected(t *testing.T) {
	if _, err := imageproc.Process(bytes.NewReader([]byte("bukan gambar"))); err == nil {
		t.Fatal("expected an error for non-image input")
	}
}
