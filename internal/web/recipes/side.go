package recipes

import (
	"strconv"
	"time"

	"github.com/kaecyra/gobbler/internal/store"
)

// recentLimit is how many made-it entries the "recently made" panel lists.
const recentLimit = 4

// idleAfterDays is how many calendar days since a recipe was last made before
// it counts as not made in a while (docs/ui/design.md, Library side panels). A
// recipe made more recently is neither suggested nor named by the forgot panel.
const idleAfterDays = 30

// sideView is the library's side panels.
type sideView struct {
	// Suggest is nil when no recipe is left to suggest.
	Suggest *suggestView
	Recent  []recentView
	// Forgot is nil when nothing was made more than idleAfterDays ago.
	Forgot *forgotView
}

// empty reports a side with no panel to draw, so the page can drop it.
func (s sideView) empty() bool { return s.Suggest == nil && len(s.Recent) == 0 && s.Forgot == nil }

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
	// Text follows the name: when it was last made.
	Text string
	// Rating is drawn as spoons; 0 draws none.
	Rating      int
	RatingLabel string
}

// homeView is the landing page.
type homeView struct {
	// Empty is true when the household has no recipes.
	Empty bool
	// Suggest is nil when no recipe is left to suggest.
	Suggest *suggestView
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

// idleFor reports a recipe last made more than idleAfterDays calendar days
// before now. The made-it date is a plain date, so the days are counted as
// ago() counts them: the hour and now's zone never move the boundary.
func idleFor(c store.Card, now time.Time) bool {
	return c.LastMade != nil && calendarDays(*c.LastMade, now) > idleAfterDays
}

// forgotPick is the recipe the forgot panel names: from the idle list (see
// store.IdleOrder), the one made longest ago, if that was a while ago. It is
// nil when no recipe was.
func forgotPick(idle []store.Card, now time.Time) *store.Card {
	if len(idle) == 0 || !idleFor(idle[0], now) {
		return nil
	}
	return &idle[0]
}

// forgotFor builds the panel from its pick; nil draws no panel.
func forgotFor(c *store.Card, now time.Time) *forgotView {
	if c == nil {
		return nil
	}
	card := cardFor(*c, now)
	return &forgotView{
		Href: card.Href, Name: card.Name, Text: "Last made in " + monthName(*c.LastMade, now) + ".",
		Rating: card.Rating, RatingLabel: card.RatingLabel,
	}
}

// ideasFrom is what a suggestion draws from, in order: recipes never made, by
// name, then those not made in a while, made longest ago first. idle is in
// store.IdleOrder. A recipe made in the last idleAfterDays is left out, and so is
// skip, the recipe the forgot panel already names on the same page (nil when
// the page has none).
func ideasFrom(idle []store.Card, now time.Time, skip *store.Card) []store.Card {
	var never, made []store.Card
	for _, c := range idle {
		switch {
		case skip != nil && c.ID == skip.ID:
		case c.LastMade == nil:
			never = append(never, c)
		case idleFor(c, now):
			made = append(made, c)
		}
	}
	return append(never, made...)
}

// suggestFor picks idea number n from ideas, wrapping round; nil when there
// are none. The idea is in the URL, so "Another idea" needs no state on the
// server.
func suggestFor(ideas []store.Card, n int, now time.Time, href func(idea int) string) *suggestView {
	if len(ideas) == 0 {
		return nil
	}
	i := n % len(ideas)
	c := ideas[i]
	v := suggestView{Card: cardFor(c, now), Voice: "Haven’t tried this one yet…", When: "Not made yet"}
	if c.LastMade != nil {
		v.Voice, v.When = "Haven’t made this in a while…", "Last made "+ago(*c.LastMade, now)
	}
	if len(ideas) > 1 {
		v.NextHref = href((i + 1) % len(ideas))
	}
	return &v
}

// homeHref is the home page showing idea n; the first idea is the bare URL.
func homeHref(n int) string {
	if n == 0 {
		return "/"
	}
	return "/?" + paramIdea + "=" + strconv.Itoa(n)
}
