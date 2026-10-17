package shopping

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	domain "github.com/kaecyra/gobbler/internal/shopping"
)

// sharedList is a list of pancakes and waffles, which both need flour, plus a
// hand-added flour and a hand-added free-text item.
func (f *fixture) sharedList(t *testing.T) int64 {
	t.Helper()
	list := f.newList("Week")
	f.addRecipe(list, f.cakes, "1")
	f.addRecipe(list, f.waffl, "1")
	rec := f.do(http.MethodPost, "/"+id(list)+"/manual", url.Values{"text": {"Zz napkins"}})
	wantStatus(t, rec, http.StatusSeeOther)
	return list
}

// groupBody returns the body between a group's heading and the next group.
func groupBody(t *testing.T, body, name string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, `data-testid="group-name">`+name+"<")
	if !ok {
		t.Fatalf("no group %q in:\n%s", name, body)
	}
	before, _, _ := strings.Cut(rest, `data-testid="recipe-group"`)
	return before
}

func TestByAisleIsTheDefaultAndOffersTheToggle(t *testing.T) {
	f := newFixture(t)
	list := f.sharedList(t)
	body := f.do(http.MethodGet, "/"+id(list), nil).Body.String()
	for _, want := range []string{`data-testid="aisle"`, `data-testid="group-toggle"`, `?group=recipe`} {
		if !strings.Contains(body, want) {
			t.Errorf("default page lacks %q", want)
		}
	}
	if strings.Contains(body, `data-testid="recipe-group"`) {
		t.Error("default page is grouped by recipe")
	}
	// An explicit aisle is the default too.
	if got := f.do(http.MethodGet, "/"+id(list)+"?group=aisle", nil).Body.String(); !strings.Contains(got, `data-testid="aisle"`) {
		t.Error("?group=aisle is not grouped by aisle")
	}
}

func TestByRecipeShowsEachRecipesOwnShare(t *testing.T) {
	f := newFixture(t)
	list := f.sharedList(t)
	rec := f.do(http.MethodGet, "/"+id(list)+"?group=recipe", nil)
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	pancakes := groupBody(t, body, "Zz Pancakes")
	if !strings.Contains(pancakes, "zz flour") || !strings.Contains(pancakes, "1 cup") {
		t.Errorf("pancakes group lacks its 1 cup of flour:\n%s", pancakes)
	}
	waffles := groupBody(t, body, "Zz Waffles")
	if !strings.Contains(waffles, "2 cup") || strings.Contains(waffles, "zz milk") {
		t.Errorf("waffles group should hold only its 2 cup of flour:\n%s", waffles)
	}
	if strings.Contains(body, `data-testid="aisle"`) {
		t.Error("by-recipe page still has aisle groups")
	}
	wantBody(t, rec, "Added by hand", "Zz napkins", `aria-current="true"`)
}

func TestInvalidGroupIsABadRequest(t *testing.T) {
	f := newFixture(t)
	list := f.sharedList(t)
	for _, path := range []string{"/" + id(list) + "?group=nope", "/" + id(list) + "/live?group=nope", "/" + id(list) + "/events?group=nope"} {
		wantStatus(t, f.do(http.MethodGet, path, nil), http.StatusBadRequest)
	}
}

