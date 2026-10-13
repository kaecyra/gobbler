package catalog

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/household"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	domain "github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

type fixture struct {
	t   *testing.T
	d   *sql.DB
	cat *domain.Store
	rec *store.Store
	srv http.Handler
}

// newFixture wires the page over a real SQLite file, with the store's merge
// hooks and usage counters registered as the binary does.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	cat, rec := domain.New(d), store.New(d)
	usage := domain.NewUsage(cat)
	rec.RegisterMergeHooks(cat)
	rec.RegisterUsageCounters(usage)
	root := chi.NewRouter()
	// The membership middleware's work: the page acts in the dev household.
	h := devHousehold(t, d)
	root.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(household.WithContext(r.Context(), h)))
		})
	})
	root.Mount(Pattern, Routes(Deps{Catalog: cat, Usage: usage}))
	return &fixture{t: t, d: d, cat: cat, rec: rec, srv: root}
}

func (f *fixture) ingredient(name, aisle string, reviewed bool, aliases ...string) domain.Ingredient {
	f.t.Helper()
	in, err := f.cat.CreateIngredient(identityContext(f.t, f.d), domain.Ingredient{Name: name, Aisle: aisle, Aliases: aliases, Reviewed: reviewed})
	if err != nil {
		f.t.Fatal(err)
	}
	return in
}

// recipeUsing stores a recipe with one line per given ingredient id.
func (f *fixture) recipeUsing(name string, ids ...int64) {
	f.t.Helper()
	var lines []recipe.IngredientLine
	for _, id := range ids {
		lines = append(lines, recipe.IngredientLine{Ingredient: recipe.IngredientID(id), Rule: quantity.RuleLinear, Raw: "one thing"})
	}
	r := recipe.Recipe{Name: name, Components: []recipe.Component{{Lines: lines, Steps: []recipe.Step{{ID: "s1", Text: "Do it."}}}}}
	if _, err := f.rec.Create(identityContext(f.t, f.d), r); err != nil {
		f.t.Fatal(err)
	}
}

func identityContext(t *testing.T, d *sql.DB) context.Context {
	t.Helper()
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:0", DataDir: t.TempDir(), DevAuthBypass: true}})
	if err != nil {
		t.Fatal(err)
	}
	var got context.Context
	a.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Context() })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if got == nil {
		t.Fatal("auth middleware did not pass the request on")
	}
	return household.WithContext(got, devHousehold(t, d))
}

func (f *fixture) do(method, target string, form url.Values) *httptest.ResponseRecorder {
	f.t.Helper()
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	w := httptest.NewRecorder()
	f.srv.ServeHTTP(w, req)
	return w
}

func (f *fixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.d.QueryRow(query, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func wantStatus(t *testing.T, w *httptest.ResponseRecorder, status int) string {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d\n%s", w.Code, status, w.Body)
	}
	return w.Body.String()
}

func wantContains(t *testing.T, body string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(body, p) {
			t.Errorf("response lacks %q\n%s", p, body)
		}
	}
}

func idStr(in domain.Ingredient) string { return strconv.FormatInt(in.ID, 10) }

func TestListShowsUnreviewedFirstAndFiltersAll(t *testing.T) {
	f := newFixture(t)
	f.ingredient("zz proposed dill", "produce", false, "zz dill weed")
	f.ingredient("zz checked mint", "produce", true)

	body := wantStatus(t, f.do("GET", "/catalog/", nil), 200)
	unrev := strings.Index(body, `data-testid="unreviewed"`)
	all := strings.Index(body, `data-testid="all"`)
	if unrev < 0 || all < unrev {
		t.Fatalf("unreviewed section must come before the full list\n%s", body)
	}
	section := body[unrev:all]
	if !strings.Contains(section, "zz proposed dill") || strings.Contains(section, "zz checked mint") {
		t.Errorf("unreviewed section = %s", section)
	}

	// The filter matches aliases and narrows both lists.
	body = wantStatus(t, f.do("GET", "/catalog/?q=DILL+weed", nil), 200)
	wantContains(t, body, "zz proposed dill")
	if strings.Contains(body, "zz checked mint") {
		t.Error("filter kept a non-matching ingredient")
	}
}

func TestListEmptyUnreviewedShowsEmptyState(t *testing.T) {
	f := newFixture(t)
	f.ingredient("zz only reviewed", "produce", true)
	// The seeded catalog may hold unreviewed entries of its own; clear them.
	if _, err := f.d.Exec(`UPDATE ingredients SET reviewed = 1`); err != nil {
		t.Fatal(err)
	}
	body := wantStatus(t, f.do("GET", "/catalog/", nil), 200)
	wantContains(t, body, `data-testid="unreviewed-empty"`, "Nothing to review")
}

