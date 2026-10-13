package recipeview

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
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

type fakeRecipes map[int64]recipe.Recipe

func (f fakeRecipes) Get(_ context.Context, id int64) (recipe.Recipe, error) {
	r, ok := f[id]
	if !ok {
		return recipe.Recipe{}, fmt.Errorf("recipe %d: %w", id, store.ErrNotFound)
	}
	return r, nil
}

// Info is the bookkeeping of a recipe that was never cloned.
func (f fakeRecipes) Info(_ context.Context, id int64) (store.Info, error) {
	if _, ok := f[id]; !ok {
		return store.Info{}, fmt.Errorf("recipe %d: %w", id, store.ErrNotFound)
	}
	return store.Info{ID: id, Version: 1}, nil
}

type fakeCatalog struct {
	ingredients map[int64]catalog.Ingredient
	equipment   map[int64]catalog.Equipment
}

func (f fakeCatalog) GetIngredient(_ context.Context, id int64) (catalog.Ingredient, error) {
	i, ok := f.ingredients[id]
	if !ok {
		return catalog.Ingredient{}, fmt.Errorf("ingredient %d: %w", id, catalog.ErrNotFound)
	}
	return i, nil
}

func (f fakeCatalog) GetEquipment(_ context.Context, id int64) (catalog.Equipment, error) {
	e, ok := f.equipment[id]
	if !ok {
		return catalog.Equipment{}, fmt.Errorf("equipment %d: %w", id, catalog.ErrNotFound)
	}
	return e, nil
}

