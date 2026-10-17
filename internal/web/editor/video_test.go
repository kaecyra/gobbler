package editor

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/video"
)

const videoID = "dQw4w9WgXcQ"

// stubThumbs is the HTTP boundary of the thumbnail download: it records the IDs
// asked for and either returns an image or fails.
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

// postVideo saves the stored recipe with the video field set to text.
func (e *env) postVideo(text string) result {
	e.t.Helper()
	f := e.form()
	f.Set("video", text)
	return e.post(f)
}

func TestShowFillsTheVideoFieldWithTheStoredID(t *testing.T) {
	e := newEnv(t)
	if res := e.postVideo(videoID); res.Code != http.StatusSeeOther {
		t.Fatalf("save status = %d: %s", res.Code, res.body())
	}
	if body := e.get().body(); !strings.Contains(body, `name="video"`) || !strings.Contains(body, `value="`+videoID+`"`) {
		t.Errorf("editor page lacks a video field holding %q", videoID)
	}
}

func TestPastedLinksAreStoredAsTheBareID(t *testing.T) {
	for _, in := range []string{
		"https://www.youtube.com/watch?v=" + videoID,
		"https://youtu.be/" + videoID + "?si=x",
		"https://www.youtube.com/shorts/" + videoID,
		"https://www.youtube.com/embed/" + videoID,
		videoID,
	} {
		e := newEnv(t)
		if res := e.postVideo(in); res.Code != http.StatusSeeOther {
			t.Fatalf("%q: save status = %d: %s", in, res.Code, res.body())
		}
		if got, _ := e.stored(); got.VideoID != videoID {
			t.Errorf("%q stored as %q, want %q", in, got.VideoID, videoID)
		}
	}
}

func TestAnUnreadableVideoIsRefusedWithAMessageAndSavesNothing(t *testing.T) {
	e := newEnv(t)
	_, before := e.stored()
	res := e.postVideo("https://vimeo.com/12345")
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.Code)
	}
	body := res.body()
	if !strings.Contains(body, "is not a YouTube video") || !strings.Contains(body, "https://vimeo.com/12345") {
		t.Errorf("no message naming what was typed: %s", body)
	}
	if got, v := e.stored(); got.VideoID != "" || v != before {
		t.Errorf("stored video %q at version %d (was %d); a refused save must change nothing", got.VideoID, v, before)
	}
	if len(e.thumbs.asked) != 0 {
		t.Errorf("a refused save downloaded %v", e.thumbs.asked)
	}
}

func TestSavingAVideoDownloadsItsThumbnailOnceAndKeepsItForTheRecipe(t *testing.T) {
	e := newEnv(t)
	if res := e.postVideo(videoID); res.Code != http.StatusSeeOther {
		t.Fatalf("status = %d: %s", res.Code, res.body())
	}
	got, err := e.store.Thumbnail(e.ctx, e.id)
	if err != nil || string(got.Content) != "jpeg:"+videoID {
		t.Fatalf("Thumbnail = %q (%v), want the downloaded image", got.Content, err)
	}
	// A second save with the same video downloads nothing again.
	if res := e.postVideo(videoID); res.Code != http.StatusSeeOther {
		t.Fatalf("second status = %d: %s", res.Code, res.body())
	}
	if len(e.thumbs.asked) != 1 {
		t.Errorf("downloads = %v, want one for the one video", e.thumbs.asked)
	}
}

// ADR-00021 obligation 3: a download that fails is logged, the save succeeds,
// and the recipe has no thumbnail (the page then shows the placeholder). A later
// save retries.
func TestAFailedThumbnailDownloadDoesNotFailTheSave(t *testing.T) {
	e := newEnv(t)
	e.thumbs.fail = errors.New("youtube is unreachable")
	if res := e.postVideo(videoID); res.Code != http.StatusSeeOther {
		t.Fatalf("save status = %d, want the save to succeed: %s", res.Code, res.body())
	}
	if got, _ := e.stored(); got.VideoID != videoID {
		t.Errorf("stored video = %q, want %q kept although the download failed", got.VideoID, videoID)
	}
	if _, err := e.store.Thumbnail(e.ctx, e.id); !errors.Is(err, store.ErrNoThumbnail) {
		t.Errorf("Thumbnail after a failed download = %v, want ErrNoThumbnail", err)
	}

	e.thumbs.fail = nil
	if res := e.postVideo(videoID); res.Code != http.StatusSeeOther {
		t.Fatalf("retry status = %d: %s", res.Code, res.body())
	}
	if _, err := e.store.Thumbnail(e.ctx, e.id); err != nil {
		t.Errorf("Thumbnail after the retry = %v, want the image", err)
	}
	if len(e.thumbs.asked) != 2 {
		t.Errorf("downloads = %v, want the failed one and the retry", e.thumbs.asked)
	}
}

func TestClearingTheVideoRemovesItAndDownloadsNothing(t *testing.T) {
	e := newEnv(t)
	if res := e.postVideo(videoID); res.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", res.Code)
	}
	if res := e.postVideo("  "); res.Code != http.StatusSeeOther {
		t.Fatalf("clearing status = %d: %s", res.Code, res.body())
	}
	if got, _ := e.stored(); got.VideoID != "" {
		t.Errorf("video after clearing = %q, want none", got.VideoID)
	}
	if len(e.thumbs.asked) != 1 {
		t.Errorf("downloads = %v, want only the first save's", e.thumbs.asked)
	}
}
