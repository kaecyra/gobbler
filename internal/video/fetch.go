package video

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"
)

// thumbnailHost is YouTube's image host. The thumbnail address was checked
// live on 2026-10-17: https://i.ytimg.com/vi/<id>/hqdefault.jpg answers 200
// image/jpeg.
const thumbnailHost = "https://i.ytimg.com"

// MaxThumbnailBytes bounds a downloaded thumbnail; a real hqdefault is tens of
// kilobytes.
const MaxThumbnailBytes = 1 << 20

// fetchTimeout bounds one download, so a save never waits long on YouTube.
const fetchTimeout = 8 * time.Second

// Thumbnail is a downloaded preview image.
type Thumbnail struct {
	Content     []byte
	ContentType string
}

// Fetcher downloads thumbnails from YouTube's image host.
type Fetcher struct {
	// Client sends the request; nil means a client with a short timeout.
	Client *http.Client
	// BaseURL replaces the image host; empty means YouTube's. It exists so a
	// test can point the fetcher at a local server, the HTTP boundary.
	BaseURL string
}

// NewFetcher returns a Fetcher for YouTube's image host.
func NewFetcher() *Fetcher { return &Fetcher{} }

// Fetch downloads the hqdefault thumbnail of video id. Any answer but an image
// of a sane size is an error.
func (f *Fetcher) Fetch(ctx context.Context, id string) (Thumbnail, error) {
	if !IsID(id) {
		return Thumbnail{}, fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	base := f.BaseURL
	if base == "" {
		base = thumbnailHost
	}
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: fetchTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+thumbnailPath(id), nil)
	if err != nil {
		return Thumbnail{}, fmt.Errorf("build thumbnail request for %s: %w", id, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Thumbnail{}, fmt.Errorf("download thumbnail of %s: %w", id, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Thumbnail{}, fmt.Errorf("download thumbnail of %s: status %d", id, resp.StatusCode)
	}
	// Only raster types are kept: the page serves the stored type back, and
	// an SVG is a document that can carry script.
	ct, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !allowedTypes[ct] {
		return Thumbnail{}, fmt.Errorf("download thumbnail of %s: content type %q is not a JPEG, PNG or WebP image", id, resp.Header.Get("Content-Type"))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxThumbnailBytes+1))
	if err != nil {
		return Thumbnail{}, fmt.Errorf("read thumbnail of %s: %w", id, err)
	}
	if len(body) > MaxThumbnailBytes {
		return Thumbnail{}, fmt.Errorf("download thumbnail of %s: %w", id, errTooLarge)
	}
	if len(body) == 0 {
		return Thumbnail{}, fmt.Errorf("download thumbnail of %s: empty body", id)
	}
	return Thumbnail{Content: body, ContentType: ct}, nil
}

var allowedTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

var errTooLarge = errors.New("thumbnail is larger than the limit")

// ThumbnailStore is what EnsureThumbnail needs from the recipe store.
type ThumbnailStore interface {
	HasThumbnail(ctx context.Context, videoID string) (bool, error)
	SaveThumbnail(ctx context.Context, videoID string, content []byte, contentType string) error
}

// ThumbnailSource downloads a thumbnail; *Fetcher is one.
type ThumbnailSource interface {
	Fetch(ctx context.Context, id string) (Thumbnail, error)
}

// EnsureThumbnail makes sure a thumbnail is stored for video id: nothing
// happens when one is, and otherwise it is downloaded and stored. The caller
// has already saved the recipe, so a returned error is to be logged, never to
// fail that save (ADR-00021 decision 3); the next save of a recipe with this ID
// tries again.
func EnsureThumbnail(ctx context.Context, st ThumbnailStore, src ThumbnailSource, id string) error {
	have, err := st.HasThumbnail(ctx, id)
	if err != nil {
		return fmt.Errorf("check thumbnail of %s: %w", id, err)
	}
	if have {
		return nil
	}
	t, err := src.Fetch(ctx, id)
	if err != nil {
		return err
	}
	if err := st.SaveThumbnail(ctx, id, t.Content, t.ContentType); err != nil {
		return fmt.Errorf("store thumbnail of %s: %w", id, err)
	}
	return nil
}
