package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"

	_ "image/gif" // registers the GIF decoder with image.Decode
	_ "image/png" // registers the PNG decoder with image.Decode

	"github.com/deepteams/webp"   // WebP encoder, and the WebP decoder image.Decode finds (pure Go)
	_ "github.com/gen2brain/avif" // registers the AVIF decoder (WebAssembly in a pure-Go runtime, so no cgo)
	_ "github.com/gen2brain/heic" // registers the HEIC decoder (WebAssembly in a pure-Go runtime, so no cgo)
	xdraw "golang.org/x/image/draw"
)

// Limits (ADR-00033).
const (
	// MaxUploadBytes is the largest upload accepted.
	MaxUploadBytes = 5 << 20
	// MaxSide is the largest width and height of a stored image.
	MaxSide = 1024
	// PreviewSide is the largest width and height of the preview the person
	// crops against.
	PreviewSide = 1600
	// maxPixels bounds the memory a decode may take: a small file can claim
	// enormous dimensions.
	maxPixels = 50_000_000
	// webpQuality is the lossy WebP quality of a stored avatar.
	webpQuality = 82
)

// Refusal is an upload the server will not take. Its text is written for the
// person who uploaded it, who can act on it; nothing else from this package is
// safe to show.
type Refusal struct{ Msg string }

func (r *Refusal) Error() string { return r.Msg }

// RefusalText returns the message of a Refusal in err's chain.
func RefusalText(err error) (string, bool) {
	var r *Refusal
	if errors.As(err, &r) {
		return r.Msg, true
	}
	return "", false
}

// formats is what an upload may be, by the name the decoder registered.
var formats = map[string]bool{"jpeg": true, "png": true, "gif": true, "webp": true, "heic": true, "avif": true}

// Decoded is an upload that decoded.
type Decoded struct {
	// Image is the picture the way a person sees it: any EXIF orientation is
	// applied.
	Image image.Image
	// Format is the format found by decoding, not the one the browser claimed.
	Format string
}

// Decode checks an upload and decodes it. The format comes from the bytes
// (ADR-00033). Too big, not a picture, not an accepted format, and a damaged
// file are each a *Refusal.
func Decode(data []byte) (Decoded, error) {
	if len(data) > MaxUploadBytes {
		return Decoded{}, &Refusal{"That file is over 5 MB. Choose a smaller picture."}
	}
	if len(data) == 0 {
		return Decoded{}, &Refusal{"That file is empty. Choose a picture."}
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if errors.Is(err, image.ErrFormat) || (err == nil && !formats[format]) {
		return Decoded{}, &Refusal{"That is not a JPEG, PNG, GIF, WebP, HEIC or AVIF picture. Choose one of those."}
	}
	if err != nil {
		return Decoded{}, &Refusal{"That picture could not be read. It may be damaged; try another."}
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return Decoded{}, &Refusal{"That picture is too large to process. Choose a smaller one."}
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Decoded{}, &Refusal{"That picture could not be read. It may be damaged; try another."}
	}
	if format == "jpeg" {
		img = orient(img, exifOrientation(data))
	}
	return Decoded{Image: img, Format: format}, nil
}

// Crop is the part of the picture to keep, as fractions of its width and height
// measured from the top left. Fractions mean the browser's preview and the
// server's original need not have the same pixel size.
type Crop struct{ X, Y, W, H float64 }

// Full is the whole picture.
var Full = Crop{0, 0, 1, 1}

// Validate refuses a rectangle that is not inside the picture.
func (c Crop) Validate() error {
	const eps = 1e-6
	for _, v := range []float64{c.X, c.Y, c.W, c.H} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return &Refusal{"The crop is not valid. Choose the part of the picture again."}
		}
	}
	if c.X < -eps || c.Y < -eps || c.W <= 0 || c.H <= 0 || c.X+c.W > 1+eps || c.Y+c.H > 1+eps {
		return &Refusal{"The crop is outside the picture. Choose the part of the picture again."}
	}
	return nil
}

