package recipes

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/ribbon"
)

// Grouping of the library: by meal type, or by the time a recipe takes to reach
// the table. The choice is the group query parameter, so it lives in the URL.
const (
	groupMeal = "meal"
	groupTime = "time"
)

// cardView is a library card, already formatted.
type cardView struct {
	Href       string
	Name       string
	Source     string
	Meal       recipe.MealType // empty when unset
	MealLabel  string
	Cuisine    string // the cuisine's label; empty when unset
	Spice      int    // chilis to draw; 0 draws none
	SpiceLabel string
	Components []string
	// Key is the key ingredients; the first is emphasised.
	Key         []string
	Rating      int // spoons, 0 when unrated
	RatingLabel string
	Made        string
	Ribbon      ribbon.Ribbon
}

// groupView is a heading and the cards under it.
type groupView struct {
	Title string
	// Meal is set on a meal group that has an icon.
	Meal  recipe.MealType
	Cards []cardView
}

// results is what the library's results area shows.
type results struct {
	Query string
	Group string
	// Heading says how many recipes are listed, and for what.
	Heading string
	Groups  []groupView
	// SwitchHref are the group-by links.
	MealHref, TimeHref string
	// Filtered is true when a query narrowed the list, which is what tells an
	// empty library from a search with no match.
	Filtered bool
}

// mealLabels name a meal type singly, as a card shows it; mealPlurals name it
// as a group heading.
var (
	mealLabels = map[recipe.MealType]string{
		recipe.MealBreakfast: "Breakfast", recipe.MealMain: "Main", recipe.MealSide: "Side",
		recipe.MealDessert: "Dessert", recipe.MealSnack: "Snack", recipe.MealDrink: "Drink",
	}
	mealPlurals = map[recipe.MealType]string{
		recipe.MealBreakfast: "Breakfast", recipe.MealMain: "Mains", recipe.MealSide: "Sides",
		recipe.MealDessert: "Desserts", recipe.MealSnack: "Snacks", recipe.MealDrink: "Drinks",
	}
)

// heatLabels name 1 to 3 chilis; verdicts name 1 to 5 spoons.
var (
	heatLabels = [...]string{"", "Mild heat", "Medium heat", "Hot"}
	verdicts   = [...]string{"Not rated", "Not again", "It was fine", "Good", "Make it again", "House favourite"}
)

// timeBands are the "time to table" groups, shortest first; the last has no
// upper bound. A recipe with no time at all is in noTimeTitle.
var timeBands = []struct {
	Title string
	Upto  time.Duration
}{
	{"Under 45 minutes", 45 * time.Minute},
	{"Up to 3 hours", 3 * time.Hour},
	{"Plan ahead", 0},
}

const noTimeTitle = "No time set"

func cardFor(c store.Card, now time.Time) cardView {
	v := cardView{
		Href: recipeHref(c.ID), Name: c.Name, Source: c.Source, Meal: c.MealType, MealLabel: mealLabels[c.MealType], Cuisine: c.Cuisine.Label(),
		Components: c.Components, Key: c.KeyIngredients,
		Made:   madeLine(c.MadeCount, c.LastMade, now),
		Ribbon: ribbon.Build(c.Steps),
	}
	if c.SpiceLevel != nil && *c.SpiceLevel >= 1 && *c.SpiceLevel < len(heatLabels) {
		v.Spice, v.SpiceLabel = *c.SpiceLevel, heatLabels[*c.SpiceLevel]
	}
	if c.Rating != nil && *c.Rating >= 1 && *c.Rating < len(verdicts) {
		v.Rating, v.RatingLabel = *c.Rating, verdicts[*c.Rating]
	}
	return v
}

// timeToTable is how long a recipe takes: its timed steps added up, else the
// total time the source stated.
func timeToTable(c store.Card) time.Duration {
	if t := ribbon.Build(c.Steps).Total; t > 0 {
		return t
	}
	return c.Total
}

// buildResults groups the found cards. An unknown group falls back to meal.
func buildResults(q, group string, found []store.Card, now time.Time) results {
	if group != groupTime {
		group = groupMeal
	}
	res := results{Query: q, Group: group, Filtered: q != "", MealHref: listHref(q, groupMeal), TimeHref: listHref(q, groupTime)}
	res.Heading = headingFor(len(found), q)
	if group == groupTime {
		res.Groups = groupByTime(found, now)
	} else {
		res.Groups = groupByMeal(found, now)
	}
	return res
}