func rat(t *testing.T, s string) quantity.Rat {
	t.Helper()
	r, err := quantity.ParseRat(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func amount(t *testing.T, qty, unit string) quantity.Amount {
	t.Helper()
	a := quantity.Amount{Qty: quantity.Exact(rat(t, qty))}
	if unit != "" {
		a.Unit = quantity.MustUnit(unit)
	}
	return a
}

func gPerML(f float64) *float64 { return &f }

// testRecipe is a two-component recipe covering every rule and a hole in the
// catalog (an unresolved line). Ingredient ids: 1 flour (with density),
// 2 sugar (none), 3 egg, 4 bay leaf, 5 salt.
func testRecipe(t *testing.T) recipe.Recipe {
	t.Helper()
	return recipe.Recipe{
		Name:        "Sponge <cake>",
		Description: "A light cake.",
		Author:      "Nan",
		SourceURL:   "https://example.com/cake",
		Servings:    quantity.Exact(rat(t, "4")),
		Yield:       recipe.Yield{Qty: quantity.Exact(rat(t, "1")), Unit: "cake"},
		Tags:        []string{"baking"},
		Times: recipe.Times{
			Prep: 20 * time.Minute, Cook: 45 * time.Minute,
			Special: []recipe.SpecialTime{{Label: recipe.LabelRest, Duration: 10 * time.Minute}},
		},
		Components: []recipe.Component{
			{
				Name: "Batter",
				Lines: []recipe.IngredientLine{
					{Amount: amount(t, "1 1/3", "cup"), Ingredient: 1, Rule: quantity.RuleLinear,
						Qualifiers: []recipe.Qualifier{{Kind: recipe.QualPreparation, Text: "sifted"}}},
					{Amount: amount(t, "1/3", "cup"), Ingredient: 2, Rule: quantity.RuleLinear},
					{Amount: amount(t, "2", ""), Ingredient: 3, Rule: quantity.RuleLinear},
					{Amount: amount(t, "1", ""), Ingredient: 4, Rule: quantity.RuleFixed},
					{Ingredient: 5, Rule: quantity.RuleToTaste, Optional: true},
					{Raw: "a splash of the good stuff", Rule: quantity.RuleLinear},
				},
				Steps: []recipe.Step{
					{ID: "mix", Text: "Mix everything.", Duration: 10 * time.Minute},
					{ID: "bake", Text: "Bake until golden.", Duration: 45 * time.Minute, Passive: true, DependsOn: []recipe.StepID{"mix"}},
				},
			},
			{
				Name:  "Glaze",
				Steps: []recipe.Step{{ID: "glaze", Text: "Glaze it.", DependsOn: []recipe.StepID{"bake"}}},
			},
		},
		Equipment: []recipe.EquipmentLink{{Equipment: 1}},
	}
}

func testCatalog() fakeCatalog {
	return fakeCatalog{
		ingredients: map[int64]catalog.Ingredient{
			1: {ID: 1, Name: "flour", DensityGPerML: gPerML(0.5)},
			2: {ID: 2, Name: "sugar"},
			3: {ID: 3, Name: "egg"},
			4: {ID: 4, Name: "bay leaf"},
			5: {ID: 5, Name: "salt"},
		},
		equipment: map[int64]catalog.Equipment{1: {ID: 1, Name: "9x13 pan"}},
	}
}

func serve(t *testing.T, recipes fakeRecipes, path string) (int, string) {
	t.Helper()
	rec := serveRequest(t, recipes, testCatalog(), httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.String()
}

func serveRequest(t *testing.T, recipes Recipes, cat Catalog, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Mount(Pattern, Routes(Deps{Recipes: recipes, Catalog: cat}))
	// Errors are logged by layout.WriteError; keep the test output quiet.
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(prev)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func mustContain(t *testing.T, body string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("body missing %q\n%s", w, body)
		}
	}
}

func mustNotContain(t *testing.T, body string, bad ...string) {
	t.Helper()
	for _, w := range bad {
		if strings.Contains(body, w) {
			t.Errorf("body contains %q\n%s", w, body)
		}
	}
}

func TestViewShowsRecipeAsWritten(t *testing.T) {
	code, body := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	if code != http.StatusOK {
		t.Fatalf("status = %d\n%s", code, body)
	}
	mustContain(t, body,
		"<title>Sponge &lt;cake&gt; - gobbler</title>",
		"1 1/3 cup flour, sifted",
		"1/3 cup sugar",
		"2 egg",
		"1 bay leaf",
		"salt to taste (optional)",
		"a splash of the good stuff",
		"Mix everything.", "Bake until golden.", "10 min", "45 min, passive",
		"9x13 pan",
		"Batter", "Glaze",
		`<ol start="3">`, // glaze step continues the numbering
		"Prep", "20 min", "Cook", "rest", "10 min",
		"1 h 15 min", // critical path of mix -> bake; glaze has no duration, so sum is used
	)
	mustNotContain(t, body, "time may change", "Scaled by")
}

func TestScaleMultipliesLinearLinesOnly(t *testing.T) {
	_, body := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7?scale=3")
	mustContain(t, body,
		"4 cup flour, sifted", // 1 1/3 x 3 = 4 exactly
		"1 cup sugar",         // 1/3 x 3: exactly 1, not 0.9999
		"6 egg",               // linear count
		"1 bay leaf",          // fixed
		"salt to taste",       // to taste unchanged
		`data-testid="servings">12<`,
		"time may change",
		"Scaled by 3",
	)
}

func TestScaleReadsABetterUnit(t *testing.T) {
	r := testRecipe(t)
	r.Components[0].Lines = []recipe.IngredientLine{{Amount: amount(t, "8", "tsp"), Ingredient: 2, Rule: quantity.RuleLinear}}
	_, body := serve(t, fakeRecipes{1: r}, "/recipes/1?scale=6") // 48 tsp
	mustContain(t, body, "1 cup sugar")
}

func TestServingsParamScalesToTargetServings(t *testing.T) {
	_, body := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7?servings=2")
	mustContain(t, body, "2/3 cup flour", "1/8 cup sugar", `data-testid="servings">2<`)
}

func TestUnitModes(t *testing.T) {
	_, metric := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7?units=metric")
	mustContain(t, metric, "315 ml flour", "79 ml sugar")
	// 1 1/3 cup is 315.45 ml; the exact amount stays available.
	mustContain(t, metric, `title="exact: 315.45 ml"`)

	_, weight := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7?units=weight")
	// flour has a density; sugar has none and falls back to metric volume.
	mustContain(t, weight, "160 g flour", "79 ml sugar")
	mustNotContain(t, weight, "cup")
}

func TestUnitModeAppliesAfterScaling(t *testing.T) {
	_, body := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7?scale=2&units=metric")
	mustContain(t, body, "630 ml flour")
}

func TestScaledViewHasNoEquipmentHint(t *testing.T) {
	// ADR-00005 limits the pan hint to equipment with a size, and the schema
	// has no equipment size (issue #85), so no equipment gets one.
	for _, path := range []string{"/recipes/7?scale=3", "/recipes/7?scale=1/2"} {
		_, body := serve(t, fakeRecipes{7: testRecipe(t)}, path)
		mustContain(t, body, "9x13 pan")
		mustNotContain(t, body, "larger pan", "smaller pan")
	}
}

func rangeRecipe(t *testing.T) recipe.Recipe {
	t.Helper()
	span, err := quantity.NewRange(rat(t, "1"), rat(t, "2"))
	if err != nil {
		t.Fatal(err)
	}
	servings, err := quantity.NewRange(rat(t, "4"), rat(t, "6"))
	if err != nil {
		t.Fatal(err)
	}
	r := testRecipe(t)
	r.Servings = servings
	r.Components[0].Lines = []recipe.IngredientLine{
		{Amount: quantity.Amount{Qty: span, Unit: quantity.MustUnit("cup")}, Ingredient: 2, Rule: quantity.RuleLinear},
		{Amount: quantity.Amount{Qty: span}, Ingredient: 3, Rule: quantity.RuleLinear},
	}
	return r
}

func TestRangesDisplayAsRanges(t *testing.T) {
	_, body := serve(t, fakeRecipes{7: rangeRecipe(t)}, "/recipes/7")
	mustContain(t, body, "1-2 cup sugar", "1-2 egg", `data-testid="servings">4-6<`)
}

func TestRangesScaleAsRanges(t *testing.T) {
	_, body := serve(t, fakeRecipes{7: rangeRecipe(t)}, "/recipes/7?scale=3")
	mustContain(t, body, "3-6 cup sugar", "3-6 egg", `data-testid="servings">12-18<`)
	// Both ends are scaled; neither is collapsed into a single number.
	mustNotContain(t, body, "9 cup", "6 cup sugar,")
}

func TestRangesConvertAsRanges(t *testing.T) {
	_, body := serve(t, fakeRecipes{7: rangeRecipe(t)}, "/recipes/7?units=metric")
	mustContain(t, body, "235-475 ml sugar")
}

func TestServingsTargetNeedsASingleServingCount(t *testing.T) {
	code, _ := serve(t, fakeRecipes{7: rangeRecipe(t)}, "/recipes/7?servings=4")
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: a range of servings has no single base to scale from", code)
	}
}

func TestUnresolvedLineIsShownAsWrittenAndFlagged(t *testing.T) {
	_, body := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7?scale=2")
	mustContain(t, body, "a splash of the good stuff", "not scaled or converted")
}

func TestControlsKeepTheOtherSetting(t *testing.T) {
	_, body := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7?scale=2&units=metric")
	mustContain(t, body,
		`href="/recipes/7?scale=3&amp;units=metric"`, // scale link keeps units
		`href="/recipes/7?scale=2&amp;units=weight"`, // unit link keeps scale
		`hx-get="/recipes/7?scale=2&amp;units=weight"`,
		`aria-current="true"`,
	)
}

func TestErrors(t *testing.T) {
	r := testRecipe(t)
	noServings := testRecipe(t)
	noServings.Servings = quantity.Absent()
	dangling := testRecipe(t)
	dangling.Components[0].Lines[0].Ingredient = 99

	for _, tc := range []struct {
		name    string
		recipes fakeRecipes
		path    string
		want    int
	}{
		{"missing recipe", fakeRecipes{}, "/recipes/9", http.StatusNotFound},
		{"non-numeric id", fakeRecipes{7: r}, "/recipes/abc", http.StatusNotFound},
		{"zero id", fakeRecipes{7: r}, "/recipes/0", http.StatusNotFound},
		{"bad scale", fakeRecipes{7: r}, "/recipes/7?scale=lots", http.StatusBadRequest},
		{"zero scale", fakeRecipes{7: r}, "/recipes/7?scale=0", http.StatusBadRequest},
		{"negative scale", fakeRecipes{7: r}, "/recipes/7?scale=-2", http.StatusBadRequest},
		{"divide by zero", fakeRecipes{7: r}, "/recipes/7?scale=1/0", http.StatusBadRequest},
		{"bad servings", fakeRecipes{7: r}, "/recipes/7?servings=x", http.StatusBadRequest},
		{"zero servings", fakeRecipes{7: r}, "/recipes/7?servings=0", http.StatusBadRequest},
		{"servings without a base", fakeRecipes{7: noServings}, "/recipes/7?servings=4", http.StatusBadRequest},
		{"scale and servings", fakeRecipes{7: r}, "/recipes/7?scale=2&servings=4", http.StatusBadRequest},
		{"unknown units", fakeRecipes{7: r}, "/recipes/7?units=cubits", http.StatusBadRequest},
		{"dangling ingredient is a server fault", fakeRecipes{7: dangling}, "/recipes/7", http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code, body := serve(t, tc.recipes, tc.path); code != tc.want {
				t.Errorf("status = %d, want %d\n%s", code, tc.want, body)
			}
		})
	}
}

