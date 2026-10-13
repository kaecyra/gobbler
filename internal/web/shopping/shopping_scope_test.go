package shopping

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// asOtherHousehold serves the same handlers to a member of a second household.
func (f *fixture) asOtherHousehold() http.Handler {
	f.t.Helper()
	h, err := household.New(f.d).Create(f.ctx, "Other household", auth.DevOtherEmail)
	if err != nil {
		f.t.Fatal(err)
	}
	root := chi.NewRouter()
	root.Use(asMember(h))
	root.Mount(Pattern, f.h.routes())
	return root
}

func serve(srv http.Handler, method, path string, form url.Values) *httptest.ResponseRecorder {
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, Pattern+path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, Pattern+path, nil)
	}
	// A route that wrongly lets the caller in would stream the list for as long
	// as the request lives; bound it so that shows up as a status, not a hang.
	ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
	defer cancel()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

// Household B cannot read, build, check, export or subscribe to household A's
// list, and cannot see A's pantry or list index (ADR-00018). The list is
// "not found", never "forbidden", so its existence is not disclosed.
func TestOtherHouseholdCannotReachAListOfAnother(t *testing.T) {
	f := newFixture(t)
	b := f.asOtherHousehold()
	list := f.newList("Secret week")
	f.addRecipe(list, f.cakes, "1")
	if err := f.shop.AddToPantry(f.ctx, recipe.IngredientID(f.salt)); err != nil {
		t.Fatal(err)
	}

	base := fmt.Sprintf("/%d", list)
	for _, path := range []string{base, base + "/live", base + "/events", base + "/export.txt"} {
		if rec := serve(b, http.MethodGet, path, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s as the other household = %d, want 404; body %s", path, rec.Code, rec.Body)
		}
	}
	form := url.Values{"recipe": {id(f.cakes)}, "multiplier": {"1"}, "text": {"x"}, "key": {"t:x"}, "checked": {"1"}, "ingredient": {id(f.salt)}}
	for _, path := range []string{base + "/recipes", base + "/manual", base + "/check", base + "/regenerate", base + "/addback", base + "/optional"} {
		if rec := serve(b, http.MethodPost, path, form); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s as the other household = %d, want 404; body %s", path, rec.Code, rec.Body)
		}
	}
	if n := f.h.hub.count(); n != 0 {
		t.Errorf("%d subscriptions left by the other household", n)
	}

	index := serve(b, http.MethodGet, "/", nil)
	wantStatus(t, index, http.StatusOK)
	if strings.Contains(index.Body.String(), "Secret week") {
		t.Error("the other household's index lists A's list")
	}
	pantry := serve(b, http.MethodGet, "/pantry", nil)
	wantStatus(t, pantry, http.StatusOK)
	wantBody(t, pantry, "The pantry is empty.")

	// A's list is as it was.
	wantBody(t, f.do(http.MethodGet, base, nil), "Secret week", "Zz Pancakes")
}
