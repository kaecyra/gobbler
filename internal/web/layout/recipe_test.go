package layout

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipe/display"
)

func renderPart(t *testing.T, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func has(t *testing.T, body string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("missing %q in\n%s", w, body)
		}
	}
}

func lacks(t *testing.T, body string, bad ...string) {
	t.Helper()
	for _, w := range bad {
		if strings.Contains(body, w) {
			t.Errorf("contains %q in\n%s", w, body)
		}
	}
}

func intPtr(n int) *int { return &n }

func TestNewRecipeMetaClampsAndLeavesUnsetFieldsEmpty(t *testing.T) {
	m := NewRecipeMeta(recipe.Recipe{SpiceLevel: intPtr(9), Rating: intPtr(12), MealType: recipe.MealDrink})
	if m.Spice != recipe.MaxSpiceLevel || m.Spoons != maxSpoons || m.Meal != "Drink" {
		t.Errorf("meta = %+v, want spice %d, spoons %d, Drink", m, recipe.MaxSpiceLevel, maxSpoons)
	}
	m = NewRecipeMeta(recipe.Recipe{SpiceLevel: intPtr(0), Rating: intPtr(0)})
	if m.Spice != 0 || m.Spoons != 0 || m.Meal != "" || m.SourceHost != "" {
		t.Errorf("unset meta = %+v, want empty", m)
	}
	// A negative level is not a chili count.
	if m = NewRecipeMeta(recipe.Recipe{SpiceLevel: intPtr(-1), Rating: intPtr(-2)}); m.Spice != 0 || m.Spoons != 0 {
		t.Errorf("negative meta = %+v, want zero", m)
	}
}