func TestEditSavesEveryFieldAndReviewedLeavesTheUnreviewedList(t *testing.T) {
	f := newFixture(t)
	in := f.ingredient("zz edit me", "produce", false)

	body := wantStatus(t, f.do("GET", "/catalog/"+idStr(in), nil), 200)
	wantContains(t, body, `value="zz edit me"`)

	w := f.do("POST", "/catalog/"+idStr(in), url.Values{
		"name": {"zz Edited"}, "aliases": {"zz edits\nzz editing\n"}, "aisle": {"Bakery"},
		"density": {"0.53"}, "usda_id": {"123456"}, "reviewed": {"1"},
	})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d\n%s", w.Code, w.Body)
	}
	got, err := f.cat.GetIngredient(identityContext(f.t, f.d), in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "zz edited" || got.Aisle != "bakery" || !got.Reviewed || len(got.Aliases) != 2 ||
		got.DensityGPerML == nil || *got.DensityGPerML != 0.53 || got.USDAID == nil || *got.USDAID != 123456 {
		t.Errorf("saved = %+v", got)
	}
	list := wantStatus(t, f.do("GET", "/catalog/", nil), 200)
	unrev := list[strings.Index(list, `data-testid="unreviewed"`):strings.Index(list, `data-testid="all"`)]
	if strings.Contains(unrev, "zz edited") {
		t.Error("reviewed ingredient is still listed as unreviewed")
	}
}

func TestMarkReviewedChangesOnlyTheFlag(t *testing.T) {
	f := newFixture(t)
	in := f.ingredient("zz flag only", "produce", false, "zz flags")
	if w := f.do("POST", "/catalog/"+idStr(in)+"/reviewed", url.Values{}); w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", w.Code)
	}
	got, err := f.cat.GetIngredient(identityContext(f.t, f.d), in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Reviewed || got.Name != "zz flag only" || len(got.Aliases) != 1 {
		t.Errorf("got %+v", got)
	}
}

func TestEditRejectsBadInputInlineAndSavesNothing(t *testing.T) {
	f := newFixture(t)
	in := f.ingredient("zz keep me", "produce", false)
	f.ingredient("zz other", "produce", true, "zz taken alias")
	withUSDA := f.ingredient("zz usda owner", "produce", true)
	withUSDA.USDAID = new(int64)
	*withUSDA.USDAID = 424242
	if err := f.cat.UpdateIngredient(identityContext(f.t, f.d), withUSDA); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		form   url.Values
		status int
		want   string
	}{
		{"empty name", url.Values{"name": {"  "}, "aisle": {"produce"}}, 400, `data-testid="name-error"`},
		{"density text", url.Values{"name": {"zz keep me"}, "aisle": {"produce"}, "density": {"heavy"}}, 400, `data-testid="density-error"`},
		{"density zero", url.Values{"name": {"zz keep me"}, "aisle": {"produce"}, "density": {"0"}}, 400, `data-testid="density-error"`},
		{"usda text", url.Values{"name": {"zz keep me"}, "aisle": {"produce"}, "usda_id": {"x"}}, 400, `data-testid="usda-error"`},
		{"qualifier in name", url.Values{"name": {"zz butter, melted"}, "aisle": {"produce"}}, 400, `data-testid="form-error"`},
		{"no aisle", url.Values{"name": {"zz keep me"}, "aisle": {""}}, 400, `data-testid="aisle-error"`},
		{"usda id taken", url.Values{"name": {"zz keep me"}, "aisle": {"produce"}, "usda_id": {"424242"}}, 409, `data-testid="usda-error"`},
		{"alias collision", url.Values{"name": {"zz keep me"}, "aisle": {"produce"}, "aliases": {"zz taken alias"}}, 409, "already used by"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := wantStatus(t, f.do("POST", "/catalog/"+idStr(in), c.form), c.status)
			wantContains(t, body, c.want)
			got, err := f.cat.GetIngredient(identityContext(f.t, f.d), in.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != "zz keep me" || len(got.Aliases) != 0 || got.DensityGPerML != nil {
				t.Errorf("rejected save changed the ingredient: %+v", got)
			}
		})
	}

}

func TestEditOfMissingIngredientIsNotFound(t *testing.T) {
	f := newFixture(t)
	wantStatus(t, f.do("GET", "/catalog/987654", nil), 404)
	wantStatus(t, f.do("POST", "/catalog/987654", url.Values{"name": {"zz x"}, "aisle": {"a"}}), 404)
	wantStatus(t, f.do("GET", "/catalog/abc", nil), 400)
}

