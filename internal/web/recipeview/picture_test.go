package recipeview

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/store"
)

type fakePictures struct {
	src string
	err error
}

func (f fakePictures) Src(context.Context, int64) (string, error) { return f.src, f.err }

func withPictures(t *testing.T, p Pictures) (int, string) {
	t.Helper()
	r := chi.NewRouter()
	r.Mount(Pattern, Routes(Deps{Recipes: fakeRecipes{7: testRecipe(t)}, Catalog: testCatalog(), Pictures: p}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/recipes/7-sponge-cake", nil))
	return rec.Code, rec.Body.String()
}

func TestHeaderShowsThePictureAndLoadsItAtOnce(t *testing.T) {
	code, body := withPictures(t, fakePictures{src: "/recipes/7/picture?v=abc"})
	if code != http.StatusOK || !strings.Contains(body, `<img src="/recipes/7/picture?v=abc"`) {
		t.Fatalf("status %d, header has no picture:\n%s", code, body)
	}
	// It is the main image at the top of the page; deferring it delays the first paint.
	if strings.Contains(body, `loading="lazy"`) {
		t.Errorf("the header picture is lazy-loaded:\n%s", body)
	}
}

// A recipe deleted between reading it and asking for its picture has none to
// show; the page still answers, with the placeholder band.
func TestHeaderWithAVanishedRecipeShowsTheBand(t *testing.T) {
	code, body := withPictures(t, fakePictures{err: fmt.Errorf("recipe 7: %w", store.ErrNotFound)})
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if strings.Contains(body, "data-pic-img") || !strings.Contains(body, `class="rhero ph ph1"`) {
		t.Errorf("want the band ph1 and no image:\n%s", body)
	}
}

func TestPictureLookupFailureIsAServerError(t *testing.T) {
	code, body := withPictures(t, fakePictures{err: errors.New("disk on fire")})
	if code != http.StatusInternalServerError || strings.Contains(body, "disk on fire") {
		t.Errorf("status %d, body leaks cause: %v", code, strings.Contains(body, "disk on fire"))
	}
}