func TestSourceHostStripsWWWAndSurvivesGarbage(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.seriouseats.com/a/b": "seriouseats.com",
		"https://example.com:8080/x":      "example.com",
		"not a url":                       "",
		"":                                "",
		"http://[::1":                     "",
	} {
		if got := sourceHost(in); got != want {
			t.Errorf("sourceHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMetaRowShowsEverySetFieldAndNothingElse(t *testing.T) {
	cal := 420
	body := renderPart(t, RecipeMetaRow(RecipeMeta{
		MealType: recipe.MealBreakfast, Meal: "Breakfast", Spice: 3, Spoons: 5, Made: 7,
		Author: "Nan", Calories: &cal, SourceURL: "https://www.example.com/x", SourceHost: "example.com",
	}))
	has(t, body,
		`data-testid="meal-type"`, "Breakfast",
		`aria-label="Hot"`, `aria-label="House favourite"`, "Made 7×", "By Nan", "420 kcal per serving",
		`data-testid="source-link"`, `target="_blank"`, `rel="noopener noreferrer"`, "(opens in a new tab)", "example.com")
	if n := strings.Count(body, `class="chili"`); n != 3 {
		t.Errorf("chilis = %d, want 3", n)
	}
	if n := strings.Count(body, `class="spoon on"`); n != 5 {
		t.Errorf("filled spoons = %d, want 5", n)
	}

	body = renderPart(t, RecipeMetaRow(RecipeMeta{}))
	lacks(t, body, "meal-type", "spice", "made-count", "By ", "kcal", "source-link", `data-testid="rating"`)
	has(t, body, `data-testid="rating-new"`)
}

func TestMetaRowFillsOnlyTheRatedSpoons(t *testing.T) {
	body := renderPart(t, RecipeMetaRow(RecipeMeta{Spoons: 2}))
	if on, off := strings.Count(body, `class="spoon on"`), strings.Count(body, `class="spoon"`); on != 2 || off != 3 {
		t.Errorf("spoons filled = %d, empty = %d, want 2 and 3", on, off)
	}
	has(t, body, `aria-label="It was fine"`)
}

func TestMetaRowSourceWithoutAHostStillLinks(t *testing.T) {
	body := renderPart(t, RecipeMetaRow(RecipeMeta{SourceURL: "https://x.test/", SourceHost: ""}))
	has(t, body, `data-testid="source-link"`, "Original recipe")
}

func TestMealIconsDifferPerMealType(t *testing.T) {
	seen := map[string]recipe.MealType{}
	for _, m := range recipe.MealTypes() {
		body := renderPart(t, RecipeMetaRow(RecipeMeta{MealType: m, Meal: mealLabels[m]}))
		start := strings.Index(body, "<svg")
		end := strings.Index(body, "</svg>")
		if start < 0 || end < start {
			t.Fatalf("no icon for %s:\n%s", m, body)
		}
		icon := body[start:end]
		if prev, dup := seen[icon]; dup {
			t.Errorf("%s and %s share an icon", m, prev)
		}
		seen[icon] = m
	}
}

func TestNewRecipeStepViewNumbersTheStepsItWaitsFor(t *testing.T) {
	s := recipe.Step{
		ID: "c", Title: "Serve", Text: "Plate it.", Duration: 15 * time.Minute, Passive: true,
		DependsOn: []recipe.StepID{"a", "b"},
	}
	got := NewRecipeStepView(s, map[recipe.StepID]int{"a": 1, "b": 2, "c": 3}, nil)
	if got.Title != "Serve" || !got.Passive || got.Time != "Timer · 15 min" || len(got.After) != 2 || got.After[0] != 1 || got.After[1] != 2 {
		t.Errorf("step view = %+v", got)
	}
	if got := NewRecipeStepView(recipe.Step{ID: "x", Text: "t"}, nil, nil); got.After != nil || got.Time != "" {
		t.Errorf("a step with no dependencies and no time has %+v", got)
	}
}

func TestStepShowsTagsOnlyForWhatItHas(t *testing.T) {
	body := renderPart(t, RecipeStep(RecipeStepView{Text: "Stir."}, "4"))
	has(t, body, `class="step"`, ">4<", "Stir.")
	lacks(t, body, "step-title", "stags", "after step", "passive")

	body = renderPart(t, RecipeStep(RecipeStepView{
		Title: "Rest", Text: "Leave it.", Passive: true, Time: "Timer · 1 h", After: []int{2, 3},
	}, "5"))
	has(t, body, `class="step passive"`, `<h4 data-testid="step-title">Rest</h4>`,
		`class="stag t"`, "Timer · 1 h", `data-testid="after"`, "after steps 2, 3")

	// An active step's time tag is not the waiting colour.
	body = renderPart(t, RecipeStep(RecipeStepView{Text: "Chop.", Time: "5 min hands-on"}, "1"))
	has(t, body, `class="stag"`)
	lacks(t, body, `class="stag t"`)
}

func TestStepEscapesRecipeText(t *testing.T) {
	body := renderPart(t, RecipeStep(RecipeStepView{Title: "<b>x</b>", Text: "a < b & c"}, "1"))
	has(t, body, "&lt;b&gt;x&lt;/b&gt;", "a &lt; b &amp; c")
	lacks(t, body, "<b>x</b>")
}

func TestMeanwhileCalloutListsEachItemWithItsStepNumber(t *testing.T) {
	body := renderPart(t, RecipeStep(RecipeStepView{
		Text: "Let it rise.", Passive: true,
		Meanwhile: []display.MeanwhileItem{{Number: 4, Text: "Chop <tomatoes>."}, {Number: 6, Text: "Set the table."}},
	}, "2"))
	has(t, body, `data-testid="meanwhile"`, "Meanwhile, while it waits", "step 4:", "Chop &lt;tomatoes&gt;.", "step 6:", "Set the table.")
	if n := strings.Count(body, "<li><b>step"); n != 2 {
		t.Errorf("callout items = %d, want 2", n)
	}
}

func TestAfterTextSingularAndPlural(t *testing.T) {
	if got := afterText([]int{1}); got != "after step 1" {
		t.Errorf("one = %q", got)
	}
	if got := afterText([]int{3, 4, 5}); got != "after steps 3, 4, 5" {
		t.Errorf("many = %q", got)
	}
}

func TestRecipeStylesAreInlineAndUseOnlyTokens(t *testing.T) {
	css := renderPart(t, RecipeStyles())
	has(t, css, "<style>", ".recipe-controls", ".tip")
	// A literal colour in a page stylesheet is a second palette (frontend rule).
	for _, bad := range []string{"#fff", "#000", "rgb(", "rgba(", "hsl("} {
		if strings.Contains(css, bad) {
			t.Errorf("recipe.css has the literal colour %q; use a token", bad)
		}
	}
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

// A write that fails part-way through a part must reach the caller. The
// padding moves the point where the output buffer fills across the part.
func TestAFailedWriteInARecipePartIsReported(t *testing.T) {
	cal := 300
	for pad := 0; pad < 9000; pad += 37 {
		text := strings.Repeat("x", pad)
		parts := map[string]templ.Component{
			"meta": RecipeMetaRow(RecipeMeta{
				MealType: recipe.MealMain, Meal: "Main", Spice: 2, Spoons: 3, Made: 2, Author: text,
				Calories: &cal, SourceURL: "https://x.test/", SourceHost: "x.test",
			}),
			"step": RecipeStep(RecipeStepView{
				Title: "T", Text: text, Passive: true, Time: "Timer · 1 h", After: []int{1, 2},
				Meanwhile: []display.MeanwhileItem{{Number: 3, Text: "Do it."}},
			}, "2"),
		}
		for name, c := range parts {
			for n := 1; n <= 6; n++ {
				w := &failingWriter{n: n}
				err := c.Render(context.Background(), w)
				if w.calls >= n && err == nil {
					t.Fatalf("%s: write %d failed with %d bytes of text and Render returned nil", name, n, pad)
				}
			}
		}
	}
}

func TestRenderOfACancelledContextStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, c := range map[string]templ.Component{
		"meta": RecipeMetaRow(RecipeMeta{}),
		"step": RecipeStep(RecipeStepView{Text: "x", Meanwhile: []display.MeanwhileItem{{Number: 1, Text: "y"}}}, "1"),
	} {
		var b strings.Builder
		if err := c.Render(ctx, &b); err == nil {
			t.Errorf("%s: a cancelled request rendered anyway", name)
		}
	}
}

func TestNewRecipeStepViewWordsTheTimeByWhoseItIs(t *testing.T) {
	for _, tc := range []struct {
		s    recipe.Step
		want string
	}{
		{recipe.Step{Duration: 25 * time.Minute}, "25 min hands-on"},
		{recipe.Step{Duration: 3 * time.Hour, Passive: true}, "Timer · 3 h"},
		{recipe.Step{Passive: true}, ""},
	} {
		if got := NewRecipeStepView(tc.s, nil, nil).Time; got != tc.want {
			t.Errorf("step %+v: time tag = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestStepShowsWhichPassiveStepsItCanRunDuring(t *testing.T) {
	body := renderPart(t, RecipeStep(RecipeStepView{Text: "Chop.", During: []int{3}}, "4"))
	has(t, body, `class="stag dep"`, `data-testid="during"`, "during step 3")
	body = renderPart(t, RecipeStep(RecipeStepView{Text: "Chop.", During: []int{2, 3}}, "4"))
	has(t, body, "during steps 2, 3")
	lacks(t, renderPart(t, RecipeStep(RecipeStepView{Text: "Stir."}, "1")), "during")
}
