package recipes

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

type fakeLibrary struct {
	got    store.SearchQuery
	cards  []store.Card
	recent []store.MadeEntry
	idle   []store.Card
	err    error
	// reads counts Cards calls.
	reads int
}

func (f *fakeLibrary) Cards(_ context.Context, _ apirecipes.Caller, q store.SearchQuery) ([]store.Card, error) {
	if f.reads == 0 {
		f.got = q // the search itself; later reads are for the side panels
	}
	f.reads++
	return f.cards, f.err
}

func (f *fakeLibrary) RecentlyMade(context.Context, apirecipes.Caller, int) ([]store.MadeEntry, error) {
	return f.recent, f.err
}

func (f *fakeLibrary) Idle(context.Context, apirecipes.Caller) ([]store.Card, error) {
	return f.idle, f.err
}

var today = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func do(t *testing.T, h http.Handler, target string, header map[string]string) (int, string, http.Header) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	return rec.Code, string(body), rec.Header()
}

func get(t *testing.T, lib *fakeLibrary, target string, header map[string]string) (int, string, http.Header) {
	t.Helper()
	return do(t, Routes(Deps{Library: lib, Now: func() time.Time { return today }}), target, header)
}

func home(t *testing.T, lib *fakeLibrary, target string, header map[string]string) (int, string, http.Header) {
	t.Helper()
	return do(t, Home(Deps{Library: lib, Now: func() time.Time { return today }}), target, header)
}

func card(id int64, name string, meal recipe.MealType) store.Card {
	return store.Card{Summary: store.Summary{ID: id, Name: name}, MealType: meal}
}

func TestQueryAndGroupingReachThePage(t *testing.T) {
	lib := &fakeLibrary{cards: []store.Card{card(1, "Soup", recipe.MealMain)}}
	_, body, _ := get(t, lib, "/?q=+soup+&group=time", nil)
	if lib.got.Text != "soup" {
		t.Fatalf("store query = %+v, want trimmed text", lib.got)
	}
	// The grouping survives a new search as a hidden field tied to the form,
	// and the other grouping is one link away with the text kept.
	if !strings.Contains(body, `type="hidden" name="group" value="time"`) {
		t.Error("the grouping is not carried as a hidden field")
	}
	if !strings.Contains(body, `href="/recipes?q=soup"`) {
		t.Errorf("the meal grouping link should keep the text:\n%s", body)
	}
}

func TestCardShowsTheCuisineLabelOnlyWhenSet(t *testing.T) {
	with := card(1, "Katsu", recipe.MealMain)
	with.Cuisine = recipe.CuisineMiddleEastern
	_, body, _ := get(t, &fakeLibrary{cards: []store.Card{with, card(2, "Plain", recipe.MealMain)}}, "/?group=time", nil)
	// The grid only: the side's suggestion is one more card, of Katsu or Plain.
	body, _, _ = strings.Cut(body, "<aside")
	if n := strings.Count(body, `data-testid="card-cuisine"`); n != 1 || !strings.Contains(body, "Middle Eastern") {
		t.Errorf("cuisine lines = %d, want 1 labelled Middle Eastern:\n%s", n, body)
	}
}

func TestCardShowsTheTotalTimeAndNoRibbon(t *testing.T) {
	timed := card(1, "Timed", recipe.MealMain)
	timed.Steps = []recipe.Step{{Duration: 20 * time.Minute}, {Duration: 3 * time.Hour}}
	stated := card(2, "Stated", recipe.MealMain)
	stated.Total = 30 * time.Minute
	none := card(3, "Untimed", recipe.MealMain)
	_, body, _ := get(t, &fakeLibrary{cards: []store.Card{timed, stated, none}}, "/?group=time", nil)
	body, _, _ = strings.Cut(body, "<aside")
	if strings.Contains(body, "data-rib") || strings.Contains(body, "/static/ribbon") {
		t.Errorf("a card carries the ribbon:\n%s", body)
	}
	for _, want := range []string{`title="3 hours 20 minutes"`, "3h 20m", `title="30 minutes"`, "30m"} {
		if !strings.Contains(body, want) {
			t.Errorf("cards lack %q:\n%s", want, body)
		}
	}
	if n := strings.Count(body, `data-testid="card-time"`); n != 2 {
		t.Errorf("time lines = %d, want 2 (the untimed card shows none)", n)
	}
}