func groupByMeal(found []store.Card, now time.Time) []groupView {
	var out []groupView
	for _, m := range append(recipe.MealTypes(), "") {
		g := groupView{Title: "Other", Meal: m}
		if m != "" {
			g.Title = mealPlurals[m]
		}
		for _, c := range found {
			if c.MealType == m {
				g.Cards = append(g.Cards, cardFor(c, now))
			}
		}
		if len(g.Cards) > 0 {
			out = append(out, g)
		}
	}
	return out
}

func groupByTime(found []store.Card, now time.Time) []groupView {
	groups := make([]groupView, len(timeBands)+1)
	for i, b := range timeBands {
		groups[i].Title = b.Title
	}
	groups[len(timeBands)].Title = noTimeTitle
	for _, c := range found {
		i := len(timeBands) // no time
		if t := timeToTable(c); t > 0 {
			i = len(timeBands) - 1
			for j, b := range timeBands {
				if b.Upto > 0 && t <= b.Upto {
					i = j
					break
				}
			}
		}
		groups[i].Cards = append(groups[i].Cards, cardFor(c, now))
	}
	var out []groupView
	for _, g := range groups {
		if len(g.Cards) > 0 {
			out = append(out, g)
		}
	}
	return out
}

func headingFor(n int, q string) string {
	noun := " recipes"
	if n == 1 {
		noun = " recipe"
	}
	if q == "" {
		return strconv.Itoa(n) + noun
	}
	return strconv.Itoa(n) + noun + " for “" + q + "”"
}

// listHref is the library page for a search and grouping: the URL that
// reproduces the results, used by the group-by links and the address bar. The
// default grouping is left out.
func listHref(q, group string) string {
	vals := url.Values{}
	if q != "" {
		vals.Set(paramText, q)
	}
	if group == groupTime {
		vals.Set(paramGroup, groupTime)
	}
	if len(vals) == 0 {
		return Pattern
	}
	return Pattern + "?" + vals.Encode()
}

// ideaHref is the library page showing suggestion n for a search and grouping;
// the first idea is left out.
func ideaHref(q, group string) func(int) string {
	return func(n int) string {
		vals := url.Values{}
		if q != "" {
			vals.Set(paramText, q)
		}
		if group == groupTime {
			vals.Set(paramGroup, groupTime)
		}
		if n > 0 {
			vals.Set(paramIdea, strconv.Itoa(n))
		}
		if len(vals) == 0 {
			return Pattern
		}
		return Pattern + "?" + vals.Encode()
	}
}

// madeLine is the card footer's account of the made-it log: "Not made yet", or
// "Made 6× · 10 Oct".
func madeLine(count int, last *time.Time, now time.Time) string {
	if count == 0 || last == nil {
		return "Not made yet"
	}
	return "Made " + strconv.Itoa(count) + "× · " + shortDate(*last, now)
}

// shortDate is "10 Oct" in the current year and "Oct 2025" in any other, so a
// date a year ago is not mistaken for this year's.
func shortDate(t, now time.Time) string {
	if t.Year() == now.Year() {
		return t.Format("2 Jan")
	}
	return t.Format("Jan 2006")
}

// monthName is "March" in the current year and "March 2025" in any other.
func monthName(t, now time.Time) string {
	if t.Year() == now.Year() {
		return t.Format("January")
	}
	return t.Format("January 2006")
}

// calendarDays counts the calendar days from t to now. t is a made-it date
// (a plain calendar date); today is now's date in now's own zone. Both are
// reduced to a date before subtracting, so the hour and the distance of now's
// zone from UTC never change the answer.
func calendarDays(t, now time.Time) int {
	date := func(t time.Time) time.Time {
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	return int(date(now).Sub(date(t)).Hours() / 24)
}

// ago says how long before now a date was, in the largest whole unit.
func ago(t, now time.Time) string {
	days := calendarDays(t, now)
	switch {
	case days < 1:
		return "today"
	case days == 1:
		return "yesterday"
	case days < 14:
		return strconv.Itoa(days) + " days ago"
	case days < 60:
		return strconv.Itoa(days/7) + " weeks ago"
	case days < 365:
		return strconv.Itoa(days/30) + " months ago"
	case days < 2*365:
		return "a year ago"
	}
	return strconv.Itoa(days/365) + " years ago"
}

// spoonWord spells a rating for the "You gave it ..." line.
func spoonWord(n int) string {
	words := [...]string{"no", "one", "two", "three", "four", "five"}
	if n < 0 || n >= len(words) {
		return strconv.Itoa(n)
	}
	return words[n]
}

func recipeHref(id int64) string { return "/recipes/" + strconv.FormatInt(id, 10) }

func itoa(n int) string { return strconv.Itoa(n) }

// join is the key ingredients after the first, as a sentence tail.
func join(parts []string) string { return strings.Join(parts, ", ") }
