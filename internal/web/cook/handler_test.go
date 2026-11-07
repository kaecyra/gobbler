package cook

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/layout/layouttest"
)

type fakeRecipes map[int64]recipe.Recipe

func (f fakeRecipes) Get(_ context.Context, _ apirecipes.Caller, id int64) (recipe.Recipe, error) {
	r, ok := f[id]
	if !ok {
		return recipe.Recipe{}, fmt.Errorf("recipe %d: %w", id, store.ErrNotFound)
	}
	return r, nil
}

type fakeCatalog struct{}

func (fakeCatalog) GetIngredient(_ context.Context, id int64) (catalog.Ingredient, error) {
	names := map[int64]string{1: "flour", 2: "sugar", 3: "tomato"}
	n, ok := names[id]
	if !ok {
		return catalog.Ingredient{}, fmt.Errorf("ingredient %d: %w", id, catalog.ErrNotFound)
	}
	return catalog.Ingredient{ID: id, Name: n}, nil
}

func (fakeCatalog) GetEquipment(_ context.Context, id int64) (catalog.Equipment, error) {
	names := map[int64]string{1: "mixing bowl", 2: "baking sheet"}
	n, ok := names[id]
	if !ok {
		return catalog.Equipment{}, fmt.Errorf("equipment %d: %w", id, catalog.ErrNotFound)
	}
	return catalog.Equipment{ID: id, Name: n}, nil
}