func TestEmptyStates(t *testing.T) {
	_, body, _ := get(t, &fakeLibrary{}, "/", nil)
	if !strings.Contains(body, `data-testid="empty-library"`) {
		t.Error("empty library shows no empty state")
	}
	_, body, _ = get(t, &fakeLibrary{}, "/?q=zebra", nil)
	if !strings.Contains(body, `data-testid="no-match"`) || strings.Contains(body, "empty-library") {
		t.Error("empty search should say nothing matched")
	}
}

func TestHTMXRequestForTheResultsGetsOnlyTheResults(t *testing.T) {
	lib := &fakeLibrary{cards: []store.Card{card(1, "Soup", recipe.MealMain)}}
	_, full, _ := get(t, lib, "/?q=soup", nil)
	_, frag, h := get(t, lib, "/?q=soup", map[string]string{"HX-Request": "true", "HX-Target": "results"})
	if !strings.Contains(full, "<html") || strings.Contains(frag, "<html") {
		t.Errorf("full page has <html>: %v, fragment has <html>: %v", strings.Contains(full, "<html"), strings.Contains(frag, "<html"))
	}
	if !strings.Contains(frag, "Soup") {
		t.Error("fragment lacks the results")
	}
	if v := strings.Join(h.Values("Vary"), ","); !strings.Contains(v, "HX-Request") || !strings.Contains(v, "HX-Target") {
		t.Errorf("Vary = %q, want HX-Request and HX-Target", v)
	}
	// The home page's search targets #content and wants the whole page.
	_, whole, _ := get(t, lib, "/?q=soup", map[string]string{"HX-Request": "true", "HX-Target": "content"})
	if !strings.Contains(whole, "<html") {
		t.Error("a request aimed at the content must get the whole page")
	}
	_, boosted, _ := get(t, lib, "/", map[string]string{"HX-Request": "true", "HX-Target": "results", "HX-Boosted": "true"})
	if !strings.Contains(boosted, "<html") {
		t.Error("a boosted request must get the whole page")
	}
}

func TestHistoryRestoreRequestGetsTheWholePage(t *testing.T) {
	// On a history-cache miss htmx asks for the URL again and swaps the answer
	// into <body>, so a fragment would leave a page with no form or layout.
	_, body, _ := get(t, &fakeLibrary{}, "/?q=soup", map[string]string{"HX-Request": "true", "HX-Target": "results", "HX-History-Restore-Request": "true"})
	if !strings.Contains(body, "<html") || !strings.Contains(body, `id="recipe-search"`) {
		t.Error("a history-restore request must get the whole page")
	}
}

func TestTextIsEscaped(t *testing.T) {
	_, body, _ := get(t, &fakeLibrary{}, `/?q="><script>x</script>`, nil)
	if strings.Contains(body, "<script>x") {
		t.Error("query echoed unescaped")
	}
}

func TestOverlongQueryIsRejected(t *testing.T) {
	code, _, _ := get(t, &fakeLibrary{}, "/?q="+strings.Repeat("a", maxQueryRunes+1), nil)
	if code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", code)
	}
}

func TestStoreFailureIsAServerError(t *testing.T) {
	boom := &fakeLibrary{err: errors.New("disk on fire")}
	for name, run := range map[string]func() (int, string, http.Header){
		"library": func() (int, string, http.Header) { return get(t, boom, "/", nil) },
		"home":    func() (int, string, http.Header) { return home(t, boom, "/", nil) },
	} {
		code, body, _ := run()
		if code != http.StatusInternalServerError || strings.Contains(body, "disk on fire") {
			t.Errorf("%s: status %d, body leaks cause: %v", name, code, strings.Contains(body, "disk on fire"))
		}
	}
}

func TestGroupingByMealAndByTime(t *testing.T) {
	steps := func(m int) []recipe.Step { return []recipe.Step{{Duration: time.Duration(m) * time.Minute}} }
	quick, mid, plan, none := card(1, "Quick", recipe.MealMain), card(2, "Mid", recipe.MealDessert), card(3, "Plan", recipe.MealMain), card(4, "Untimed", "")
	quick.Steps, mid.Steps, plan.Steps = steps(45), steps(46), steps(181)
	none.Total = 0
	found := []store.Card{quick, mid, plan, none}

	titles := func(gs []groupView) string {
		var out []string
		for _, g := range gs {
			names := ""
			for _, c := range g.Cards {
				names += c.Name + ","
			}
			out = append(out, g.Title+":"+names)
		}
		return strings.Join(out, " | ")
	}
	if got, want := titles(buildResults("", "meal", found, today).Groups), "Mains:Quick,Plan, | Desserts:Mid, | Other:Untimed,"; got != want {
		t.Errorf("by meal = %q, want %q", got, want)
	}
	// A bad grouping falls back to meal rather than failing the page.
	if got := buildResults("", "bogus", found, today).Group; got != groupMeal {
		t.Errorf("unknown grouping = %q, want meal", got)
	}
	want := "Under 45 minutes:Quick, | Up to 3 hours:Mid, | Plan ahead:Plan, | No time set:Untimed,"
	if got := titles(buildResults("", "time", found, today).Groups); got != want {
		t.Errorf("by time = %q, want %q", got, want)
	}
}

