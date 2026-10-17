package video

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"
)

// DefaultThumbnailHost is YouTube's image host, the default of the configured
// thumbnail host. The thumbnail address was checked live on 2026-10-17:
// https://i.ytimg.com/vi/<id>/hqdefault.jpg answers 200 image/jpeg.
const DefaultThumbnailHost = "https://i.ytimg.com"

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
	// Client sends the request; nil means a client with a short timeout that
	// does not follow redirects.
	Client *http.Client
	// BaseURL replaces the image host; empty means YouTube's. It exists so a
	// test can point the fetcher at a local server, the HTTP boundary.
	BaseURL string
}

// NewFetcher returns a Fetcher for YouTube's image host.
func NewFetcher() *Fetcher { return &Fetcher{} }

// NewFetcherAt returns a Fetcher for the image host at base, the configured
// thumbnail host (GOBBLER_VIDEO_THUMBNAIL_HOST); empty means YouTube's.
// Production leaves it unset; the e2e suite points it at a local fixture.
func NewFetcherAt(base string) *Fetcher { return &Fetcher{BaseURL: strings.TrimRight(base, "/")} }

// Fetch downloads the hqdefault thumbnail of video id. Any answer but an image
// of a sane size is an error.
func (f *Fetcher) Fetch(ctx context.Context, id string) (Thumbnail, error) {
	if !IsID(id) {
		return Thumbnail{}, fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	base := f.BaseURL
	if base == "" {
		base = DefaultThumbnailHost
	}
	client := f.Client
	if client == nil {
		// A redirect is not followed: the download stays pinned to the image
		// host, and a redirect is a failed download (ADR-00021).
		client = &http.Client{
			Timeout:       fetchTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
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

// KeepThumbnail is what every save of a recipe revision with a video calls
// afterwards (ADR-00021 decision 3): it ensures the thumbnail of videoID is
// stored, downloading it from src, or from YouTube's image host when src is
// nil. The revision is already saved, so a failure is logged and never
// returned; the recipe shows the placeholder until the next save of a recipe
// with this video tries again. An empty videoID does nothing.
func KeepThumbnail(ctx context.Context, st ThumbnailStore, src ThumbnailSource, recipeID int64, videoID string) {
	if videoID == "" {
		return
	}
	if src == nil {
		src = NewFetcher()
	}
	if err := EnsureThumbnail(ctx, st, src, videoID); err != nil {
		slog.WarnContext(ctx, "recipe saved without a video thumbnail", "recipe_id", recipeID, "video_id", videoID, "error", err.Error())
	}
}

// KeepThumbnailAfter is KeepThumbnail for a handler that must read the saved
// recipe back to learn its video ID: readVideoID does that read. A failed read
// is logged and the thumbnail check skipped, since the revision is already
// saved and the next save of the recipe tries again.
func KeepThumbnailAfter(ctx context.Context, st ThumbnailStore, src ThumbnailSource, recipeID int64, readVideoID func(context.Context) (string, error)) {
	videoID, err := readVideoID(ctx)
	if err != nil {
		slog.WarnContext(ctx, "saved recipe left without a video thumbnail check", "recipe_id", recipeID, "error", err.Error())
		return
	}
	KeepThumbnail(ctx, st, src, recipeID, videoID)
}
