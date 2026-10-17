package shopping

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
)

func (f *fixture) quickAdd(recipeID int64, mult string) *httptest.ResponseRecorder {
	f.t.Helper()
	form := url.Values{"recipe": {id(recipeID)}}
	if mult != "" {
		form.Set("multiplier", mult)
	}
	return f.do(http.MethodPost, "/add", form)
}

func TestQuickAddPutsTheRecipeOnTheNewestListAtTheMultiplier(t *testing.T) {
	f := newFixture(t)
	f.newList("Old")
	newest := f.newList("Week")
	rec := f.quickAdd(f.cakes, "2")
	wantStatus(t, rec, http.StatusSeeOther)
	if got, want := rec.Header().Get("Location"), fmt.Sprintf("/shopping/%d", newest); got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
	view, err := f.shop.Load(f.ctx, newest)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Entries) != 1 || view.Entries[0].RecipeID != f.cakes || !view.Entries[0].Multiplier.Equal(quantity.Int(2)) {
		t.Fatalf("entries = %+v, want pancakes at 2", view.Entries)
	}
}

func TestQuickAddAcceptsAMixedNumberMultiplier(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	wantStatus(t, f.quickAdd(f.cakes, "1 1/2"), http.StatusSeeOther)
	view, err := f.shop.Load(f.ctx, list)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Entries) != 1 || view.Entries[0].Multiplier.String() != "1 1/2" {
		t.Fatalf("entries = %+v, want multiplier 1 1/2", view.Entries)
	}
}

func TestQuickAddOfARecipeAlreadyOnTheListUpdatesItsMultiplier(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.addRecipe(list, f.cakes, "1")
	wantStatus(t, f.quickAdd(f.cakes, "3"), http.StatusSeeOther)
	view, err := f.shop.Load(f.ctx, list)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Entries) != 1 || !view.Entries[0].Multiplier.Equal(quantity.Int(3)) {
		t.Fatalf("entries = %+v, want one pancakes entry at 3", view.Entries)
	}
}

func TestQuickAddWithNoListSendsYouToChooseOne(t *testing.T) {
	f := newFixture(t)
	rec := f.quickAdd(f.cakes, "1")
	wantStatus(t, rec, http.StatusSeeOther)
	if got := rec.Header().Get("Location"); got != Pattern {
		t.Fatalf("Location = %q, want %q", got, Pattern)
	}
}

func TestQuickAddRejectsBadInput(t *testing.T) {
	f := newFixture(t)
	f.newList("Week")
	for name, form := range map[string]url.Values{
		"missing recipe": {"multiplier": {"1"}},
		"zero":           {"recipe": {id(f.cakes)}, "multiplier": {"0"}},
		"text":           {"recipe": {id(f.cakes)}, "multiplier": {"lots"}},
	} {
		if rec := f.do(http.MethodPost, "/add", form); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400; body %s", name, rec.Code, rec.Body)
		}
	}
}

func TestQuickAddOfAnUnknownRecipeIs404(t *testing.T) {
	f := newFixture(t)
	f.newList("Week")
	if rec := f.quickAdd(999999, "1"); rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404; body %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
}

// A recipe of another household is "not found" even when the caller has a list
// of its own, and its list is left alone (ADR-00018).
func TestQuickAddOfAnotherHouseholdsRecipeIs404(t *testing.T) {
	f := newFixture(t)
	b := f.asOtherHousehold()
	if rec := serve(b, http.MethodPost, "/", url.Values{"name": {"B week"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create list as B: %d %s", rec.Code, rec.Body)
	}
	rec := serve(b, http.MethodPost, "/add", url.Values{"recipe": {id(f.cakes)}, "multiplier": {"1"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404; body %s", rec.Code, rec.Body)
	}
}
