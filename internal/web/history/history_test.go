package history

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/household"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// env is a real store over a throwaway database, and a router with the
// history routes mounted the way internal/web mounts them. The store is the
// boundary these handlers exist to expose, so it is not faked.
type env struct {
	t      *testing.T
	ctx    context.Context
	store  *store.Store
	router chi.Router
	flour  int64
	pan    int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}})
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	var authed context.Context
	a.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { authed = r.Context() })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if authed == nil {
		t.Fatal("auth middleware did not pass the request on")
	}

	authed = household.WithContext(authed, devHousehold(t, d))
	s, cat := store.New(d), catalog.New(d)
	flour, err := cat.CreateIngredient(authed, catalog.Ingredient{Name: "test flour", Aisle: "baking"})
	if err != nil {
		t.Fatalf("create ingredient: %v", err)
	}
	pan, err := cat.CreateEquipment(authed, catalog.Equipment{Name: "test pan"})
	if err != nil {
		t.Fatalf("create equipment: %v", err)
	}

	deps := Deps{Recipes: s, Catalog: cat}
	r := chi.NewRouter()
	// Stand in for the auth middleware, which gives every request an identity.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(withIdentity{Context: req.Context(), identity: authed}))
		})
	})
	r.Mount(Pattern, Routes(deps))
	r.Mount(ClonePattern, CloneRoutes(deps))
	return &env{t: t, ctx: authed, store: s, router: r, flour: flour.ID, pan: pan.ID}
}

// withIdentity answers context values from the request first and falls back to
// the context the auth middleware produced, so chi's route context and the
// verified identity are both visible.
type withIdentity struct {
	context.Context
	identity context.Context
}

func (w withIdentity) Value(key any) any {
	if v := w.Context.Value(key); v != nil {
		return v
	}
	return w.identity.Value(key)
}

func (e *env) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	e.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(prev)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e *env) get(path string) (int, string) {
	e.t.Helper()
	rec := e.do(http.MethodGet, path, nil)
	return rec.Code, rec.Body.String()
}