func TestEditsKeepTheGroupingTheyCameFrom(t *testing.T) {
	f := newFixture(t)
	list := f.sharedList(t)
	body := f.do(http.MethodGet, "/"+id(list)+"?group=recipe", nil).Body.String()
	for _, want := range []string{
		`action="/shopping/` + id(list) + `/regenerate?group=recipe"`,
		`action="/shopping/` + id(list) + `/manual?group=recipe"`,
		`action="/shopping/` + id(list) + `/recipes?group=recipe"`,
		`href="/shopping/` + id(list) + `/live?group=recipe"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("by-recipe page lacks %q", want)
		}
	}
	rec := f.do(http.MethodPost, "/"+id(list)+"/regenerate?group=recipe", url.Values{})
	wantStatus(t, rec, http.StatusSeeOther)
	if loc := rec.Header().Get("Location"); loc != "/shopping/"+id(list)+"?group=recipe" {
		t.Errorf("Location after an edit = %q", loc)
	}
	rec = f.do(http.MethodPost, "/"+id(list)+"/check?group=recipe", checkForm("i:"+id(f.flour), "1"))
	wantStatus(t, rec, http.StatusSeeOther)
	if loc := rec.Header().Get("Location"); loc != "/shopping/"+id(list)+"/live?group=recipe" {
		t.Errorf("Location after a check = %q", loc)
	}
	rec = f.do(http.MethodPost, "/"+id(list)+"/regenerate", url.Values{})
	if loc := rec.Header().Get("Location"); loc != "/shopping/"+id(list) {
		t.Errorf("an edit from the default view redirected to %q", loc)
	}
}

func TestCheckingAnItemChecksItInEveryGroup(t *testing.T) {
	f := newFixture(t)
	list := f.sharedList(t)
	if err := f.shop.SetChecked(f.ctx, list, domain.ItemKey("i:"+id(f.flour)), true); err != nil {
		t.Fatal(err)
	}
	body := f.do(http.MethodGet, "/"+id(list)+"/live?group=recipe", nil).Body.String()
	for _, g := range []string{"Zz Pancakes", "Zz Waffles"} {
		if !strings.Contains(groupBody(t, body, g), `aria-checked="true"`) {
			t.Errorf("flour is not checked under %s", g)
		}
	}
}

func TestEventStreamFollowsTheRequestedGrouping(t *testing.T) {
	f := newFixture(t)
	list := f.sharedList(t)
	srv := f.streamServer(0)

	byRecipe := openStreamAt(t, srv.URL+Pattern+"/"+id(list)+"/events?group=recipe")
	first := byRecipe.next(5 * time.Second)
	if !strings.Contains(first, `data-testid="recipe-group"`) {
		t.Fatalf("by-recipe stream sent no recipe groups: %q", first)
	}
	if err := f.shop.SetChecked(f.ctx, list, domain.ItemKey("i:"+id(f.flour)), true); err != nil {
		t.Fatal(err)
	}
	if got := byRecipe.next(5 * time.Second); !strings.Contains(got, `recipe-group`) || !strings.Contains(got, `data-checked="true"`) {
		t.Fatalf("by-recipe update = %q", got)
	}

	byAisle := openStream(t, srv.URL, list)
	if got := byAisle.next(5 * time.Second); strings.Contains(got, `recipe-group`) || !strings.Contains(got, `data-testid="aisle"`) {
		t.Fatalf("default stream = %q", got)
	}
}

func TestLivePageStreamsInItsOwnGrouping(t *testing.T) {
	f := newFixture(t)
	list := f.sharedList(t)
	body := f.do(http.MethodGet, "/"+id(list)+"/live?group=recipe", nil).Body.String()
	if !strings.Contains(body, `data-events="/shopping/`+id(list)+`/events?group=recipe"`) {
		t.Errorf("by-recipe phone view follows the default stream:\n%s", body)
	}
}

func TestPostWithABadGroupChangesNothing(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.addRecipe(list, f.waffl, "1")
	key := "i:" + id(f.flour)
	posts := []struct {
		path string
		form url.Values
	}{
		{"/recipes", url.Values{"recipe": {id(f.cakes)}, "multiplier": {"1"}}},
		{"/manual", url.Values{"text": {"Zz napkins"}}},
		{"/optional", url.Values{"optional": {"1"}}},
		{"/check", checkForm(key, "1")},
	}
	for _, p := range posts {
		wantStatus(t, f.do(http.MethodPost, "/"+id(list)+p.path+"?group=bogus", p.form), http.StatusBadRequest)
	}
	view, err := f.shop.Load(f.ctx, list)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Entries) != 1 || len(view.Manual) != 0 || view.IncludeOptional {
		t.Errorf("a rejected request changed the list: %d entries, %d manual, optional %v", len(view.Entries), len(view.Manual), view.IncludeOptional)
	}
	for _, a := range view.Aisles {
		for _, it := range a.Items {
			if it.Checked {
				t.Errorf("%s was checked by a rejected request", it.Name)
			}
		}
	}
}
