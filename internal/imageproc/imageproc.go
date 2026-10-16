// Package imageproc normalizes uploaded photos before they are stored:
// upright (EXIF orientation applied), at most MaxSide px, re-encoded as JPEG —
// which also strips EXIF metadata (GPS, phone model) — plus a square thumbnail.
package imageproc

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"io"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp" // register the WEBP decoder for image.Decode
)

const (
	MaxSide      = 1600 // longest side of the stored photo
	ThumbSide    = 400  // square thumbnail for grids
	jpegQuality  = 80
	thumbQuality = 75
	// Refuse images that would take a lot of memory to decode (decompression bombs).
	maxPixels = 50_000_000
)

var ErrTooLarge = errors.New("resolusi foto terlalu besar")

// Result holds the encoded JPEGs.
type Result struct {
	Photo []byte
	Thumb []byte
}

// Process decodes r (JPEG/PNG/WEBP), and returns the normalized photo and its thumbnail.
func Process(r io.Reader) (*Result, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width*cfg.Height > maxPixels {
		return nil, ErrTooLarge
	}

	img, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return nil, err
	}

	photo := img
	if b := img.Bounds(); b.Dx() > MaxSide || b.Dy() > MaxSide {
		photo = imaging.Fit(img, MaxSide, MaxSide, imaging.Lanczos)
	}
	thumb := imaging.Fill(img, ThumbSide, ThumbSide, imaging.Center, imaging.Lanczos)

	var photoBuf, thumbBuf bytes.Buffer
	if err := jpeg.Encode(&photoBuf, photo, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}
	if err := jpeg.Encode(&thumbBuf, thumb, &jpeg.Options{Quality: thumbQuality}); err != nil {
		return nil, err
	}
	return &Result{Photo: photoBuf.Bytes(), Thumb: thumbBuf.Bytes()}, nil
}