func rat(t *testing.T, s string) quantity.Rat {
	t.Helper()
	r, err := quantity.ParseRat(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func line(t *testing.T, qty, unit string, ing recipe.IngredientID) recipe.IngredientLine {
	t.Helper()
	a := quantity.Amount{Qty: quantity.Exact(rat(t, qty))}
	if unit != "" {
		a.Unit = quantity.MustUnit(unit)
	}
	return recipe.IngredientLine{Amount: a, Ingredient: ing, Rule: quantity.RuleLinear}
}

// pizza: 1 mix, 2 rise (passive, 1 h), 3 bake, 4 chop <tomatoes>, 5 simmer,
// 6 serve.
// The bowl is used by mix only, the sheet by no step in particular.
func pizza(t *testing.T) recipe.Recipe {
	t.Helper()
	return recipe.Recipe{
		Name:     "Pizza <1>",
		Servings: quantity.Exact(rat(t, "4")),
		Components: []recipe.Component{
			{Name: "Dough", Lines: []recipe.IngredientLine{line(t, "1 1/3", "cup", 1), line(t, "1/3", "cup", 2)},
				Steps: []recipe.Step{
					{ID: "mix", Text: "Mix the dough.", Title: "Make the dough", Duration: 10 * time.Minute},
					{ID: "rise", Text: "Let it rise.", Duration: time.Hour, Passive: true, DependsOn: []recipe.StepID{"mix"}},
					{ID: "bake", Text: "Bake.", Title: "Finish in the oven", DependsOn: []recipe.StepID{"rise"}},
				}},
			{Name: "Sauce", Lines: []recipe.IngredientLine{line(t, "2", "", 3)},
				Steps: []recipe.Step{
					{ID: "chop", Text: "Chop <tomatoes>.", Duration: 5 * time.Minute},
					{ID: "simmer", Text: "Simmer the sauce.", Duration: 20 * time.Minute, DependsOn: []recipe.StepID{"chop"}},
				}},
			{Name: "Finish", Steps: []recipe.Step{
				{ID: "serve", Text: "Top and serve.", Duration: time.Minute, DependsOn: []recipe.StepID{"bake", "simmer"}},
			}},
		},
		Equipment: []recipe.EquipmentLink{
			{Equipment: 1, Steps: []recipe.StepID{"mix"}},
			{Equipment: 2},
		},
	}
}

func serve(t *testing.T, recipes fakeRecipes, path string) (int, string) {
	t.Helper()
	r := chi.NewRouter()
	r.Mount(Pattern, Routes(Deps{Recipes: recipes, Catalog: fakeCatalog{}}))
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

func TestFirstStepShowsOneStepAndItsContext(t *testing.T) {
	code, body := serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook")
	if code != http.StatusOK {
		t.Fatalf("status = %d\n%s", code, body)
	}
	mustContain(t, body,
		"Step 1 of 6", "Mix the dough.", "Dough",
		"1 1/3 cup flour", "1/3 cup sugar", // the step's component
		"mixing bowl", "baking sheet", // linked to this step, and linked to none
		`data-testid="next"`, `href="/recipes/3/cook?step=2"`,
		`data-testid="step-list"`, `aria-current="step"`,
		"Pizza &lt;1&gt;",
	)
	mustNotContain(t, body, "2 tomato", `data-testid="prev"`)
	mustNotContain(t, elementOf(t, body, "step-text", "</div>"), "Let it rise.")
	// The other steps appear only in the step list, not as the shown step.
	if n := strings.Count(body, `data-testid="step-text"`); n != 1 {
		t.Errorf("shown steps = %d, want 1", n)
	}
}

func TestStepTitleHeadsTheStepAndUpNextNamesTheNextOne(t *testing.T) {
	_, body := serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook")
	mustContain(t, body, `data-testid="step-title"`, "Make the dough")
	// The step list holds every step's text, so up next is read on its own.
	mustContain(t, upNextOf(t, body), "Let it rise.")
	// A step without a title has no heading; the last step has no up next.
	_, body = serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook?step=6")
	mustNotContain(t, body, `data-testid="step-title"`, `data-testid="up-next"`)
}

func TestUpNextShowsTheNextStepsTitleWhenItHasOne(t *testing.T) {
	_, body := serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook?step=2")
	up := upNextOf(t, body)
	mustContain(t, up, "Finish in the oven", `href="/recipes/3/cook?step=3"`)
	mustNotContain(t, up, "Bake.")
}

// upNextOf returns the up-next element of a rendered cook page.
func upNextOf(t *testing.T, body string) string {
	t.Helper()
	return elementOf(t, body, "up-next", "</aside>")
}

// elementOf returns a rendered page from the element with the test id up to
// the first end after it.
func elementOf(t *testing.T, body, testID, end string) string {
	t.Helper()
	i := strings.Index(body, `data-testid="`+testID+`"`)
	if i < 0 {
		t.Fatalf("no %s element\n%s", testID, body)
	}
	j := strings.Index(body[i:], end)
	if j < 0 {
		t.Fatalf("%s element is not closed by %s\n%s", testID, end, body)
	}
	return body[i : i+j]
}

// ADR-00046: cook mode renders step text through the one markdown renderer,
// in the step itself and wherever a step without a title is named.
func TestStepTextRendersAsMarkdown(t *testing.T) {
	r := pizza(t)
	r.Components[0].Steps[0].Text = "Mix **well**."
	r.Components[0].Steps[1].Text = "Let it *rise*."
	_, body := serve(t, fakeRecipes{3: r}, "/recipes/3/cook")
	mustContain(t, body, `<div class="cook-text md" data-testid="step-text">`)
	mustContain(t, elementOf(t, body, "step-text", "</div>"), "<p>Mix <strong>well</strong>.</p>")
	mustContain(t, upNextOf(t, body), "<p>Let it <em>rise</em>.</p>")
	mustNotContain(t, body, "**well**", "*rise*")
}

func TestHeaderShowsServingsAndUnits(t *testing.T) {
	_, body := serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook?scale=2&units=metric")
	mustContain(t, elementOf(t, body, "cook-meta", "</span>"), "Serves 8 · Metric")
	r := pizza(t)
	r.Servings = quantity.Quantity{}
	_, body = serve(t, fakeRecipes{3: r}, "/recipes/3/cook")
	mustContain(t, body, `data-testid="cook-meta">As written</span>`)
}

// The mockup marks a step done while another waits ("Step 4 of 6 ·
// meanwhile"), and the next step is named once when it is also meanwhile work.
func TestMeanwhileStepsAreMarkedAndNamedOnce(t *testing.T) {
	_, body := serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook?step=4")
	mustContain(t, elementOf(t, body, "progress", "</p>"), "· meanwhile")
	_, body = serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook?step=3")
	mustNotContain(t, elementOf(t, body, "progress", "</p>"), "meanwhile")

	// Bake moves to the last component, so chop follows the rise: offered
	// meanwhile during it, and also the next step.
	r := pizza(t)
	bake := r.Components[0].Steps[2]
	r.Components[0].Steps = r.Components[0].Steps[:2]
	r.Components[2].Steps = append([]recipe.Step{bake}, r.Components[2].Steps...)
	_, body = serve(t, fakeRecipes{3: r}, "/recipes/3/cook?step=2")
	mustContain(t, elementOf(t, body, "meanwhile", "</ul>"), "Chop &lt;tomatoes&gt;.", `href="/recipes/3/cook?step=3"`)
	mustNotContain(t, body, `data-testid="up-next"`)
	mustContain(t, body, `data-testid="next"`) // Next still goes there
}

func TestLaterStepHasPreviousAndOtherComponentContext(t *testing.T) {
	_, body := serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook?step=4")
	mustContain(t, body, "Step 4 of 6", "Chop &lt;tomatoes&gt;.", "2 tomato", "Sauce",
		`data-testid="prev"`, `href="/recipes/3/cook?step=3"`, `data-testid="next"`)
	mustNotContain(t, body, "flour", "mixing bowl")
	mustContain(t, body, "baking sheet") // equipment with no step link applies throughout
}

func TestStepWithDurationOffersALabelledTimer(t *testing.T) {
	_, body := serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook?step=2")
	mustContain(t, body, `data-cook-timer`, `data-step="2"`, `data-seconds="3600"`,
		`data-label="Let it rise."`, "Start timer (1 h)")
	// A step without a title names its timer by its text as the reader sees
	// it, not its markdown source (ADR-00046).
	r := pizza(t)
	r.Components[0].Steps[1].Text = "Let it **rise** & `rest`."
	_, body = serve(t, fakeRecipes{3: r}, "/recipes/3/cook?step=2")
	mustContain(t, body, `data-label="Let it rise &amp; rest."`)
	mustNotContain(t, body, `data-label="Let it **rise**`)
	// A step with a title names its timer by it.
	_, body = serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook?step=1")
	mustContain(t, body, `data-label="Make the dough"`)
}

func TestStepWithoutDurationHasNoTimer(t *testing.T) {
	_, body := serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook?step=3")
	mustNotContain(t, body, "data-cook-timer", "data-seconds")
}

func TestTimersSitOutsideTheSwappedStepPanel(t *testing.T) {
	_, body := serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook")
	step := strings.Index(body, `id="cook-step"`)
	timers := strings.Index(body, `id="cook-timer-list"`)
	end := strings.Index(body[step:], "</nav>")
	if step < 0 || timers < 0 || timers < step+end {
		t.Errorf("timer list must come after, not inside, the step panel (step %d, timers %d)", step, timers)
	}
	mustContain(t, body, `/static/cook-timers.js`, `/static/cook.js`, `/static/cook.css`, `id="cook-wake"`)
	layouttest.AssertNoRemoteAssets(t, "cook page", body) // nothing from another origin
}

func TestPassiveStepShowsMeanwhileGuidance(t *testing.T) {
	_, body := serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook?step=2")
	guidance := elementOf(t, body, "meanwhile", "</ul>")
	mustContain(t, guidance, "Meanwhile", "Step 4", "Chop &lt;tomatoes&gt;.", `href="/recipes/3/cook?step=4"`)
	mustNotContain(t, guidance, "Simmer the sauce.") // waits for chop, so not offered
	_, body = serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook?step=1")
	mustNotContain(t, body, `data-testid="meanwhile"`)
}

func TestScaleAndUnitsApplyAndAreKept(t *testing.T) {
	_, body := serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook?scale=3&units=metric")
	mustContain(t, body, "ml flour") // converted
	mustContain(t, body,
		`href="/recipes/3/cook?scale=3&amp;step=2&amp;units=metric"`, // stepping keeps the state
		`href="/recipes/3-pizza-1?scale=3&amp;units=metric"`)         // so does leaving
	_, body = serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook?servings=8")
	mustContain(t, body, "2 2/3 cup flour")
}

func TestRecipeWithNoStepsShowsAnEmptyState(t *testing.T) {
	r := recipe.Recipe{Name: "Water", Components: []recipe.Component{{}}}
	code, body := serve(t, fakeRecipes{5: r}, "/recipes/5/cook")
	if code != http.StatusOK {
		t.Fatalf("status = %d\n%s", code, body)
	}
	mustContain(t, body, `data-testid="cook-empty"`, "no steps", `href="/recipes/5-water"`)
	mustNotContain(t, body, `data-testid="next"`, "data-cook-timer")
}

func TestErrors(t *testing.T) {
	r := fakeRecipes{3: pizza(t)}
	for _, tc := range []struct {
		name string
		path string
		want int
	}{
		{"unknown recipe", "/recipes/9/cook", http.StatusNotFound},
		{"non-numeric id", "/recipes/abc/cook", http.StatusNotFound},
		{"zero id", "/recipes/0/cook", http.StatusNotFound},
		{"step not a number", "/recipes/3/cook?step=x", http.StatusBadRequest},
		{"step zero", "/recipes/3/cook?step=0", http.StatusBadRequest},
		{"step negative", "/recipes/3/cook?step=-1", http.StatusBadRequest},
		{"step past the end", "/recipes/3/cook?step=7", http.StatusBadRequest},
		{"bad scale", "/recipes/3/cook?scale=lots", http.StatusBadRequest},
		{"bad units", "/recipes/3/cook?units=cubits", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code, body := serve(t, r, tc.path); code != tc.want {
				t.Errorf("status = %d, want %d\n%s", code, tc.want, body)
			}
		})
	}
}

func TestDanglingIngredientIsAServerFault(t *testing.T) {
	r := pizza(t)
	r.Components[0].Lines[0].Ingredient = 99
	if code, _ := serve(t, fakeRecipes{3: r}, "/recipes/3/cook"); code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", code)
	}
}

func TestBrokenStepGraphFailsLoudly(t *testing.T) {
	r := pizza(t)
	r.Components[1].Steps[0].DependsOn = []recipe.StepID{"simmer"}
	if code, _ := serve(t, fakeRecipes{3: r}, "/recipes/3/cook"); code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", code)
	}
}

// TestImportsNoOtherWebFeature enforces ADR-00012: a feature package does not
// import another internal/web/<area> package. layout is the shared shell, and
// layout/layouttest its shared test checks.
func TestImportsNoOtherWebFeature(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}{{"\n"}}{{join .TestImports "\n"}}`, ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, imp := range strings.Fields(string(out)) {
		if strings.Contains(imp, "/internal/web/") && !strings.HasSuffix(imp, "/internal/web/layout") && !strings.HasSuffix(imp, "/internal/web/layout/layouttest") && !strings.HasSuffix(imp, "/internal/web/cook") {
			t.Errorf("imports %s: a web feature package may not import another (ADR-00012)", imp)
		}
	}
}

// ADR-00024: cook mode shows equipment as chips with an icon.
func TestEquipmentIsAChipWithAnIcon(t *testing.T) {
	_, body := serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook?step=1")
	mustContain(t, body, `<li class="equip-chip"><svg class="equip-ic"`, "mixing bowl")
}
