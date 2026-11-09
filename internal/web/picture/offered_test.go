package picture_test

import (
	"net/http"
	"testing"

	"github.com/kaecyra/gobbler/internal/web/picture"
)

// ADR-00051: a picture offered on the edit page is served to the recipe's
// household only, under its ID.
func TestOfferedPictureIsServedToItsHouseholdOnly(t *testing.T) {
	e := newEnv(t)
	id, _ := e.recipeWithPicture(t)
	o, err := e.svc.Offer(e.own, id, pngBytes(t, 60, 40))
	if err != nil {
		t.Fatal(err)
	}
	path := picture.OfferedSrc(id, o.ID)

	w := e.get(e.own, path)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("own offered picture = %d %q, want 200 image/webp", w.Code, w.Header().Get("Content-Type"))
	}
	if w := e.get(e.foreign, path); w.Code != http.StatusNotFound {
		t.Errorf("another household's offered picture = %d, want 404", w.Code)
	}
	if w := e.get(e.own, picture.OfferedSrc(id, "AAAAAAAAAAAAAAAAAAAAAA")); w.Code != http.StatusNotFound {
		t.Errorf("an ID with nothing offered = %d, want 404", w.Code)
	}
}
