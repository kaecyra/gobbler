package recipeview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/catalog"
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
	r := chi.NewRouter()
	r.Mount(Pattern, Routes(Deps{Recipes: recipes, Catalog: testCatalog()}))
	// Errors are logged by layout.WriteError; keep the test output quiet.
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(prev)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.String()
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
	mustNotContain(t, body, "time may change", "may need a larger pan", "Scaled by")
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
		"may need a larger pan", "time may change",
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
	mustContain(t, body, "2/3 cup flour", "1/8 cup sugar", `data-testid="servings">2<`, "may need a smaller pan")
	mustNotContain(t, body, "larger pan")
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

func TestSourceURLWithScriptSchemeIsNotLinked(t *testing.T) {
	r := testRecipe(t)
	r.SourceURL = "javascript:alert(1)"
	_, body := serve(t, fakeRecipes{7: r}, "/recipes/7")
	mustNotContain(t, body, `href="javascript:`)
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "", 30 * time.Second: "1 min", 5 * time.Minute: "5 min", time.Hour: "1 h",
		90 * time.Minute: "1 h 30 min", 10 * time.Second: "under 1 min",
	} {
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}