func TestMergeConfirmationStatesTheUseCountThenRepointsEveryUse(t *testing.T) {
	f := newFixture(t)
	src := f.ingredient("zz merge source", "produce", false)
	dst := f.ingredient("zz merge target", "produce", true)
	f.recipeUsing("zz one", src.ID, src.ID)
	f.recipeUsing("zz two", src.ID, dst.ID)

	chooser := wantStatus(t, f.do("GET", "/catalog/merge?source="+idStr(src), nil), 200)
	wantContains(t, chooser, `data-testid="merge-chooser"`, "zz merge target")
	if strings.Contains(chooser, `>zz merge source</option>`) {
		t.Error("the source is offered as its own target")
	}

	confirm := wantStatus(t, f.do("GET", "/catalog/merge?source="+idStr(src)+"&target="+idStr(dst), nil), 200)
	wantContains(t, confirm, `data-testid="merge-uses">3</span>`)

	w := f.do("POST", "/catalog/merge", url.Values{"source": {idStr(src)}, "target": {idStr(dst)}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d\n%s", w.Code, w.Body)
	}
	if n := f.count(`SELECT COUNT(*) FROM ingredient_lines WHERE ingredient_id = ?`, src.ID); n != 0 {
		t.Errorf("%d lines still point at the source", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM ingredient_lines WHERE ingredient_id = ?`, dst.ID); n != 4 {
		t.Errorf("%d lines point at the target, want 4", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM ingredients WHERE id = ?`, src.ID); n != 0 {
		t.Error("the source ingredient still exists")
	}
	if loc := w.Header().Get("Location"); loc != "/catalog/"+idStr(dst) {
		t.Errorf("redirect = %q", loc)
	}
}

func TestMergeFailureLeavesEverythingUnchanged(t *testing.T) {
	f := newFixture(t)
	src := f.ingredient("zz fail source", "produce", false, "zz fail alias")
	dst := f.ingredient("zz fail target", "produce", true)
	f.recipeUsing("zz fail recipe", src.ID)
	// A later hook failing must roll back the lines the earlier hook moved.
	f.cat.OnIngredientMerge(func(context.Context, *sql.Tx, int64, int64) error { return context.DeadlineExceeded })

	w := f.do("POST", "/catalog/merge", url.Values{"source": {idStr(src)}, "target": {idStr(dst)}})
	wantStatus(t, w, 500)
	if n := f.count(`SELECT COUNT(*) FROM ingredient_lines WHERE ingredient_id = ?`, src.ID); n != 1 {
		t.Errorf("source lines = %d, want the 1 original", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM ingredients WHERE id IN (?, ?)`, src.ID, dst.ID); n != 2 {
		t.Errorf("ingredients left = %d, want both", n)
	}
	// The alias moves that ran before the failing hook were rolled back too:
	// the source keeps its own alias, the target gained none.
	if n := f.count(`SELECT COUNT(*) FROM ingredient_aliases WHERE ingredient_id = ?`, src.ID); n != 1 {
		t.Errorf("source aliases = %d, want the 1 original", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM ingredient_aliases WHERE ingredient_id = ?`, dst.ID); n != 0 {
		t.Errorf("target aliases = %d, want none gained", n)
	}
}

func TestMergeRejectsSelfAndMissing(t *testing.T) {
	f := newFixture(t)
	a := f.ingredient("zz self", "produce", false)

	for _, w := range []*httptest.ResponseRecorder{
		f.do("GET", "/catalog/merge?source="+idStr(a)+"&target="+idStr(a), nil),
		f.do("POST", "/catalog/merge", url.Values{"source": {idStr(a)}, "target": {idStr(a)}}),
	} {
		body := wantStatus(t, w, 400)
		wantContains(t, body, "cannot be merged into itself")
	}
	wantStatus(t, f.do("GET", "/catalog/merge?source="+idStr(a)+"&target=987654", nil), 404)
	wantStatus(t, f.do("POST", "/catalog/merge", url.Values{"source": {idStr(a)}, "target": {"987654"}}), 404)
	wantStatus(t, f.do("POST", "/catalog/merge", url.Values{"source": {"987654"}, "target": {idStr(a)}}), 404)
	if n := f.count(`SELECT COUNT(*) FROM ingredients WHERE id = ?`, a.ID); n != 1 {
		t.Error("a rejected merge removed the ingredient")
	}
}

func TestPageCreatesNoIngredients(t *testing.T) {
	f := newFixture(t)
	before := f.count(`SELECT COUNT(*) FROM ingredients`)
	f.do("POST", "/catalog/", url.Values{"name": {"zz sneaky"}, "aisle": {"x"}})
	f.do("POST", "/catalog/0", url.Values{"name": {"zz sneaky"}, "aisle": {"x"}})
	if after := f.count(`SELECT COUNT(*) FROM ingredients`); after != before {
		t.Errorf("ingredients %d -> %d", before, after)
	}
}

func TestFilter(t *testing.T) {
	in := []domain.Ingredient{
		{ID: 1, Name: "olive oil", Aisle: "oils", Aliases: []string{"evoo"}},
		{ID: 2, Name: "flour", Aisle: "baking"},
	}
	for q, want := range map[string][]int64{"": {1, 2}, "OIL": {1}, "evoo": {1}, "baking": {2}, "zzz": nil} {
		var got []int64
		for _, i := range filter(in, q) {
			got = append(got, i.ID)
		}
		if len(got) != len(want) {
			t.Errorf("filter(%q) = %v, want %v", q, got, want)
		}
	}
}

// devHousehold returns the household the membership middleware resolves for
// the dev identity, creating it in d the way the dev bypass does.
func devHousehold(t *testing.T, d *sql.DB) household.Household {
	t.Helper()
	hs := household.New(d)
	if err := hs.Seed(context.Background(), "Dev household", auth.DevEmail); err != nil {
		t.Fatalf("seed household: %v", err)
	}
	h, err := hs.ForEmail(context.Background(), auth.DevEmail)
	if err != nil {
		t.Fatalf("household of %s: %v", auth.DevEmail, err)
	}
	return h
}