func (c Crop) rect(b image.Rectangle) image.Rectangle {
	w, h := float64(b.Dx()), float64(b.Dy())
	x0 := b.Min.X + int(math.Round(math.Max(c.X, 0)*w))
	y0 := b.Min.Y + int(math.Round(math.Max(c.Y, 0)*h))
	x1 := b.Min.X + int(math.Round(math.Min(c.X+c.W, 1)*w))
	y1 := b.Min.Y + int(math.Round(math.Min(c.Y+c.H, 1)*h))
	if x1 <= x0 {
		x1 = min(x0+1, b.Max.X)
		x0 = x1 - 1
	}
	if y1 <= y0 {
		y1 = min(y0+1, b.Max.Y)
		y0 = y1 - 1
	}
	return image.Rect(x0, y0, x1, y1)
}

// fit returns the size of a w by h picture scaled down to fit side by side,
// never up.
func fit(w, h, side int) (int, int) {
	if w <= side && h <= side {
		return w, h
	}
	if w >= h {
		return side, max(1, int(math.Round(float64(h)*float64(side)/float64(w))))
	}
	return max(1, int(math.Round(float64(w)*float64(side)/float64(h)))), side
}

// Avatar crops img, scales the result to fit MaxSide by MaxSide and encodes it
// as lossy WebP. The encoder writes the pixels only, so no EXIF, XMP or ICC
// profile survives (ADR-00033).
func Avatar(img image.Image, c Crop) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	src := c.rect(img.Bounds())
	w, h := fit(src.Dx(), src.Dy(), MaxSide)
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, src, xdraw.Src, nil)
	opts := webp.DefaultOptions()
	opts.Quality = webpQuality
	var buf bytes.Buffer
	if err := webp.Encode(&buf, dst, opts); err != nil {
		return nil, fmt.Errorf("encode avatar as WebP: %w", err)
	}
	return buf.Bytes(), nil
}

// Preview is what the person crops against: the picture, oriented and scaled to
// fit PreviewSide, as a JPEG that every browser shows, whatever the upload was.
// Transparent areas are drawn on white.
func Preview(img image.Image) ([]byte, error) {
	b := img.Bounds()
	w, h := fit(b.Dx(), b.Dy(), PreviewSide)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("encode crop preview: %w", err)
	}
	return buf.Bytes(), nil
}

// exifOrientation reads the EXIF orientation (1 to 8) of a JPEG, or 1 when
// there is none. Browsers show a JPEG upright using it, so the server must
// agree on which way is up before applying a crop.
func exifOrientation(data []byte) int {
	const none = 1
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return none
	}
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return none
		}
		marker := data[i+1]
		if marker == 0xFF {
			i++
			continue
		}
		if marker == 0xDA || marker == 0xD9 { // image data starts, no EXIF came first
			return none
		}
		size := int(data[i+2])<<8 | int(data[i+3])
		if size < 2 || i+2+size > len(data) {
			return none
		}
		seg := data[i+4 : i+2+size]
		if marker == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return tiffOrientation(seg[6:])
		}
		i += 2 + size
	}
	return none
}

func tiffOrientation(t []byte) int {
	const none = 1
	if len(t) < 8 {
		return none
	}
	var u16 func([]byte) int
	var u32 func([]byte) int
	switch string(t[:2]) {
	case "II":
		u16 = func(b []byte) int { return int(b[0]) | int(b[1])<<8 }
		u32 = func(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 | int(b[3])<<24 }
	case "MM":
		u16 = func(b []byte) int { return int(b[0])<<8 | int(b[1]) }
		u32 = func(b []byte) int { return int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3]) }
	default:
		return none
	}
	off := u32(t[4:8])
	if off < 8 || off+2 > len(t) {
		return none
	}
	n := u16(t[off:])
	for k := 0; k < n; k++ {
		e := off + 2 + 12*k
		if e+12 > len(t) {
			return none
		}
		if u16(t[e:]) == 0x0112 {
			if o := u16(t[e+8:]); o >= 1 && o <= 8 {
				return o
			}
			return none
		}
	}
	return none
}

// orient returns img turned upright for EXIF orientation o.
func orient(img image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var nx, ny int
			switch o {
			case 2:
				nx, ny = w-1-x, y
			case 3:
				nx, ny = w-1-x, h-1-y
			case 4:
				nx, ny = x, h-1-y
			case 5:
				nx, ny = y, x
			case 6:
				nx, ny = h-1-y, x
			case 7:
				nx, ny = h-1-y, w-1-x
			case 8:
				nx, ny = y, w-1-x
			}
			dst.Set(nx, ny, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