func TestTimeToTableFallsBackToTheStatedTotal(t *testing.T) {
	c := card(1, "Stated", recipe.MealMain)
	c.Total = 30 * time.Minute
	if got := timeToTable(c); got != 30*time.Minute {
		t.Errorf("no timed steps: %v, want the stated total", got)
	}
	c.Steps = []recipe.Step{{Duration: time.Hour}}
	if got := timeToTable(c); got != time.Hour {
		t.Errorf("timed steps: %v, want their sum", got)
	}
}

func TestHomeSuggestionCyclesAndValidatesTheIdea(t *testing.T) {
	last := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	a, b := card(1, "Old soup", recipe.MealMain), card(2, "Never cake", recipe.MealDessert)
	a.LastMade = &last
	lib := &fakeLibrary{idle: []store.Card{a, b}}

	// Never made comes first; home has no forgot panel, so the recipe made
	// longest ago is the next idea rather than skipped.
	_, body, _ := home(t, lib, "/", nil)
	if !strings.Contains(body, "Never cake") || !strings.Contains(body, "Not made yet") || !strings.Contains(body, `href="/?idea=1"`) {
		t.Errorf("first idea:\n%s", body)
	}
	_, body, _ = home(t, lib, "/?idea=1", nil)
	if !strings.Contains(body, "Old soup") || !strings.Contains(body, "Last made 7 months ago") || !strings.Contains(body, `href="/"`) {
		t.Errorf("second idea should wrap back to the bare URL:\n%s", body)
	}
	_, body, _ = home(t, lib, "/?idea=2", nil)
	if !strings.Contains(body, "Never cake") {
		t.Error("an idea past the end should wrap round")
	}
	for _, bad := range []string{"-1", "x", "99999999999"} {
		if code, _, _ := home(t, lib, "/?idea="+bad, nil); code != http.StatusBadRequest {
			t.Errorf("idea=%s status %d, want 400", bad, code)
		}
	}
	_, frag, _ := home(t, lib, "/?idea=1", map[string]string{"HX-Request": "true", "HX-Target": "suggest"})
	if strings.Contains(frag, "<html") || !strings.Contains(frag, `id="suggest"`) {
		t.Errorf("another idea should get just the suggestion:\n%s", frag)
	}
}

func TestHomeOffersNoOtherIdeaWhenThereIsOneRecipe(t *testing.T) {
	_, body, _ := home(t, &fakeLibrary{idle: []store.Card{card(1, "Only", "")}}, "/", nil)
	if strings.Contains(body, "Another idea") {
		t.Error("a library of one has no other idea to offer")
	}
}

func TestHomeOfAnEmptyLibraryShowsTheEmptyState(t *testing.T) {
	_, body, _ := home(t, &fakeLibrary{}, "/", nil)
	if !strings.Contains(body, `data-testid="empty-library"`) || strings.Contains(body, `id="suggest"`) {
		t.Error("an empty library has an empty state and no suggestion")
	}
}

func TestPageStyleIsRenderedAndUsesTokens(t *testing.T) {
	_, body, _ := get(t, &fakeLibrary{}, "/", nil)
	if !strings.Contains(body, ".rcard {") || strings.Contains(body, "templ.Raw") {
		t.Error("the page's stylesheet is not in the output")
	}
	for _, literal := range []string{"#", "rgb(", "rgba(", "hsl("} {
		if strings.Contains(libraryStyle, literal) {
			t.Errorf("libraryStyle holds a literal colour (%q); use the tokens", literal)
		}
	}
}

