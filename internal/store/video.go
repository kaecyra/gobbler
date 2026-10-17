package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kaecyra/gobbler/internal/store/storedb"
	"github.com/kaecyra/gobbler/internal/video"
)

// A recipe's YouTube video is its 11-character ID, a field of the recipe
// document (ADR-00021). The preview image is a different thing: one row per
// video ID in video_thumbnails, shared by every recipe and revision that names
// the video, written when a recipe naming it is saved and read only through a
// recipe of the requesting household.

// ErrNoThumbnail is returned by Thumbnail when the recipe has no video, or its
// video has no stored thumbnail. Callers show the placeholder. It wraps
// ErrNotFound, so a missing recipe and a missing thumbnail are the same answer
// to an HTTP caller.
var ErrNoThumbnail = fmt.Errorf("%w: no thumbnail", ErrNotFound)

// Thumbnail is a stored preview image.
type Thumbnail struct {
	Content     []byte
	ContentType string
}

// setVideo writes the video ID of recipe id inside the transaction q is bound
// to. An ID that is not well formed is a document error, ErrInvalid.
func setVideo(ctx context.Context, q *storedb.Queries, hid, id int64, videoID string) error {
	if videoID != "" && !video.IsID(videoID) {
		return fmt.Errorf("%w: video id %q", ErrInvalid, videoID)
	}
	if err := q.SetRecipeVideo(ctx, storedb.SetRecipeVideoParams{VideoID: videoID, ID: id, HouseholdID: hid}); err != nil {
		return fmt.Errorf("write recipe %d video: %w", id, err)
	}
	return nil
}

// HasThumbnail reports whether a thumbnail is stored for video ID videoID. It
// answers the save path's question (does this ID still need a download?), not a
// page's, and so takes no recipe; it reveals nothing to a caller, which only
// ever passes IDs from its own recipes.
func (s *Store) HasThumbnail(ctx context.Context, videoID string) (bool, error) {
	if _, err := scope(ctx); err != nil {
		return false, err
	}
	ok, err := s.q.HasVideoThumbnail(ctx, videoID)
	if err != nil {
		return false, readErr(fmt.Sprintf("thumbnail of video %s", videoID), err)
	}
	return ok, nil
}

// SaveThumbnail stores the downloaded thumbnail of video ID videoID. The first
// download of an ID is kept; saving again changes nothing. An ID that is not a
// video ID, or an empty image, is ErrInvalid.
func (s *Store) SaveThumbnail(ctx context.Context, videoID string, content []byte, contentType string) error {
	if _, err := identity(ctx); err != nil {
		return err
	}
	if _, err := scope(ctx); err != nil {
		return err
	}
	if !video.IsID(videoID) || len(content) == 0 || contentType == "" {
		return fmt.Errorf("%w: thumbnail of video %q", ErrInvalid, videoID)
	}
	return s.withTx(ctx, func(q *storedb.Queries) error {
		err := q.InsertVideoThumbnail(ctx, storedb.InsertVideoThumbnailParams{
			VideoID: videoID, Content: content, ContentType: contentType, FetchedAt: s.stamp(),
		})
		if err != nil {
			return wrapWrite(fmt.Sprintf("write thumbnail of video %s", videoID), err)
		}
		return nil
	})
}

// Thumbnail returns the stored thumbnail of the video of recipe id (of the
// request's household). It is the only way to read one: by recipe, never by
// video ID, so one household cannot learn which videos another has stored. A
// missing or foreign recipe, a recipe with no video and a video with no
// thumbnail are all ErrNoThumbnail (which is also ErrNotFound).
func (s *Store) Thumbnail(ctx context.Context, id int64) (Thumbnail, error) {
	hid, err := scope(ctx)
	if err != nil {
		return Thumbnail{}, err
	}
	row, err := s.q.GetRecipeThumbnail(ctx, storedb.GetRecipeThumbnailParams{ID: id, HouseholdID: hid})
	if errors.Is(err, sql.ErrNoRows) {
		return Thumbnail{}, fmt.Errorf("%w: recipe %d", ErrNoThumbnail, id)
	}
	if err != nil {
		return Thumbnail{}, readErr(fmt.Sprintf("thumbnail of recipe %d", id), err)
	}
	return Thumbnail{Content: row.Content, ContentType: row.ContentType}, nil
}
