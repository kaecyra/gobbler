package recipeview

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	mustContain(t, body, "Equipment"[:0]+"Mix the dough.")
}
