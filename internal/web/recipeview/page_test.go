package recipeview

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe/display"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// journaled is a recipe store whose recipe has a journal.
type journaled struct {
	fakeRecipes
	notes []store.Note
	made  []store.MadeIt
}

func (j journaled) ListNotes(context.Context, int64) ([]store.Note, error) { return j.notes, nil }

func (j journaled) ListMadeIt(context.Context, int64) ([]store.MadeIt, error) { return j.made, nil }

func page(t *testing.T, r Recipes, path string) string {
	t.Helper()
	rec := serveRequest(t, r, testCatalog(), httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d\n%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func intp(n int) *int { return &n }

func TestPinnedNoteShowsAsATipAndAnUnpinnedOneDoesNot(t *testing.T) {
	body := page(t, journaled{
		fakeRecipes: fakeRecipes{7: testRecipe(t)},
		notes: []store.Note{
			{ID: 1, Body: "Salt the night before.", Pinned: true},
			{ID: 2, Body: "Private remark.", Pinned: false},
		},
	}, "/recipes/7")
	mustContain(t, body, `class="tip"`, `data-testid="tip"`, "Salt the night before.")
	mustNotContain(t, body, "Private remark.")
}

func TestRecipeWithoutPinnedNotesHasNoTip(t *testing.T) {
	body := page(t, journaled{fakeRecipes: fakeRecipes{7: testRecipe(t)}}, "/recipes/7")
	mustNotContain(t, body, `data-testid="tip"`)
}

func TestStepTitleHeadsTheStep(t *testing.T) {
	r := testRecipe(t)
	r.Components[0].Steps[0].Title = "Cream the butter"
	body := page(t, fakeRecipes{7: r}, "/recipes/7")
	mustContain(t, body, `<h4 data-testid="step-title">Cream the butter</h4>`)
	// A step with no title gets no empty heading.
	if n := strings.Count(body, `data-testid="step-title"`); n != 1 {
		t.Errorf("step titles = %d, want 1\n%s", n, body)
	}
}

func TestMetaRowShowsMealSpiceRatingAndMadeCount(t *testing.T) {
	r := testRecipe(t)
	r.MealType = recipe.MealDessert
	r.SpiceLevel = intp(2)
	r.Rating = intp(3)
	body := page(t, journaled{
		fakeRecipes: fakeRecipes{7: r},
		made:        []store.MadeIt{{ID: 1}, {ID: 2}, {ID: 3}},
	}, "/recipes/7")
	mustContain(t, body,
		`data-testid="meal-type"`, "Dessert",
		`data-testid="spice"`, `aria-label="Medium heat"`,
		`data-testid="rating"`, `aria-label="Good"`,
		`data-testid="made-count"`, "Made 3×",
		`data-testid="source-link"`, "example.com",
	)
	if n := strings.Count(body, `class="chili"`); n != 2 {
		t.Errorf("chilis = %d, want 2", n)
	}
}

func TestMetaRowShowsTheCuisineLabelOnlyWhenSet(t *testing.T) {
	r := testRecipe(t)
	r.Cuisine = recipe.CuisineMiddleEastern
	body := page(t, fakeRecipes{7: r}, "/recipes/7")
	mustContain(t, body, `data-testid="cuisine"`, "Middle Eastern")
	mustNotContain(t, page(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7"), `data-testid="cuisine"`)
}

func TestUnsetFieldsShowNothingAndAnUnratedRecipeSaysNew(t *testing.T) {
	body := page(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	mustNotContain(t, body, `data-testid="meal-type"`, `data-testid="spice"`, `data-testid="made-count"`)
	mustContain(t, body, `data-testid="rating-new"`)
}

func TestSpiceLevelZeroShowsNoChilis(t *testing.T) {
	r := testRecipe(t)
	r.SpiceLevel = intp(0)
	body := page(t, fakeRecipes{7: r}, "/recipes/7")
	mustNotContain(t, body, `class="chili"`, `data-testid="spice"`)
}

func TestCookModeSitsInTheControlsRow(t *testing.T) {
	body := page(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	start := indexOf(t, body, `class="recipe-controls"`)
	end := start + indexOf(t, body[start:], `class="recipe-grid"`)
	row := body[start:end]
	if !strings.Contains(row, `data-testid="cook-link"`) || !strings.Contains(row, ">Cook mode<") {
		t.Errorf("Cook mode is not in the controls row\n%s", row)
	}
	mustNotContain(t, body, "Start cooking")
}

func TestReadyInAboutAboveALargeRibbon(t *testing.T) {
	body := page(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	mustContain(t, body, "Ready in about", "rib rib-big", `/static/ribbon.css`, `/static/ribbon.js`)
}

func TestRecipeWithNoTimedStepHasNoRibbon(t *testing.T) {
	r := recipe.Recipe{Name: "Toast", Components: []recipe.Component{{Steps: []recipe.Step{{ID: "a", Text: "Toast it."}}}}}
	body := page(t, fakeRecipes{1: r}, "/recipes/1")
	mustNotContain(t, body, "rib-big", "Ready in about")
}

func TestStepShowsWhatItWaitsFor(t *testing.T) {
	body := page(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	mustContain(t, body, "after step 1")
}

func TestPageCarriesItsStylesheet(t *testing.T) {
	body := page(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	mustContain(t, body, "<style>", ".recipe-controls")
}

func TestComponentWithNoIngredientLinesHasNoIngredientHeading(t *testing.T) {
	body := page(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	// Glaze has steps and no lines: it heads its steps, not an empty list.
	if n := strings.Count(body, "<h3>Glaze</h3>"); n != 0 {
		t.Errorf("empty ingredient heading present\n%s", body)
	}
	mustContain(t, body, `<h3 class="part">Glaze</h3>`)
}

func TestRecipeOfStepsAloneHasNoIngredientsHeading(t *testing.T) {
	body := page(t, fakeRecipes{3: doughAndSauce()}, "/recipes/3")
	mustNotContain(t, body, "Ingredients")
	mustContain(t, body, "Mix the dough.")
}

// ADR-00004 allows three total rules, and the page says which applied. One
// timed step and one untimed step with no stated times has none of them, so
// the page states no total: summing the timed step would skip the untimed one.
func TestNoDerivableTotalStatesNoReadyInTime(t *testing.T) {
	r := recipe.Recipe{Name: "Half known", Components: []recipe.Component{{Steps: []recipe.Step{
		{ID: "a", Text: "Timed.", Duration: 30 * time.Minute},
		{ID: "b", Text: "Untimed.", DependsOn: []recipe.StepID{"a"}},
	}}}}
	body := page(t, fakeRecipes{1: r}, "/recipes/1")
	mustNotContain(t, body, "Ready in about", `data-testid="total"`)
	// The ribbon still draws the one step that has a time.
	mustContain(t, body, "rib rib-big")
}

// brokenJournal fails the journal reads the page uses for tips and the made
// count. A recipe page without them would look complete and be wrong, so a
// failure is a server error (error-handling rule).
type brokenJournal struct {
	fakeRecipes
	notes, made, thumb bool
}

func (b brokenJournal) ListNotes(ctx context.Context, id int64) ([]store.Note, error) {
	if b.notes {
		return nil, errors.New("disk on fire")
	}
	return b.fakeRecipes.ListNotes(ctx, id)
}

func (b brokenJournal) ListMadeIt(ctx context.Context, id int64) ([]store.MadeIt, error) {
	if b.made {
		return nil, errors.New("disk on fire")
	}
	return b.fakeRecipes.ListMadeIt(ctx, id)
}

func (b brokenJournal) HasThumbnail(ctx context.Context, videoID string) (bool, error) {
	if b.thumb {
		return false, errors.New("disk on fire")
	}
	return b.fakeRecipes.HasThumbnail(ctx, videoID)
}

func TestFailedReadsAreServerErrorsAndLeakNoCause(t *testing.T) {
	withVideo := testRecipe(t)
	withVideo.VideoID = "dQw4w9WgXcQ"
	for name, r := range map[string]Recipes{
		"notes":     brokenJournal{fakeRecipes: fakeRecipes{7: testRecipe(t)}, notes: true},
		"made-it":   brokenJournal{fakeRecipes: fakeRecipes{7: testRecipe(t)}, made: true},
		"thumbnail": brokenJournal{fakeRecipes: fakeRecipes{7: withVideo}, thumb: true},
	} {
		rec := serveRequest(t, r, testCatalog(), httptest.NewRequest(http.MethodGet, "/recipes/7", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status = %d, want 500", name, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "disk on fire") {
			t.Errorf("%s: the page leaks the cause", name)
		}
	}
}

func TestUnknownCatalogEntriesAreServerErrorsNotBlankLines(t *testing.T) {
	missingIngredient := testRecipe(t)
	missingIngredient.Components[0].Lines[0].Ingredient = 99
	missingEquipment := testRecipe(t)
	missingEquipment.Equipment = []recipe.EquipmentLink{{Equipment: 99}}
	for name, r := range map[string]recipe.Recipe{"ingredient": missingIngredient, "equipment": missingEquipment} {
		rec := serveRequest(t, fakeRecipes{7: r}, testCatalog(), httptest.NewRequest(http.MethodGet, "/recipes/7", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status = %d, want 500", name, rec.Code)
		}
	}
}

func TestPageOfARecipeWithNothingToShowStillRenders(t *testing.T) {
	r := recipe.Recipe{Name: "Bare", Components: []recipe.Component{{}}}
	body := page(t, fakeRecipes{1: r}, "/recipes/1")
	mustContain(t, body, "<h1>Bare</h1>", `data-testid="rating-new"`)
	mustNotContain(t, body, "Ingredients", "Equipment", "Ready in about", `data-testid="tip"`, "step-title")
}

// failingWriter accepts writes until the nth call, then fails every one.
type failingWriter struct{ n, calls int }

func (f *failingWriter) Write(p []byte) (int, error) {
	f.calls++
	if f.calls >= f.n {
		return 0, errors.New("broken pipe")
	}
	return len(p), nil
}

// A write that fails part-way through the page must reach the caller, so the
// handler can log it; a page silently cut short would look complete. The
// padding moves the point where the output buffer fills across the page, so
// the failure lands in different components.
func TestAFailedWriteIsReportedWhereverItLands(t *testing.T) {
	r := testRecipe(t)
	r.VideoID = "dQw4w9WgXcQ"
	r.MealType, r.SpiceLevel, r.Rating = recipe.MealMain, intp(2), intp(3)
	for pad := 0; pad < 9000; pad += 61 {
		r.Description = strings.Repeat("x", pad)
		v, err := buildView(context.Background(), testCatalog(), 7, r, display.Default())
		if err != nil {
			t.Fatal(err)
		}
		v.Tips = []string{"A tip."}
		v.Notice = "Careful."
		v.Scaled = true
		v.Origin = &originView{RecipeID: 3, Version: 2}
		v.HasThumbnail = true
		for n := 1; n <= 8; n++ {
			w := &failingWriter{n: n}
			err := recipePage(v).Render(context.Background(), w)
			if w.calls >= n && err == nil {
				t.Fatalf("write %d failed with description of %d bytes and Render returned nil", n, pad)
			}
		}
	}
}