func TestFormatting(t *testing.T) {
	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }
	madeAt := d(2026, 10, 10)
	for _, tc := range []struct{ got, want string }{
		{madeLine(0, nil, today), "Not made yet"},
		{madeLine(6, &madeAt, today), "Made 6× · 10 Oct"},
		{shortDate(d(2025, 3, 4), today), "Mar 2025"},
		{monthName(d(2026, 3, 4), today), "March"},
		{monthName(d(2025, 3, 4), today), "March 2025"},
		{headingFor(1, ""), "1 recipe"},
		{headingFor(2, "lemon"), "2 recipes for “lemon”"},
		{listHref("a b", groupTime), "/recipes?group=time&q=a+b"},
		{listHref("", groupMeal), "/recipes"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

func TestLibraryShowsTheSuggestionAndKeepsTheSearchOnAnotherIdea(t *testing.T) {
	older, last := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	f, a, b := card(3, "Older stew", recipe.MealMain), card(1, "Old soup", recipe.MealMain), card(2, "Never cake", recipe.MealDessert)
	f.LastMade, a.LastMade = &older, &last
	lib := &fakeLibrary{cards: []store.Card{b, a, f}}

	_, body, _ := get(t, lib, "/?q=cake&group=time", nil)
	if !strings.Contains(body, `id="suggest"`) || !strings.Contains(body, "Never cake") {
		t.Errorf("the library aside lacks the suggestion:\n%s", body)
	}
	if !strings.Contains(body, `href="/recipes?group=time&amp;idea=1&amp;q=cake"`) {
		t.Errorf("Another idea should stay on the library page and keep q and group:\n%s", body)
	}
	_, frag, _ := get(t, lib, "/?q=cake&idea=1", map[string]string{"HX-Request": "true", "HX-Target": "suggest"})
	if strings.Contains(frag, "<html") || !strings.Contains(frag, "Old soup") {
		t.Errorf("another idea should get just the next suggestion:\n%s", frag)
	}
	// The forgot panel's recipe is never also the suggestion on the same page.
	// Two ideas, so idea 2 wraps to the first; the forgot recipe is never one.
	_, frag, _ = get(t, lib, "/?idea=2", map[string]string{"HX-Request": "true", "HX-Target": "suggest"})
	if strings.Contains(frag, "Older stew") || !strings.Contains(frag, "Never cake") {
		t.Errorf("idea 2 should wrap to Never cake and never repeat the forgot panel:\n%s", frag)
	}
	if code, _, _ := get(t, lib, "/?idea=x", nil); code != http.StatusBadRequest {
		t.Errorf("bad idea status %d, want 400", code)
	}
}

func TestIdeasAndForgotPick(t *testing.T) {
	// A made-it date is a plain date at midnight UTC, as the store returns it,
	// while today is noon: a count by duration would put the boundary a day off.
	day := func(daysAgo int) *time.Time {
		y, m, d := today.AddDate(0, 0, -daysAgo).Date()
		t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		return &t
	}
	made := func(id int64, name string, daysAgo int) store.Card {
		c := card(id, name, "")
		c.LastMade = day(daysAgo)
		return c
	}
	names := func(cs []store.Card) string {
		var out []string
		for _, c := range cs {
			out = append(out, c.Name)
		}
		return strings.Join(out, ",")
	}
	for _, tc := range []struct {
		name       string
		idle       []store.Card // in store.IdleOrder
		forgot     string       // the pick, "" for none
		home, page string       // ideas without and with the forgot skip
	}{
		{"empty", nil, "", "", ""},
		{"never made only", []store.Card{card(1, "A", ""), card(2, "B", "")}, "", "A,B", "A,B"},
		{"never made first, then idle oldest first",
			[]store.Card{made(1, "Old", 200), made(2, "Mid", 60), card(3, "New", "")}, "Old", "New,Old,Mid", "New,Mid"},
		{"made in the last 30 days is left out",
			[]store.Card{made(1, "Thirty", 30), made(2, "Two", 2)}, "", "", ""},
		{"31 days counts as a while", []store.Card{made(1, "Month", 31)}, "Month", "Month", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pick := forgotPick(tc.idle, today)
			got := ""
			if pick != nil {
				got = pick.Name
			}
			if got != tc.forgot {
				t.Errorf("forgot = %q, want %q", got, tc.forgot)
			}
			if g := names(ideasFrom(tc.idle, today, nil)); g != tc.home {
				t.Errorf("home ideas = %q, want %q", g, tc.home)
			}
			if g := names(ideasFrom(tc.idle, today, pick)); g != tc.page {
				t.Errorf("library ideas = %q, want %q", g, tc.page)
			}
		})
	}
}

func TestPanelsWithNothingToShowAreDropped(t *testing.T) {
	recent := func(daysAgo int) store.Card {
		c := card(1, "Fresh soup", recipe.MealMain)
		d := today.AddDate(0, 0, -daysAgo)
		c.LastMade = &d
		return c
	}
	// Recipes, none made: no Recently made, no forgot, but a suggestion.
	_, body, _ := home(t, &fakeLibrary{idle: []store.Card{card(1, "Soup", "")}}, "/", nil)
	if strings.Contains(body, `data-testid="recently-made"`) || !strings.Contains(body, `id="suggest"`) {
		t.Errorf("home with nothing made:\n%s", body)
	}
	// Everything made this week, with entries: Recently made alone.
	made := []store.MadeEntry{{RecipeID: 1, Name: "Fresh soup", On: today.AddDate(0, 0, -2)}}
	_, body, _ = home(t, &fakeLibrary{idle: []store.Card{recent(2)}, recent: made}, "/", nil)
	if strings.Contains(body, `id="suggest"`) || !strings.Contains(body, `data-testid="recently-made"`) || !strings.Contains(body, `class="lib home"`) {
		t.Errorf("home with only Recently made to show:\n%s", body)
	}
	// Made this week and no entries to list: no panel, so no grid.
	_, body, _ = home(t, &fakeLibrary{idle: []store.Card{recent(2)}}, "/", nil)
	if strings.Contains(body, `id="suggest"`) || strings.Contains(body, `class="lib home"`) {
		t.Errorf("home with no panel to show still draws the grid:\n%s", body)
	}
	// "Another idea" with none left answers with nothing, on both pages, so
	// the card is removed rather than a page swapped into it.
	htmx := map[string]string{"HX-Request": "true", "HX-Target": "suggest"}
	if code, frag, _ := home(t, &fakeLibrary{idle: []store.Card{recent(2)}}, "/?idea=1", htmx); code != http.StatusOK || frag != "" {
		t.Errorf("home, no idea left: %d %q, want an empty 200", code, frag)
	}
	if code, frag, _ := get(t, &fakeLibrary{cards: []store.Card{recent(2)}}, "/?idea=1", htmx); code != http.StatusOK || frag != "" {
		t.Errorf("library, no idea left: %d %q, want an empty 200", code, frag)
	}
	// An empty library has no side at all.
	_, body, _ = get(t, &fakeLibrary{}, "/", nil)
	if strings.Contains(body, `<aside`) || !strings.Contains(body, `class="lib solo"`) {
		t.Errorf("an empty library keeps its side:\n%s", body)
	}
	_, body, _ = home(t, &fakeLibrary{}, "/", nil)
	if strings.Contains(body, `data-testid="recently-made"`) || strings.Contains(body, `class="lib home"`) {
		t.Errorf("home of an empty library draws its panels:\n%s", body)
	}
}

func TestForgotPanelShowsItsSpoons(t *testing.T) {
	c := card(1, "Lemon cake", recipe.MealDessert)
	d, four := time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC), 4
	c.LastMade, c.Rating = &d, &four
	_, body, _ := get(t, &fakeLibrary{cards: []store.Card{c}}, "/", nil)
	i := strings.Index(body, `data-testid="forgot"`)
	if i < 0 {
		t.Fatalf("no forgot panel:\n%s", body)
	}
	panel := body[i:]
	end := strings.Index(panel, "</aside>")
	if end < 0 {
		t.Fatalf("the forgot panel is not closed by the aside:\n%s", body)
	}
	panel = panel[:end]
	if !strings.Contains(panel, "Last made in April.") || !strings.Contains(panel, `class="spoons"`) || strings.Contains(panel, "You gave it") {
		t.Errorf("forgot panel:\n%s", panel)
	}
}

func TestLibraryReadsTheCardsOnceWhenNothingNarrowsThem(t *testing.T) {
	lib := &fakeLibrary{cards: []store.Card{card(1, "Soup", recipe.MealMain)}}
	get(t, lib, "/", nil)
	if lib.reads != 1 {
		t.Errorf("Cards called %d times for the whole library, want once", lib.reads)
	}
}

// The library lists baked after sides and before desserts, the order of
// recipe.MealTypes, under the plural the type itself names.
func TestGroupByMealListsBakedAfterSides(t *testing.T) {
	today := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	found := []store.Card{
		card(1, "Cake", recipe.MealDessert), card(2, "Loaf", recipe.MealBaked), card(3, "Slaw", recipe.MealSide), card(4, "Roast", recipe.MealMain),
	}
	var got []string
	for _, g := range buildResults("", "meal", found, today).Groups {
		got = append(got, g.Title)
	}
	if want := "Mains,Sides,Baked,Desserts"; strings.Join(got, ",") != want {
		t.Errorf("group order = %v, want %s", got, want)
	}
}
