package recipeview

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

const testVideo = "dQw4w9WgXcQ"

// videoRecipe stores a recipe with the given video and returns its id and a
// getter for its page.
func videoRecipe(t *testing.T, videoID string, thumb bool) (page func(path string) *httptest.ResponseRecorder, id int64, rs *store.Store) {
	t.Helper()
	ctx, rs, cs := storeEnv(t)
	r := testRecipe(t)
	r.Components[0].Lines = []recipe.IngredientLine{r.Components[0].Lines[1]}
	r.Equipment = nil
	r.VideoID = videoID
	id, err := rs.Create(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if thumb {
		if err := rs.SaveThumbnail(ctx, videoID, []byte("\xff\xd8thumb"), "image/jpeg"); err != nil {
			t.Fatal(err)
		}
	}
	return func(path string) *httptest.ResponseRecorder {
		return serveRequest(t, rs, cs, httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx))
	}, id, rs
}

// attrs returns every src and href value of the page.
func attrs(body string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`\b(?:src|href)="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// ADR-00021 decision 2: until Play is pressed the page holds nothing a browser
// would load from a YouTube origin: no iframe, and no src or stylesheet or
// script address on that host. (The play control's href is a link followed
// only on a click, and the embed address sits in a data attribute.)
func TestVideoPageLoadsNothingFromYouTubeBeforePlay(t *testing.T) {
	page, id, _ := videoRecipe(t, testVideo, true)
	rec := page(fmt.Sprintf("/recipes/%d", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	mustContain(t, body,
		`data-testid="video"`, `data-video-play`,
		`data-embed="https://www.youtube-nocookie.com/embed/`+testVideo+`?autoplay=1&amp;rel=0"`,
		fmt.Sprintf(`src="/recipes/%d/video/thumbnail"`, id),
		`href="/static/video.css"`, `src="/static/video.js"`)
	mustNotContain(t, body, "<iframe", "ytimg.com", "youtube-nocookie.com/embed/"+testVideo+"\"")
	for _, a := range attrs(body) {
		if strings.Contains(a, "youtube") && !strings.HasPrefix(a, "https://www.youtube.com/watch?v=") {
			t.Errorf("page loads %q before Play", a)
		}
	}
	// Every src on the page is the application's own, except none.
	for _, m := range regexp.MustCompile(`\bsrc="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		if !strings.HasPrefix(m[1], "/") {
			t.Errorf("src %q is not from this origin", m[1])
		}
	}
}

func TestVideoPageWithoutAStoredThumbnailShowsThePlaceholder(t *testing.T) {
	page, id, _ := videoRecipe(t, testVideo, false)
	body := page(fmt.Sprintf("/recipes/%d", id)).Body.String()
	mustContain(t, body, `data-testid="video"`, "Play video")
	mustNotContain(t, body, `data-testid="video-thumbnail"`, "/video/thumbnail")
}

func TestPageOfARecipeWithoutAVideoHasNoPlayer(t *testing.T) {
	ctx, rs, cs := storeEnv(t)
	id, err := rs.Create(ctx, testRecipe(t))
	if err != nil {
		t.Fatal(err)
	}
	rec := serveRequest(t, rs, cs, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/recipes/%d", id), nil).WithContext(ctx))
	mustNotContain(t, rec.Body.String(), `data-testid="video"`, "video.js", "video.css")
}

func TestThumbnailRouteServesTheStoredImageThroughTheRecipe(t *testing.T) {
	page, id, _ := videoRecipe(t, testVideo, true)
	rec := page(fmt.Sprintf("/recipes/%d/video/thumbnail", id))
	if rec.Code != http.StatusOK || rec.Body.String() != "\xff\xd8thumb" {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
	h := rec.Header()
	if h.Get("Content-Type") != "image/jpeg" || h.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(h.Get("Cache-Control"), "private") {
		t.Errorf("headers = %v", h)
	}
}

func TestThumbnailRouteIs404WhenThereIsNothingToServe(t *testing.T) {
	page, id, _ := videoRecipe(t, testVideo, false)
	for name, path := range map[string]string{
		"a video with no stored image": fmt.Sprintf("/recipes/%d/video/thumbnail", id),
		"a recipe that does not exist": "/recipes/9999/video/thumbnail",
		"a bad id":                     "/recipes/abc/video/thumbnail",
	} {
		if rec := page(path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", name, rec.Code)
		}
	}
}
