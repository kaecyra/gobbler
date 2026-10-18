package layout

import (
	_ "embed" // the recipe stylesheet is embedded in the binary (ADR-00002)
	"net/url"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipe/display"
)

// The recipe parts below are shared by the pages that show a recipe document:
// the recipe page and the page of a past version. They sit here, not in either
// feature package, because a feature package does not import another one
// (ADR-00012).

// mealLabels name the meal types on the page, in the library's order.
var mealLabels = map[recipe.MealType]string{
	recipe.MealBreakfast: "Breakfast", recipe.MealMain: "Main", recipe.MealSide: "Side",
	recipe.MealDessert: "Dessert", recipe.MealSnack: "Snack", recipe.MealDrink: "Drink",
}

// heatLabels are indexed by spice level; 0 is nothing to say.
var heatLabels = [...]string{"", "Mild heat", "Medium heat", "Hot"}

// verdicts are the words of the spoon scale, indexed by rating; they are the
// accessible name of the spoons.
var verdicts = [...]string{"Not rated", "Not again", "It was fine", "Good", "Make it again", "House favourite"}

// maxSpoons is the top of the rating scale.
const maxSpoons = 5

//go:embed recipe.css
var recipeStylesheet string

// RecipeStyles is the stylesheet of a recipe document, written into the page.
// It is inline so no page needs a route of its own for it.
func RecipeStyles() templ.Component {
	return templ.Raw("<style>" + recipeStylesheet + "</style>")
}

// RecipeStepView is one step as the recipe page and a past version show it.
type RecipeStepView struct {
	// Title is the step's short heading, empty when it has none.
	Title string
	Text  string
	// Passive marks a step that needs no attention while it runs.
	Passive bool
	// Note is the duration and the passive marker, "20 min, passive".
	Note string
	// After lists the numbers of the steps this one waits for.
	After []int
	// Meanwhile is work offered while this passive step runs (ADR-00004);
	// empty for every other step and for a past version.
	Meanwhile []display.MeanwhileItem
}

// NewRecipeStepView formats one step. number is display.StepNumbers of the
// recipe, which turns each dependency into the number printed on its step;
// meanwhile is the guidance offered while it runs, nil for none.
func NewRecipeStepView(s recipe.Step, number map[recipe.StepID]int, meanwhile []display.MeanwhileItem) RecipeStepView {
	sv := RecipeStepView{Title: s.Title, Text: s.Text, Passive: s.Passive, Note: display.StepNote(s), Meanwhile: meanwhile}
	for _, dep := range s.DependsOn {
		sv.After = append(sv.After, number[dep])
	}
	return sv
}

// RecipeMeta is what sits in the line under a recipe's title: meal type,
// spice, rating, made count, author, calories and source.
type RecipeMeta struct {
	// MealType is the meal type's key and Meal its label, both empty when
	// unset. Spice is 0 to 3 and 0 shows nothing. Spoons is the rating, 0 for
	// unrated. Made is the length of the made-it log; a past version has none.
	MealType   recipe.MealType
	Meal       string
	Cuisine    string // the cuisine's label, empty when unset (ADR-00025)
	Spice      int
	Spoons     int
	Made       int
	Author     string
	Calories   *int
	SourceURL  string
	SourceHost string
}

// NewRecipeMeta reads the fields of r that the meta row shows.
func NewRecipeMeta(r recipe.Recipe) RecipeMeta {
	m := RecipeMeta{
		MealType: r.MealType, Meal: mealLabels[r.MealType], Cuisine: r.Cuisine.Label(), Author: r.Author,
		Calories: r.CaloriesPerServing, SourceURL: r.SourceURL, SourceHost: sourceHost(r.SourceURL),
	}
	if r.SpiceLevel != nil && *r.SpiceLevel > 0 {
		m.Spice = min(*r.SpiceLevel, recipe.MaxSpiceLevel)
	}
	if r.Rating != nil && *r.Rating > 0 {
		m.Spoons = min(*r.Rating, maxSpoons)
	}
	return m
}

// sourceHost is the host of a source address for the link text, empty when the
// address does not parse or has none.
func sourceHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// heatLabel and verdict name the chilis and the spoons for screen readers.
func heatLabel(level int) string { return heatLabels[level] }

func verdict(spoons int) string { return verdicts[spoons] }

// spoonsOn lists the five spoons of the rating, true for the filled ones.
func spoonsOn(n int) []bool {
	out := make([]bool, maxSpoons)
	for i := range out {
		out[i] = i < n
	}
	return out
}

// chilis lists the peppers of the spice scale.
func chilis(n int) []struct{} { return make([]struct{}, n) }

// afterText renders "after step 3" or "after steps 3, 4, 5" for the steps a
// step waits for.
func afterText(nums []int) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = strconv.Itoa(n)
	}
	if len(parts) == 1 {
		return "after step " + parts[0]
	}
	return "after steps " + strings.Join(parts, ", ")
}
