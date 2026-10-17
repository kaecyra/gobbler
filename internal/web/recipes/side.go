package recipes

import (
	"strconv"
	"time"

	"github.com/kaecyra/gobbler/internal/store"
)

// recentLimit is how many made-it entries the "recently made" panel lists.
const recentLimit = 4

// sideView is the library's side panels.
type sideView struct {
	// Suggest is nil when the library is empty.
	Suggest *suggestView
	Recent  []recentView
	// Forgot is nil when nothing has been made.
	Forgot *forgotView
}

// recentView is one entry of the "recently made" panel.
type recentView struct {
	Day, Month string
	Href, Name string
	// Note is the entry's comment, or "Made it" when it has none.
	Note string
}

// forgotView is the "haven't made in a while" panel.
type forgotView struct {
	Href, Name string
	// Text follows the name: when it was last made and what it was rated.
	Text string
}

// homeView is the landing page.
type homeView struct {
	// Empty is true when the household has no recipes.
	Empty   bool
	Suggest suggestView
	Recent  []recentView
}

// suggestView is the home page's suggestion.
type suggestView struct {
	Card  cardView
	Voice string
	When  string
	// NextHref asks for the next idea; empty when there is no other to offer.
	NextHref string
}

func recentViews(entries []store.MadeEntry) []recentView {
	out := make([]recentView, 0, len(entries))
	for _, e := range entries {
		note := "Made it"
		if e.Comment != "" {
			note = "“" + e.Comment + "”"
		}
		out = append(out, recentView{
			Day: strconv.Itoa(e.On.Day()), Month: e.On.Format("Jan"),
			Href: recipeHref(e.RecipeID), Name: e.Name, Note: note,
		})
	}
	return out
}

// forgotFor builds the panel from the idle list, which has the recipe made
// longest ago first. A library where nothing was made has no such recipe.
func forgotFor(idle []store.Card, now time.Time) *forgotView {
	if len(idle) == 0 || idle[0].LastMade == nil {
		return nil
	}
	c := idle[0]
	text := "Last made in " + monthName(*c.LastMade, now) + "."
	if c.Rating != nil && *c.Rating >= 1 {
		text += " You gave it " + spoonWord(*c.Rating) + " " + plural(*c.Rating, "spoon") + "."
	}
	return &forgotView{Href: recipeHref(c.ID), Name: c.Name, Text: text}
}

// suggestFor picks idea number n from the idle list, wrapping round. The idea
// is in the URL, so "Another idea" needs no state on the server.
func suggestFor(idle []store.Card, n int, now time.Time, href func(idea int) string) suggestView {
	i := n % len(idle)
	c := idle[i]
	v := suggestView{Card: cardFor(c, now), Voice: "Haven’t tried this one yet…", When: "Not made yet"}
	if c.LastMade != nil {
		v.Voice, v.When = "Haven’t made this in a while…", "Last made "+ago(*c.LastMade, now)
	}
	if len(idle) > 1 {
		v.NextHref = href((i + 1) % len(idle))
	}
	return v
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// homeHref is the home page showing idea n; the first idea is the bare URL.
func homeHref(n int) string {
	if n == 0 {
		return "/"
	}
	return "/?" + paramIdea + "=" + strconv.Itoa(n)
}
