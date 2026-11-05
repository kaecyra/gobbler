// Package picseed gives a seeded recipe a picture, through the real picture
// service and a local media store under the data directory, which is where the
// server under test reads it. It is test tooling shared by the e2e seeds that
// show recipe pictures (ADR-00013, ADR-00039).
package picseed

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"path/filepath"

	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/picture"
	"github.com/kaecyra/gobbler/internal/store"
)

// Give uploads a banded picture of colour c as recipe id's picture. dir is the
// data directory the server will run on; ctx carries the household (seedutil.Open).
func Give(ctx context.Context, d *sql.DB, dir string, id int64, c color.RGBA) error {
	img := image.NewRGBA(image.Rect(0, 0, 1200, 400))
	for y := range 400 {
		shade := uint8(140)
		if y/40%2 == 1 {
			shade = 255
		}
		px := color.RGBA{R: uint8(int(c.R) * int(shade) / 255), G: uint8(int(c.G) * int(shade) / 255), B: uint8(int(c.B) * int(shade) / 255), A: 255}
		for x := range 1200 {
			img.SetRGBA(x, y, px)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return fmt.Errorf("encode seed picture: %w", err)
	}
	svc := picture.New(d, media.NewLocal(filepath.Join(dir, media.LocalDirName)), store.New(d), nil, nil)
	if _, err := svc.Upload(ctx, id, buf.Bytes()); err != nil {
		return fmt.Errorf("give recipe %d a picture: %w", id, err)
	}
	return nil
}