func TestStoreFailureIsAServerError(t *testing.T) {
	r := chi.NewRouter()
	r.Mount(Pattern, Routes(Deps{Recipes: failingRecipes{}, Catalog: testCatalog()}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/recipes/1", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "disk on fire") {
		t.Error("the error text of a failing store reached the page")
	}
}

type failingRecipes struct{}

func (failingRecipes) Get(context.Context, int64) (recipe.Recipe, error) {
	return recipe.Recipe{}, errors.New("disk on fire")
}

func (failingRecipes) Info(context.Context, int64) (store.Info, error) {
	return store.Info{}, errors.New("disk on fire")
}

func TestSourceURLWithScriptSchemeIsNotLinked(t *testing.T) {
	r := testRecipe(t)
	r.SourceURL = "javascript:alert(1)"
	_, body := serve(t, fakeRecipes{7: r}, "/recipes/7")
	mustNotContain(t, body, `href="javascript:`)
}

// storeEnv is a real SQLite file with every migration applied and the real
// recipe store and catalog over it.
func storeEnv(t *testing.T) (context.Context, *store.Store, *catalog.Store) {
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
		t.Fatal("auth middleware did not call the handler")
	}
	return household.WithContext(authed, devHousehold(t, d)), store.New(d), catalog.New(d)
}

