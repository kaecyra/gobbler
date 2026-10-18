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

func (f *fakeLibrary) Cards(_ context.Context, q store.SearchQuery) ([]store.Card, error) {
	if f.reads == 0 {
		f.got = q // the search itself; later reads are for the side panels
	}
	f.reads++
	return f.cards, f.err
}

func (f *fakeLibrary) RecentlyMade(context.Context, int) ([]store.MadeEntry, error) {
	return f.recent, f.err
}

func (f *fakeLibrary) Idle(context.Context) ([]store.Card, error) { return f.idle, f.err }

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
	if n := strings.Count(body, `data-testid="card-cuisine"`); n != 1 || !strings.Contains(body, "Middle Eastern") {
		t.Errorf("cuisine lines = %d, want 1 labelled Middle Eastern:\n%s", n, body)
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

	_, body, _ := home(t, lib, "/", nil)
	if !strings.Contains(body, "Old soup") || !strings.Contains(body, "Last made 7 months ago") || !strings.Contains(body, `href="/?idea=1"`) {
		t.Errorf("first idea:\n%s", body)
	}
	_, body, _ = home(t, lib, "/?idea=1", nil)
	if !strings.Contains(body, "Never cake") || !strings.Contains(body, "Not made yet") || !strings.Contains(body, `href="/"`) {
		t.Errorf("second idea should wrap back to the bare URL:\n%s", body)
	}
	_, body, _ = home(t, lib, "/?idea=3", nil)
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
		{ago(today, today), "today"},
		{ago(d(2026, 10, 16), today), "yesterday"},
		{ago(d(2026, 10, 10), today), "7 days ago"},
		{ago(d(2026, 9, 1), today), "6 weeks ago"},
		{ago(d(2026, 3, 2), today), "7 months ago"},
		{ago(d(2025, 8, 1), today), "a year ago"},
		{ago(d(2023, 1, 1), today), "3 years ago"},
		{spoonWord(4), "four"},
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

// A made-it date is a plain calendar date (stored and parsed as UTC midnight);
// "today" is the calendar date in now's own zone. The difference must not
// depend on the hour or on how far now's zone sits from UTC.
func TestAgoCountsCalendarDays(t *testing.T) {
	toronto := time.FixedZone("EDT", -4*3600)
	tokyo := time.FixedZone("JST", 9*3600)
	made := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		now  time.Time
	}{
		{"toronto 00:30", time.Date(2026, 10, 18, 0, 30, 0, 0, toronto)},
		{"toronto 12:00", time.Date(2026, 10, 18, 12, 0, 0, 0, toronto)},
		{"toronto 23:30 (already the 19th in UTC)", time.Date(2026, 10, 18, 23, 30, 0, 0, toronto)},
		{"utc 00:30", time.Date(2026, 10, 18, 0, 30, 0, 0, time.UTC)},
		{"utc 23:30", time.Date(2026, 10, 18, 23, 30, 0, 0, time.UTC)},
		{"tokyo 00:30 (still the 17th in UTC)", time.Date(2026, 10, 18, 0, 30, 0, 0, tokyo)},
		{"tokyo 23:30", time.Date(2026, 10, 18, 23, 30, 0, 0, tokyo)},
	} {
		if got := ago(made, tc.now); got != "2 days ago" {
			t.Errorf("%s: ago = %q, want %q", tc.name, got, "2 days ago")
		}
	}
	if got := ago(made, time.Date(2026, 10, 16, 23, 59, 0, 0, toronto)); got != "today" {
		t.Errorf("same calendar day late evening: %q, want today", got)
	}
	if got := ago(made, time.Date(2026, 10, 17, 0, 1, 0, 0, tokyo)); got != "yesterday" {
		t.Errorf("just past midnight: %q, want yesterday", got)
	}
}

func TestLibraryShowsTheSuggestionAndKeepsTheSearchOnAnotherIdea(t *testing.T) {
	last := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	a, b := card(1, "Old soup", recipe.MealMain), card(2, "Never cake", recipe.MealDessert)
	a.LastMade = &last
	lib := &fakeLibrary{cards: []store.Card{b, a}}

	_, body, _ := get(t, lib, "/?q=cake&group=time", nil)
	if !strings.Contains(body, `id="suggest"`) || !strings.Contains(body, "Old soup") {
		t.Errorf("the library aside lacks the suggestion:\n%s", body)
	}
	if !strings.Contains(body, `href="/recipes?group=time&amp;idea=1&amp;q=cake"`) {
		t.Errorf("Another idea should stay on the library page and keep q and group:\n%s", body)
	}
	_, frag, _ := get(t, lib, "/?q=cake&idea=1", map[string]string{"HX-Request": "true", "HX-Target": "suggest"})
	if strings.Contains(frag, "<html") || !strings.Contains(frag, "Never cake") {
		t.Errorf("another idea should get just the next suggestion:\n%s", frag)
	}
	if code, _, _ := get(t, lib, "/?idea=x", nil); code != http.StatusBadRequest {
		t.Errorf("bad idea status %d, want 400", code)
	}
}

func TestLibraryReadsTheCardsOnceWhenNothingNarrowsThem(t *testing.T) {
	lib := &fakeLibrary{cards: []store.Card{card(1, "Soup", recipe.MealMain)}}
	get(t, lib, "/", nil)
	if lib.reads != 1 {
		t.Errorf("Cards called %d times for the whole library, want once", lib.reads)
	}
}
