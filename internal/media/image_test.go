package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/gen2brain/avif"
)

// halves is a w by h picture, red on the left half and blue on the right.
func halves(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(w/2, 0, w, h), image.NewUniform(color.RGBA{B: 255, A: 255}), image.Point{}, draw.Src)
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// segment is one JPEG marker segment.
func segment(marker byte, payload []byte) []byte {
	out := []byte{0xFF, marker, 0, 0}
	binary.BigEndian.PutUint16(out[2:], uint16(len(payload)+2))
	return append(out, payload...)
}

// exifWith builds an EXIF APP1 payload with an orientation and a GPS IFD.
func exifWith(orientation uint16) []byte {
	le := binary.LittleEndian
	t := make([]byte, 8)
	copy(t, "II*\x00")
	le.PutUint32(t[4:], 8)
	// IFD0 at 8: two entries (orientation, GPS pointer), then the GPS IFD.
	ifd0 := make([]byte, 2+2*12+4)
	le.PutUint16(ifd0, 2)
	le.PutUint16(ifd0[2:], 0x0112)
	le.PutUint16(ifd0[4:], 3)
	le.PutUint32(ifd0[6:], 1)
	le.PutUint16(ifd0[10:], orientation)
	le.PutUint16(ifd0[14:], 0x8825)
	le.PutUint16(ifd0[16:], 4)
	le.PutUint32(ifd0[18:], 1)
	le.PutUint32(ifd0[22:], uint32(8+len(ifd0)))
	gps := make([]byte, 2+12+4)
	le.PutUint16(gps, 1)
	le.PutUint16(gps[2:], 0x0001) // GPSLatitudeRef
	le.PutUint16(gps[4:], 2)
	le.PutUint32(gps[6:], 2)
	copy(gps[10:], "N\x00")
	tiff := append(append(t, ifd0...), gps...)
	return append([]byte("Exif\x00\x00"), tiff...)
}

// taggedJPEG is a JPEG carrying EXIF with a GPS tag, an XMP packet and an ICC
// profile marker: everything ADR-00033 says must not survive.
func taggedJPEG(t *testing.T, img image.Image, orientation uint16) []byte {
	t.Helper()
	plain := encodeJPEG(t, img)
	var out bytes.Buffer
	out.Write(plain[:2])
	out.Write(segment(0xE1, exifWith(orientation)))
	out.Write(segment(0xE1, []byte("http://ns.adobe.com/xap/1.0/\x00<x:xmpmeta>SECRETPLACE</x:xmpmeta>")))
	out.Write(segment(0xE2, append([]byte("ICC_PROFILE\x00\x01\x01"), bytes.Repeat([]byte{7}, 64)...)))
	out.Write(plain[2:])
	return out.Bytes()
}

// riffChunks lists the chunk names of a WebP file.
func riffChunks(t *testing.T, b []byte) []string {
	t.Helper()
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		t.Fatalf("not a WebP file: % x", b[:min(len(b), 16)])
	}
	var names []string
	for i := 12; i+8 <= len(b); {
		names = append(names, string(b[i:i+4]))
		size := int(binary.LittleEndian.Uint32(b[i+4:]))
		i += 8 + size + size%2
	}
	return names
}

func TestAvatarIsLossyWebPWithinBoundsAndHasNoMetadata(t *testing.T) {
	src := taggedJPEG(t, halves(3000, 2000), 1)
	for _, marker := range []string{"Exif", "ICC_PROFILE", "xmpmeta", "SECRETPLACE"} {
		if !bytes.Contains(src, []byte(marker)) {
			t.Fatalf("test fixture lacks %q", marker)
		}
	}
	d, err := Decode(src)
	if err != nil {
		t.Fatal(err)
	}
	if d.Format != "jpeg" {
		t.Errorf("Format = %q", d.Format)
	}
	out, err := Avatar(d.Image, Full)
	if err != nil {
		t.Fatal(err)
	}
	chunks := riffChunks(t, out)
	for _, c := range chunks {
		switch c {
		case "EXIF", "XMP ", "ICCP":
			t.Errorf("stored avatar has a %q chunk; chunks = %v", c, chunks)
		}
	}
	if !slicesContains(chunks, "VP8 ") {
		t.Errorf("want a lossy VP8 chunk, chunks = %v", chunks)
	}
	for _, leak := range []string{"Exif", "GPS", "ICC_PROFILE", "xmpmeta", "SECRETPLACE"} {
		if bytes.Contains(out, []byte(leak)) {
			t.Errorf("stored avatar contains %q", leak)
		}
	}
	got, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 1024 || got.Height != 683 {
		t.Errorf("size = %dx%d, want 1024x683", got.Width, got.Height)
	}
}

func slicesContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestAvatarAppliesTheCropAndNeverScalesUp(t *testing.T) {
	d, err := Decode(encodeJPEG(t, halves(200, 100)))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Avatar(d.Image, Crop{X: 0.5, Y: 0, W: 0.5, H: 1})
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 100 || b.Dy() != 100 {
		t.Fatalf("size = %v, want 100x100", b)
	}
	r, _, bl, _ := img.At(50, 50).RGBA()
	if bl>>8 < 200 || r>>8 > 60 {
		t.Errorf("the cropped right half should be blue, got r=%d b=%d", r>>8, bl>>8)
	}
}

func TestCropValidation(t *testing.T) {
	d, err := Decode(encodeJPEG(t, halves(40, 40)))
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]Crop{
		"past the right edge": {X: 0.6, Y: 0, W: 0.6, H: 1},
		"negative":            {X: -0.2, Y: 0, W: 0.5, H: 0.5},
		"empty":               {X: 0, Y: 0, W: 0, H: 0.5},
	} {
		if _, err := Avatar(d.Image, c); err == nil {
			t.Errorf("%s: want a refusal", name)
		} else if _, ok := RefusalText(err); !ok {
			t.Errorf("%s: %v is not a Refusal", name, err)
		}
	}
}

// noise is a picture that does not compress to nothing, so a cut in the file
// lands in the image data and not in the headers.
func noise(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * y), G: uint8(x ^ y), B: uint8(x + 3*y), A: 255})
		}
	}
	return img
}

func cutInHalf(b []byte) []byte { return b[:len(b)/2] }

func TestDecodeRefusals(t *testing.T) {
	const formats = "JPEG, PNG, GIF, WebP, HEIC or AVIF"
	cases := map[string]struct {
		data []byte
		want string
	}{
		"over 5 MB":      {bytes.Repeat([]byte{0}, MaxUploadBytes+1), "5 MB"},
		"empty":          {nil, "empty"},
		"not a picture":  {[]byte("%PDF-1.7 this is a document"), formats},
		"unsupported":    {[]byte("BM" + strings.Repeat("\x00", 60)), formats},
		"damaged":        {cutInHalf(encodeJPEG(t, noise(300, 300))), "could not be read"},
		"text as a jpeg": {append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte("not really")...), "could not be read"},
	}
	for name, tc := range cases {
		_, err := Decode(tc.data)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if msg, ok := RefusalText(err); !ok || !strings.Contains(msg, tc.want) {
			t.Errorf("%s: %v, want a refusal mentioning %q", name, err, tc.want)
		}
	}
}

func TestDecodeAcceptsEveryAllowedFormat(t *testing.T) {
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, halves(20, 10)); err != nil {
		t.Fatal(err)
	}
	var avifBuf bytes.Buffer
	if err := avif.Encode(&avifBuf, halves(32, 32)); err != nil {
		t.Fatal(err)
	}
	heic, err := os.ReadFile("testdata/sample.heic")
	if err != nil {
		t.Fatal(err)
	}
	webpSrc, err := Avatar(halves(30, 30), Full)
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		data   []byte
		format string
	}{
		"jpeg": {encodeJPEG(t, halves(20, 10)), "jpeg"},
		"png":  {pngBuf.Bytes(), "png"},
		"webp": {webpSrc, "webp"},
		"avif": {avifBuf.Bytes(), "avif"},
		"heic": {heic, "heic"},
	} {
		d, err := Decode(tc.data)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if d.Format != tc.format {
			t.Errorf("%s: Format = %q", name, d.Format)
		}
		if _, err := Avatar(d.Image, Full); err != nil {
			t.Errorf("%s: avatar: %v", name, err)
		}
	}
}

func TestDecodeAppliesExifOrientation(t *testing.T) {
	// Orientation 6 means "rotate 90 clockwise to view": a 40x20 picture is
	// 20x40 upright, and its red left half is the top.
	d, err := Decode(taggedJPEG(t, halves(40, 20), 6))
	if err != nil {
		t.Fatal(err)
	}
	if b := d.Image.Bounds(); b.Dx() != 20 || b.Dy() != 40 {
		t.Fatalf("size = %v, want 20x40", b)
	}
	r, _, _, _ := d.Image.At(10, 2).RGBA()
	_, _, bl, _ := d.Image.At(10, 37).RGBA()
	if r>>8 < 200 || bl>>8 < 200 {
		t.Errorf("want red on top and blue at the bottom, got r=%d b=%d", r>>8, bl>>8)
	}
}

func TestPreviewIsAJPEGThatFits(t *testing.T) {
	heic, err := os.ReadFile("testdata/sample.heic")
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"heic": heic, "large jpeg": encodeJPEG(t, halves(3200, 1600))} {
		d, err := Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		p, err := Preview(d.Image)
		if err != nil {
			t.Fatal(err)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(p))
		if err != nil || format != "jpeg" {
			t.Fatalf("%s: preview is %q, %v", name, format, err)
		}
		if cfg.Width > PreviewSide || cfg.Height > PreviewSide {
			t.Errorf("%s: preview %dx%d exceeds %d", name, cfg.Width, cfg.Height, PreviewSide)
		}
	}
}
