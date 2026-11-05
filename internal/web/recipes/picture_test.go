package recipes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

type fakePictures struct {
	src string
	err error
}

func (f fakePictures) Src(context.Context, int64) (string, error) { return f.src, f.err }

func withPictures(t *testing.T, p Pictures) (int, string) {
	t.Helper()
	lib := &fakeLibrary{cards: []store.Card{card(4, "Soup", recipe.MealMain)}}
	h := Routes(Deps{Library: lib, Pictures: p, Now: func() time.Time { return today }})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?group=time", nil))
	return rec.Code, rec.Body.String()
}

func TestCardShowsItsPicture(t *testing.T) {
	code, body := withPictures(t, fakePictures{src: "/recipes/4/picture?v=abc"})
	if code != http.StatusOK || !strings.Contains(body, `<img src="/recipes/4/picture?v=abc"`) {
		t.Errorf("status %d, card has no picture:\n%s", code, body)
	}
}

// A recipe deleted between reading the cards and asking for its picture has
// none to show; the library still answers, with the placeholder band.
func TestCardWithAVanishedRecipeShowsTheBand(t *testing.T) {
	code, body := withPictures(t, fakePictures{err: fmt.Errorf("recipe 4: %w", store.ErrNotFound)})
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if strings.Contains(body, "data-pic-img") || !strings.Contains(body, `class="rpic ph ph4"`) || !strings.Contains(body, `class="phmark"`) {
		t.Errorf("want the band ph4 with its meal tile and no image:\n%s", body)
	}
}

func TestPictureLookupFailureIsAServerError(t *testing.T) {
	code, body := withPictures(t, fakePictures{err: errors.New("disk on fire")})
	if code != http.StatusInternalServerError || strings.Contains(body, "disk on fire") {
		t.Errorf("status %d, body leaks cause: %v", code, strings.Contains(body, "disk on fire"))
	}
}
