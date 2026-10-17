package ingest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/video"
)

const testVideoID = "dQw4w9WgXcQ"

// pancakesWith is the pancakes fixture with a schema.org "video" property
// added, or unchanged for an empty property.
func pancakesWith(t *testing.T, videoJSON string) []byte {
	t.Helper()
	page := string(readFixture(t, "pancakes.html"))
	if videoJSON == "" {
		return []byte(page)
	}
	out := strings.Replace(page, `"name":"Fluffy Pancakes",`, `"name":"Fluffy Pancakes","video":`+videoJSON+`,`, 1)
	if out == page {
		t.Fatal("fixture has no name to anchor the video on")
	}
	return []byte(out)
}

// stubThumbs is the HTTP boundary of the thumbnail download.
type stubThumbs struct {
	asked []string
	fail  error
}

func (s *stubThumbs) Fetch(_ context.Context, id string) (video.Thumbnail, error) {
	s.asked = append(s.asked, id)
	if s.fail != nil {
		return video.Thumbnail{}, s.fail
	}
	return video.Thumbnail{Content: []byte("jpeg:" + id), ContentType: "image/jpeg"}, nil
}

func (e *env) draftPage(t *testing.T, page []byte) Draft {
	t.Helper()
	srv, _ := serve(t, page)
	d, err := e.pipeline(t, srv.Client()).FromURL(e.ctx, srv.URL+"/pancakes")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// ADR-00021 obligation 5: the draft carries the ID only for a YouTube video.
func TestDraftCarriesAYouTubeVideoOnly(t *testing.T) {
	for name, tc := range map[string]struct{ video, want string }{
		"youtube":     {`{"@type":"VideoObject","embedUrl":"https://www.youtube.com/embed/` + testVideoID + `"}`, testVideoID},
		"other host":  {`{"@type":"VideoObject","embedUrl":"https://player.vimeo.com/video/1"}`, ""},
		"no property": {``, ""},
	} {
		t.Run(name, func(t *testing.T) {
			d := newEnv(t).draftPage(t, pancakesWith(t, tc.video))
			if d.VideoID != tc.want {
				t.Errorf("VideoID = %q, want %q", d.VideoID, tc.want)
			}
		})
	}
}

func TestSaveStoresTheVideoAndItsThumbnail(t *testing.T) {
	e := newEnv(t)
	d := e.draftPage(t, pancakesWith(t, `{"embedUrl":"https://youtu.be/`+testVideoID+`"}`))
	thumbs := &stubThumbs{}
	saved, err := e.saver().WithThumbnails(thumbs).Save(e.ctx, confirm(t, d))
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.store.Get(e.ctx, saved.RecipeID)
	if err != nil {
		t.Fatal(err)
	}
	if got.VideoID != testVideoID {
		t.Errorf("stored VideoID = %q", got.VideoID)
	}
	if have, err := e.store.HasThumbnail(e.ctx, testVideoID); err != nil || !have {
		t.Errorf("thumbnail stored = %v, %v", have, err)
	}
	if len(thumbs.asked) != 1 || thumbs.asked[0] != testVideoID {
		t.Errorf("thumbnail asked for = %v", thumbs.asked)
	}
}

// ADR-00021 decision 3: a failed download does not fail the save.
func TestSaveSucceedsWhenTheThumbnailDownloadFails(t *testing.T) {
	e := newEnv(t)
	d := e.draftPage(t, pancakesWith(t, `{"embedUrl":"https://youtu.be/`+testVideoID+`"}`))
	saved, err := e.saver().WithThumbnails(&stubThumbs{fail: errors.New("youtube down")}).Save(e.ctx, confirm(t, d))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := e.store.Get(e.ctx, saved.RecipeID)
	if err != nil || got.VideoID != testVideoID {
		t.Errorf("recipe = %q, %v", got.VideoID, err)
	}
	if have, _ := e.store.HasThumbnail(e.ctx, testVideoID); have {
		t.Error("a thumbnail was stored though the download failed")
	}
}

func TestSaveWithoutAVideoFetchesNothing(t *testing.T) {
	e := newEnv(t)
	thumbs := &stubThumbs{}
	if _, err := e.saver().WithThumbnails(thumbs).Save(e.ctx, confirm(t, e.draftPage(t, pancakesWith(t, "")))); err != nil {
		t.Fatal(err)
	}
	if len(thumbs.asked) != 0 {
		t.Errorf("thumbnail asked for = %v", thumbs.asked)
	}
}
