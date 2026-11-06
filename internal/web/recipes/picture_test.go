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
		t.Fatalf("status %d, card has no picture:\n%s", code, body)
	}
	// Cards can be far down the page, so their pictures wait until they are near.
	if !strings.Contains(body, `loading="lazy"`) {
		t.Errorf("the card picture is not lazy-loaded:\n%s", body)
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

func TestHomeSuggestionIsALibraryCardWithItsPicture(t *testing.T) {
	lib := &fakeLibrary{idle: []store.Card{card(4, "Never cake", recipe.MealDessert)}}
	h := Home(Deps{Library: lib, Pictures: fakePictures{src: "/recipes/4/picture?v=abc"}, Now: func() time.Time { return today }})
	for name, header := range map[string]map[string]string{
		"page":     nil,
		"fragment": {"HX-Request": "true", "HX-Target": "suggest"},
	} {
		code, body, _ := do(t, h, "/", header)
		if code != http.StatusOK {
			t.Fatalf("%s: status %d", name, code)
		}
		for _, want := range []string{`<article class="rcard">`, `data-testid="card-picture"`, `src="/recipes/4/picture?v=abc"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: the suggestion lacks %s:\n%s", name, want, body)
			}
		}
		if strings.Contains(body, "Open recipe") {
			t.Errorf("%s: the card opens the recipe, so no separate button", name)
		}
	}

	h = Home(Deps{Library: lib, Pictures: fakePictures{err: errors.New("disk on fire")}, Now: func() time.Time { return today }})
	if code, _, _ := do(t, h, "/", nil); code != http.StatusInternalServerError {
		t.Errorf("a picture lookup failure gave status %d, want 500", code)
	}
}

func TestLibrarySuggestionIsALibraryCardWithItsPicture(t *testing.T) {
	lib := &fakeLibrary{cards: []store.Card{card(4, "Never cake", recipe.MealDessert)}, idle: nil}
	lib.idle = lib.cards
	h := Routes(Deps{Library: lib, Pictures: fakePictures{src: "/recipes/4/picture?v=abc"}, Now: func() time.Time { return today }})
	for name, header := range map[string]map[string]string{
		"page":     nil,
		"fragment": {"HX-Request": "true", "HX-Target": "suggest"},
	} {
		_, body, _ := do(t, h, "/?q=zzz", header)
		start := strings.Index(body, `id="suggest"`)
		if start < 0 {
			t.Fatalf("%s: no suggestion:\n%s", name, body)
		}
		got := body[start:]
		for _, want := range []string{`<article class="rcard">`, `src="/recipes/4/picture?v=abc"`} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: the suggestion lacks %s", name, want)
			}
		}
	}
}