func rat(t *testing.T, s string) quantity.Rat {
	t.Helper()
	r, err := quantity.ParseRat(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// doc is a recipe that uses every part of the document: two components, an
// ingredient with a unit and qualifiers, equipment tied to a step, a step
// dependency, special times.
func (e *env) doc(name, flourQty string) recipe.Recipe {
	return recipe.Recipe{
		Name:        name,
		Description: "A test recipe.",
		Author:      "Tester",
		Servings:    quantity.Exact(rat(e.t, "4")),
		Yield:       recipe.Yield{Qty: quantity.Exact(rat(e.t, "1")), Unit: "loaf"},
		Tags:        []string{"bread"},
		Times: recipe.Times{
			Prep: 20 * time.Minute, Cook: 45 * time.Minute,
			Special: []recipe.SpecialTime{{Label: recipe.LabelRise, Duration: time.Hour}},
		},
		Components: []recipe.Component{
			{
				Name: "Dough",
				Lines: []recipe.IngredientLine{{
					Amount:     quantity.Amount{Qty: quantity.Exact(rat(e.t, flourQty)), Unit: quantity.MustUnit("cup")},
					Ingredient: recipe.IngredientID(e.flour),
					Qualifiers: []recipe.Qualifier{{Kind: recipe.QualState, Text: "sifted"}},
					Rule:       quantity.RuleLinear,
					Raw:        flourQty + " cups flour, sifted",
				}},
				Steps: []recipe.Step{
					{ID: "mix", Text: "Mix the dough.", Duration: 10 * time.Minute},
					{ID: "rise", Text: "Let it rise.", Duration: time.Hour, Passive: true, DependsOn: []recipe.StepID{"mix"}},
				},
			},
			{
				Name:  "Finish",
				Steps: []recipe.Step{{ID: "bake", Text: "Bake it.", Duration: 45 * time.Minute, DependsOn: []recipe.StepID{"rise"}}},
			},
		},
		Equipment: []recipe.EquipmentLink{{Equipment: recipe.EquipmentID(e.pan), Steps: []recipe.StepID{"bake"}}},
	}
}

func (e *env) create(r recipe.Recipe) int64 {
	e.t.Helper()
	id, err := e.store.Create(e.ctx, r)
	if err != nil {
		e.t.Fatalf("create recipe: %v", err)
	}
	return id
}

func (e *env) update(id int64, r recipe.Recipe) {
	e.t.Helper()
	if err := e.store.Update(e.ctx, id, r); err != nil {
		e.t.Fatalf("update recipe %d: %v", id, err)
	}
}

func (e *env) current(id int64) recipe.Recipe {
	e.t.Helper()
	r, err := e.store.Get(e.ctx, id)
	if err != nil {
		e.t.Fatalf("get recipe %d: %v", id, err)
	}
	return r
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestListWithoutRevisionsShowsEmptyState(t *testing.T) {
	e := newEnv(t)
	id := e.create(e.doc("Plain Loaf", "3"))

	code, body := e.get("/recipes/" + itoa(id) + "/history")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !strings.Contains(body, `data-testid="empty"`) || !strings.Contains(body, "has not been edited") {
		t.Errorf("no empty state in:\n%s", body)
	}
	if strings.Contains(body, `data-testid="revision"`) {
		t.Errorf("an unedited recipe lists a revision:\n%s", body)
	}
	if !strings.Contains(body, "(current)") {
		t.Errorf("the current version is not shown:\n%s", body)
	}
}

func TestListShowsRevisionsNewestFirstWithAuthorAndTime(t *testing.T) {
	e := newEnv(t)
	id := e.create(e.doc("Loaf v1", "3"))
	e.update(id, e.doc("Loaf v2", "4"))
	e.update(id, e.doc("Loaf v3", "5"))

	code, body := e.get("/recipes/" + itoa(id) + "/history")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	one := strings.Index(body, "/history/1")
	two := strings.Index(body, "/history/2")
	three := strings.Index(body, "/history/3")
	if one < 0 || two < 0 || three < 0 || three >= two || two >= one {
		t.Errorf("versions not listed newest first (positions 3=%d 2=%d 1=%d):\n%s", three, two, one, body)
	}
	if !strings.Contains(body, "by dev@localhost") || !strings.Contains(body, " UTC") {
		t.Errorf("no timestamp or author in:\n%s", body)
	}
	if strings.Count(body, `data-testid="revision"`) != 2 {
		t.Errorf("want the two replaced versions listed:\n%s", body)
	}
}

func TestViewShowsTheWholeDocumentOfThatVersion(t *testing.T) {
	e := newEnv(t)
	id := e.create(e.doc("Loaf v1", "3"))
	e.update(id, e.doc("Loaf v2", "4"))

	code, body := e.get("/recipes/" + itoa(id) + "/history/1")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	for _, want := range []string{
		"Loaf v1", "3 cup test flour, sifted", "Mix the dough.", "Let it rise.", "Bake it.",
		"after step 1", "after step 2", "test pan", "(steps 3)", "1 h", "Dough", "Finish", "bread", "Makes", "loaf",
		"an earlier version", `action="/recipes/` + itoa(id) + `/history/1/restore"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("version page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Loaf v2") || strings.Contains(body, "4 cup") {
		t.Errorf("version 1 shows content of version 2:\n%s", body)
	}
}

func TestViewOfCurrentVersionOffersNoRestore(t *testing.T) {
	e := newEnv(t)
	id := e.create(e.doc("Loaf v1", "3"))
	e.update(id, e.doc("Loaf v2", "4"))

	code, body := e.get("/recipes/" + itoa(id) + "/history/2")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !strings.Contains(body, "the current version") || strings.Contains(body, "/restore") {
		t.Errorf("current version page is wrong:\n%s", body)
	}
}

func TestRestoreRoundTripEqualsTheOriginalInFull(t *testing.T) {
	e := newEnv(t)
	id := e.create(e.doc("Loaf v1", "3"))
	original := e.current(id)
	edited := e.doc("Loaf v2", "4")
	edited.Components[0].Steps[1].DependsOn = nil
	edited.Equipment = nil
	edited.Times.Special = nil
	e.update(id, edited)
	if reflect.DeepEqual(e.current(id), original) {
		t.Fatal("the edit changed nothing; the round trip proves nothing")
	}

	rec := e.do(http.MethodPost, "/recipes/"+itoa(id)+"/history/1/restore", url.Values{confirmField: {confirmValue}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/recipes/"+itoa(id)+"/history" {
		t.Fatalf("restore = %d to %q, want 303 to the history", rec.Code, rec.Header().Get("Location"))
	}
	if got := e.current(id); !reflect.DeepEqual(got, original) {
		t.Errorf("restored recipe differs from the original:\n got  %+v\n want %+v", got, original)
	}

	_, body := e.get("/recipes/" + itoa(id) + "/history")
	if got := strings.Count(body, `data-testid="revision"`); got != 2 {
		t.Errorf("history lists %d revisions after edit and restore, want 2:\n%s", got, body)
	}
	if !strings.Contains(body, "restore of version 1") {
		t.Errorf("the restore is not shown as a revision:\n%s", body)
	}
	// The restore replaced version 2 and kept it viewable.
	if code, body := e.get("/recipes/" + itoa(id) + "/history/2"); code != http.StatusOK || !strings.Contains(body, "Loaf v2") {
		t.Errorf("version 2 is no longer viewable after the restore: %d", code)
	}
}

func TestRestoreNeedsConfirmation(t *testing.T) {
	e := newEnv(t)
	id := e.create(e.doc("Loaf v1", "3"))
	e.update(id, e.doc("Loaf v2", "4"))
	before := e.current(id)

	rec := e.do(http.MethodPost, "/recipes/"+itoa(id)+"/history/1/restore", url.Values{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed restore = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "needs confirmation") {
		t.Errorf("message does not say what to do:\n%s", rec.Body.String())
	}
	if !reflect.DeepEqual(e.current(id), before) {
		t.Error("an unconfirmed restore changed the recipe")
	}
}

func TestRestoreOfTheCurrentVersionIsRefused(t *testing.T) {
	e := newEnv(t)
	id := e.create(e.doc("Loaf v1", "3"))
	e.update(id, e.doc("Loaf v2", "4"))

	rec := e.do(http.MethodPost, "/recipes/"+itoa(id)+"/history/2/restore", url.Values{confirmField: {confirmValue}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("restore of current = %d, want 400", rec.Code)
	}
	revs, err := e.store.ListRevisions(e.ctx, id)
	if err != nil || len(revs) != 1 {
		t.Errorf("revisions = %v, %v; want the single original one", revs, err)
	}
}

func TestUnknownIdsAre404(t *testing.T) {
	e := newEnv(t)
	id := e.create(e.doc("Loaf v1", "3"))
	e.update(id, e.doc("Loaf v2", "4"))
	confirm := url.Values{confirmField: {confirmValue}}

	for _, tc := range []struct{ method, path string }{
		{"GET", "/recipes/9999/history"},
		{"GET", "/recipes/abc/history"},
		{"GET", "/recipes/0/history"},
		{"GET", "/recipes/" + itoa(id) + "/history/9"},
		{"GET", "/recipes/" + itoa(id) + "/history/x"},
		{"GET", "/recipes/9999/history/1"},
		{"POST", "/recipes/9999/history/1/restore"},
		{"POST", "/recipes/" + itoa(id) + "/history/9/restore"},
		{"POST", "/recipes/9999/clone"},
	} {
		var form url.Values
		if tc.method == "POST" {
			form = confirm
		}
		if rec := e.do(tc.method, tc.path, form); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
}

func TestRestoreOfAnotherRecipesRevisionIsRejected(t *testing.T) {
	e := newEnv(t)
	a := e.create(e.doc("Recipe A", "3"))
	e.update(a, e.doc("Recipe A edited", "4"))
	e.update(a, e.doc("Recipe A edited again", "5"))
	b := e.create(e.doc("Recipe B", "9"))
	before := e.current(b)

	// Version 2 exists for A as a revision; B has only version 1, so the same
	// number must not reach A's document through B's URL.
	rec := e.do(http.MethodPost, "/recipes/"+itoa(b)+"/history/2/restore", url.Values{confirmField: {confirmValue}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("restore across recipes = %d, want 404", rec.Code)
	}
	if !reflect.DeepEqual(e.current(b), before) {
		t.Error("B changed")
	}
	if code, _ := e.get("/recipes/" + itoa(b) + "/history/2"); code != http.StatusNotFound {
		t.Errorf("viewing A's revision through B = %d, want 404", code)
	}
}

func TestCloneCreatesAnIndependentRecipeAndRecordsItsOrigin(t *testing.T) {
	e := newEnv(t)
	id := e.create(e.doc("Loaf", "3"))
	e.update(id, e.doc("Loaf edited", "4"))
	original := e.current(id)

	rec := e.do(http.MethodPost, "/recipes/"+itoa(id)+"/clone", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("clone = %d, want 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	cloneID, err := strconv.ParseInt(strings.TrimPrefix(loc, "/recipes/"), 10, 64)
	if err != nil || cloneID == id {
		t.Fatalf("redirect %q is not a new recipe", loc)
	}
	if got := e.current(cloneID); !reflect.DeepEqual(got, original) {
		t.Errorf("clone differs from the original:\n got  %+v\n want %+v", got, original)
	}
	info, err := e.store.Info(e.ctx, cloneID)
	if err != nil || info.ClonedFrom == nil || info.ClonedFrom.RecipeID != id || info.ClonedFrom.Version != 2 {
		t.Fatalf("origin = %+v (%v), want recipe %d version 2", info.ClonedFrom, err, id)
	}

	_, body := e.get("/recipes/" + itoa(cloneID) + "/history")
	if !strings.Contains(body, `data-testid="origin"`) || !strings.Contains(body, "/recipes/"+itoa(id)) {
		t.Errorf("clone's history does not show its origin:\n%s", body)
	}

	e.update(cloneID, e.doc("Clone edited", "7"))
	if !reflect.DeepEqual(e.current(id), original) {
		t.Error("editing the clone changed the original")
	}
}

// TestRoutesNeverTouchSourceSnapshots pins the whole route set: this package
// registers no route that could update or delete a snapshot, and a new route
// must be added here on purpose.
func TestRoutesNeverTouchSourceSnapshots(t *testing.T) {
	e := newEnv(t)
	var got []string
	err := chi.Walk(e.router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got = append(got, method+" "+route)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /recipes/{id}/history/",
		"GET /recipes/{id}/history/{version}",
		"POST /recipes/{id}/history/{version}/restore",
		"POST /recipes/{id}/clone/",
	}
	slices.Sort(got)
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("routes = %v, want %v", got, want)
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