func TestScalingAndUnitModesDoNotChangeTheStoredRecipe(t *testing.T) {
	ctx, rs, cs := storeEnv(t)
	density := 0.5
	flour, err := cs.CreateIngredient(ctx, catalog.Ingredient{Name: "view test flour", Aisle: "baking", DensityGPerML: &density})
	if err != nil {
		t.Fatal(err)
	}
	pan, err := cs.CreateEquipment(ctx, catalog.Equipment{Name: "view test pan"})
	if err != nil {
		t.Fatal(err)
	}
	r := testRecipe(t)
	r.Components[0].Lines = []recipe.IngredientLine{
		{Amount: amount(t, "1 1/3", "cup"), Ingredient: recipe.IngredientID(flour.ID), Rule: quantity.RuleLinear},
		{Raw: "a splash of the good stuff", Rule: quantity.RuleLinear},
	}
	r.Equipment = []recipe.EquipmentLink{{Equipment: recipe.EquipmentID(pan.ID)}}
	id, err := rs.Create(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	before, err := rs.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	path := fmt.Sprintf("/recipes/%d", id)
	for _, q := range []string{"?scale=3", "?servings=2&units=metric", "?units=weight", "?scale=1/2&units=metric"} {
		rec := serveRequest(t, rs, cs, httptest.NewRequest(http.MethodGet, path+q, nil).WithContext(ctx))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d\n%s", q, rec.Code, rec.Body)
		}
		after, err := rs.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("%s changed the stored recipe:\nbefore %+v\nafter  %+v", q, before, after)
		}
	}
}

func TestInvalidInputFromHTMXIsShownInPlace(t *testing.T) {
	// htmx 2 does not swap 4xx responses, so the user would see nothing. For
	// an htmx request the page answers 200 with the recipe and a message, and
	// asks htmx not to push the bad URL.
	req := httptest.NewRequest(http.MethodGet, "/recipes/7?servings=x", nil)
	req.Header.Set("HX-Request", "true")
	rec := serveRequest(t, fakeRecipes{7: testRecipe(t)}, testCatalog(), req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("HX-Push-Url"); got != "false" {
		t.Errorf("HX-Push-Url = %q, want false", got)
	}
	mustContain(t, rec.Body.String(), `id="recipe"`, `role="alert"`, "cannot be used", "1 1/3 cup flour")

	// Without htmx the same input is a plain 400 page (TestErrors).
	if code, _ := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7?servings=x"); code != http.StatusBadRequest {
		t.Errorf("plain request status = %d, want 400", code)
	}
}

func TestServingsTargetOnARangeOfServingsExplainsWhatToDo(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/recipes/7?servings=4", nil)
	req.Header.Set("HX-Request", "true")
	rec := serveRequest(t, fakeRecipes{7: rangeRecipe(t)}, testCatalog(), req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	mustContain(t, body, `role="alert"`, "Scale by a multiplier")
	// The generic "enter a number" advice would be wrong: 4 is a number.
	mustNotContain(t, body, "positive number or fraction")
}

func TestViewLinksToCookModeKeepingTheState(t *testing.T) {
	_, body := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	mustContain(t, body, `data-testid="cook-link"`, `href="/recipes/7/cook"`)
	_, body = serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7?scale=2&units=metric")
	mustContain(t, body, `href="/recipes/7/cook?scale=2&amp;units=metric"`)
}

func TestViewLinksToTheEditor(t *testing.T) {
	_, body := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	mustContain(t, body, `data-testid="edit-link"`, `href="/recipes/7/edit"`)
	// The editor has no display state, so scaling does not leak into its URL.
	_, body = serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7?scale=2&units=metric")
	mustContain(t, body, `href="/recipes/7/edit"`)
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
