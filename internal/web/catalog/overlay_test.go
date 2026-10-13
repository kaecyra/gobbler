package catalog

import (
	"context"
	"net/url"
	"strings"
	"testing"

	domain "github.com/kaecyra/gobbler/internal/catalog"
)

// central returns the seeded central entry called name.
func (f *fixture) central(name string) domain.Ingredient {
	f.t.Helper()
	in, ok, err := f.cat.MatchIngredient(context.Background(), name)
	if err != nil || !ok || !in.Central {
		f.t.Fatalf("central %q = %+v ok=%v err=%v", name, in, ok, err)
	}
	return in
}

func TestCentralEntryEditPageLocksTheNameAndOffersNoMerge(t *testing.T) {
	f := newFixture(t)
	onion := f.central("onion")
	body := wantStatus(t, f.do("GET", "/catalog/"+idStr(onion), nil), 200)
	wantContains(t, body, `data-testid="central-note"`, `name="name" value="onion" required readonly`)
	for _, absent := range []string{"Merge this ingredient into another", `data-testid="suggest-form"`} {
		if strings.Contains(body, absent) {
			t.Errorf("central entry page offers %q", absent)
		}
	}
	// Its mark-reviewed route and merge chooser refuse a central source.
	wantStatus(t, f.do("GET", "/catalog/merge?source="+idStr(onion)+"&target="+idStr(f.ingredient("zz mine", "x", true)), nil), 400)
}

func TestHouseholdSetsItsOwnAliasAndAisleOnACentralEntry(t *testing.T) {
	f := newFixture(t)
	onion := f.central("onion")
	w := f.do("POST", "/catalog/"+idStr(onion), url.Values{
		"name": {"onion"}, "aliases": {"onions\nzz bulb"}, "aisle": {"zz veg"},
	})
	if w.Code != 303 {
		t.Fatalf("status = %d\n%s", w.Code, w.Body)
	}
	mine, _, _ := f.cat.MatchIngredient(identityContext(t, f.d), "zz bulb")
	if mine.ID != onion.ID || mine.Aisle != "zz veg" {
		t.Errorf("the household's alias resolves to %+v, want onion in its own aisle", mine)
	}
	// The central row itself is unchanged.
	if n := f.count(`SELECT COUNT(*) FROM ingredients WHERE id = ? AND aisle = ?`, onion.ID, onion.Aisle); n != 1 {
		t.Error("the central aisle changed")
	}
	// Renaming it is refused with a message, and changes nothing.
	w = f.do("POST", "/catalog/"+idStr(onion), url.Values{"name": {"zz renamed"}, "aliases": {""}, "aisle": {"x"}})
	wantContains(t, wantStatus(t, w, 400), "central")
	if n := f.count(`SELECT COUNT(*) FROM ingredients WHERE id = ? AND name = 'onion'`, onion.ID); n != 1 {
		t.Error("the central name changed")
	}
}

func TestSuggestForCentralFlagsOnlyTheHouseholdsOwnEntry(t *testing.T) {
	f := newFixture(t)
	mine := f.ingredient("zz ghee", "dairy", true)
	body := wantStatus(t, f.do("GET", "/catalog/"+idStr(mine), nil), 200)
	wantContains(t, body, `data-testid="suggest-form"`)

	if w := f.do("POST", "/catalog/"+idStr(mine)+"/suggest", url.Values{}); w.Code != 303 {
		t.Fatalf("suggest status = %d\n%s", w.Code, w.Body)
	}
	if n := f.count(`SELECT COUNT(*) FROM ingredients WHERE id = ? AND suggested_at IS NOT NULL`, mine.ID); n != 1 {
		t.Error("the entry was not flagged")
	}
	wantContains(t, wantStatus(t, f.do("GET", "/catalog/"+idStr(mine), nil), 200), `data-testid="unsuggest-form"`)

	if w := f.do("POST", "/catalog/"+idStr(mine)+"/unsuggest", url.Values{}); w.Code != 303 {
		t.Fatalf("unsuggest status = %d", w.Code)
	}
	if n := f.count(`SELECT COUNT(*) FROM ingredients WHERE suggested_at IS NOT NULL`); n != 0 {
		t.Error("the flag was not withdrawn")
	}
	wantStatus(t, f.do("POST", "/catalog/"+idStr(f.central("onion"))+"/suggest", url.Values{}), 404)
	wantStatus(t, f.do("POST", "/catalog/987654/suggest", url.Values{}), 404)
}

func TestListMarksCentralEntries(t *testing.T) {
	f := newFixture(t)
	f.ingredient("zz mine", "x", true)
	body := wantStatus(t, f.do("GET", "/catalog/?q=onion", nil), 200)
	wantContains(t, body, `data-testid="central-badge"`)
	body = wantStatus(t, f.do("GET", "/catalog/?q=zz+mine", nil), 200)
	if strings.Contains(body, `data-testid="central-badge"`) {
		t.Error("an overlay entry is marked central")
	}
}
