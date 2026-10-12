package shopping

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	domain "github.com/kaecyra/gobbler/internal/shopping"
	"github.com/kaecyra/gobbler/internal/store"
)

type fixture struct {
	t     *testing.T
	d     *sql.DB
	cat   *catalog.Store
	rec   *store.Store
	shop  *domain.Store
	h     *handler
	srv   http.Handler
	flour int64
	milk  int64
	salt  int64
	cakes int64 // pancakes: 1 cup flour, 1 cup milk, salt
	waffl int64 // waffles: 2 cup flour
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	cat, rec := catalog.New(d), store.New(d)
	shop := domain.New(d, rec, cat)
	f := &fixture{t: t, d: d, cat: cat, rec: rec, shop: shop}
	deps := Deps{Lists: shop, Recipes: rec, Catalog: cat}
	f.h = newHandler(deps)
	shop.OnChange(f.h.hub.notify)
	root := chi.NewRouter()
	root.Mount(Pattern, f.h.routes())
	f.srv = root

	f.flour, f.milk, f.salt = f.ingredient("zz flour", "baking"), f.ingredient("zz milk", "dairy"), f.ingredient("zz salt", "spices")
	cup := quantity.MustUnit("cup")
	f.cakes = f.recipe("Zz Pancakes",
		f.line(f.flour, 1, cup, false), f.line(f.milk, 1, cup, false), f.line(f.salt, 0, quantity.Unit{}, false))
	f.waffl = f.recipe("Zz Waffles", f.line(f.flour, 2, cup, false))
	return f
}

func (f *fixture) ingredient(name, aisle string) int64 {
	f.t.Helper()
	in, err := f.cat.CreateIngredient(context.Background(), catalog.Ingredient{Name: name, Aisle: aisle})
	if err != nil {
		f.t.Fatal(err)
	}
	return in.ID
}

func (f *fixture) line(ing int64, n int64, unit quantity.Unit, optional bool) recipe.IngredientLine {
	l := recipe.IngredientLine{Ingredient: recipe.IngredientID(ing), Rule: quantity.RuleLinear, Raw: "zz line", Optional: optional}
	if n > 0 {
		l.Amount = quantity.Amount{Qty: quantity.Exact(quantity.Int(n)), Unit: unit}
	}
	return l
}

func (f *fixture) recipe(name string, lines ...recipe.IngredientLine) int64 {
	f.t.Helper()
	id, err := f.rec.Create(identityContext(f.t), recipe.Recipe{
		Name:       name,
		Components: []recipe.Component{{Lines: lines, Steps: []recipe.Step{{ID: "s1", Text: "Do it."}}}},
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func identityContext(t *testing.T) context.Context {
	t.Helper()
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:0", DataDir: t.TempDir(), DevAuthBypass: true}})
	if err != nil {
		t.Fatal(err)
	}
	var got context.Context
	a.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Context() })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if got == nil {
		t.Fatal("auth middleware did not pass the request on")
	}
	return got
}

func (f *fixture) do(method, path string, form url.Values, hdr ...string) *httptest.ResponseRecorder {
	f.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, Pattern+path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

func (f *fixture) newList(name string) int64 {
	f.t.Helper()
	info, err := f.shop.CreateList(context.Background(), name)
	if err != nil {
		f.t.Fatal(err)
	}
	return info.ID
}

func (f *fixture) addRecipe(list, recipeID int64, mult string) {
	f.t.Helper()
	rec := f.do(http.MethodPost, fmt.Sprintf("/%d/recipes", list), url.Values{"recipe": {id(recipeID)}, "multiplier": {mult}})
	if rec.Code != http.StatusSeeOther {
		f.t.Fatalf("add recipe: status %d, body %s", rec.Code, rec.Body)
	}
}

func id(n int64) string { return strconv.FormatInt(n, 10) }

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, want, rec.Body)
	}
}

func wantBody(t *testing.T, rec *httptest.ResponseRecorder, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(rec.Body.String(), s) {
			t.Errorf("body lacks %q:\n%s", s, rec.Body)
		}
	}
}

func TestCreateListRedirectsToItsPage(t *testing.T) {
	f := newFixture(t)
	rec := f.do(http.MethodPost, "/", url.Values{"name": {"Weekend"}})
	wantStatus(t, rec, http.StatusSeeOther)
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, Pattern+"/") {
		t.Fatalf("Location = %q", loc)
	}
	page := f.do(http.MethodGet, strings.TrimPrefix(loc, Pattern), nil)
	wantStatus(t, page, http.StatusOK)
	wantBody(t, page, "Weekend")
}

func TestCreateListWithoutNameIsAnInlineError(t *testing.T) {
	f := newFixture(t)
	rec := f.do(http.MethodPost, "/", url.Values{"name": {"  "}})
	wantStatus(t, rec, http.StatusBadRequest)
	wantBody(t, rec, msgName)
}

func TestBuildListMergesRecipesAndShowsSources(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.addRecipe(list, f.cakes, "1")
	f.addRecipe(list, f.waffl, "1")

	page := f.do(http.MethodGet, "/"+id(list), nil)
	wantStatus(t, page, http.StatusOK)
	// Flour from both recipes is one item with the summed amount, under its aisle,
	// and names both recipes.
	wantBody(t, page, "zz flour", "3 cup", "Zz Pancakes, Zz Waffles", "baking", "dairy")
	if n := strings.Count(page.Body.String(), `data-testid="item-name">zz flour`); n != 1 {
		t.Errorf("flour appears %d times in the list, want once", n)
	}
}

func TestMultiplierScalesAndInvalidOnesAreInline(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.addRecipe(list, f.waffl, "2")
	wantBody(t, f.do(http.MethodGet, "/"+id(list), nil), "4 cup")

	for _, bad := range []string{"0", "-1", "abc"} {
		rec := f.do(http.MethodPost, fmt.Sprintf("/%d/recipes", list), url.Values{"recipe": {id(f.cakes)}, "multiplier": {bad}})
		wantStatus(t, rec, http.StatusBadRequest)
		wantBody(t, rec, msgMultiplier)
	}
	view, err := f.shop.Load(context.Background(), list)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Entries) != 1 {
		t.Fatalf("a rejected multiplier changed the entries: %+v", view.Entries)
	}

	entry := view.Entries[0].ID
	rec := f.do(http.MethodPost, fmt.Sprintf("/%d/recipes/%d", list, entry), url.Values{"multiplier": {"1/2"}})
	wantStatus(t, rec, http.StatusSeeOther)
	wantBody(t, f.do(http.MethodGet, "/"+id(list), nil), "1 cup")
	rec = f.do(http.MethodPost, fmt.Sprintf("/%d/recipes/%d", list, entry), url.Values{"multiplier": {"0"}})
	wantStatus(t, rec, http.StatusBadRequest)
	wantBody(t, rec, msgMultiplier)
}

func TestRecipeAlreadyOnTheListIsAnInlineConflict(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.addRecipe(list, f.cakes, "1")
	rec := f.do(http.MethodPost, fmt.Sprintf("/%d/recipes", list), url.Values{"recipe": {id(f.cakes)}, "multiplier": {"1"}})
	wantStatus(t, rec, http.StatusConflict)
	wantBody(t, rec, msgDuplicate)
}

func TestRemoveRecipe(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.addRecipe(list, f.cakes, "1")
	view, _ := f.shop.Load(context.Background(), list)
	rec := f.do(http.MethodPost, fmt.Sprintf("/%d/recipes/%d/remove", list, view.Entries[0].ID), url.Values{})
	wantStatus(t, rec, http.StatusSeeOther)
	wantBody(t, f.do(http.MethodGet, "/"+id(list), nil), "Nothing on this list yet.")
}

func TestEntryOfAnotherListIsNotFound(t *testing.T) {
	f := newFixture(t)
	a, b := f.newList("A"), f.newList("B")
	f.addRecipe(a, f.cakes, "1")
	view, _ := f.shop.Load(context.Background(), a)
	rec := f.do(http.MethodPost, fmt.Sprintf("/%d/recipes/%d/remove", b, view.Entries[0].ID), url.Values{})
	wantStatus(t, rec, http.StatusNotFound)
	if v, _ := f.shop.Load(context.Background(), a); len(v.Entries) != 1 {
		t.Fatal("a URL naming list B removed list A's entry")
	}
}

func TestManualItems(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")

	rec := f.do(http.MethodPost, fmt.Sprintf("/%d/manual", list), url.Values{"text": {"paper towels"}})
	wantStatus(t, rec, http.StatusSeeOther)
	rec = f.do(http.MethodPost, fmt.Sprintf("/%d/manual", list), url.Values{"ingredient": {id(f.milk)}, "qty": {"2"}, "unit": {"cup"}})
	wantStatus(t, rec, http.StatusSeeOther)
	page := f.do(http.MethodGet, "/"+id(list), nil)
	wantBody(t, page, "paper towels", "zz milk", "2 cup", "added by hand")

	for name, form := range map[string]url.Values{
		"nothing":      {},
		"bad quantity": {"text": {"x"}, "qty": {"lots"}},
		"bad unit":     {"text": {"x"}, "qty": {"1"}, "unit": {"furlong"}},
	} {
		t.Run(name, func(t *testing.T) {
			rec := f.do(http.MethodPost, fmt.Sprintf("/%d/manual", list), form)
			wantStatus(t, rec, http.StatusBadRequest)
			wantBody(t, rec, `data-testid="problem"`)
		})
	}

	view, _ := f.shop.Load(context.Background(), list)
	rec = f.do(http.MethodPost, fmt.Sprintf("/%d/manual/%d/remove", list, view.Manual[0].ID), url.Values{})
	wantStatus(t, rec, http.StatusSeeOther)
	if v, _ := f.shop.Load(context.Background(), list); len(v.Manual) != 1 {
		t.Fatalf("manual items after remove = %d, want 1", len(v.Manual))
	}
}

func TestPantryDropsItemsAndAddBackReturnsThem(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.addRecipe(list, f.cakes, "1")

	// Adding twice is not an error.
	for range 2 {
		wantStatus(t, f.do(http.MethodPost, "/pantry/add", url.Values{"ingredient": {id(f.salt)}}), http.StatusSeeOther)
	}
	page := f.do(http.MethodGet, "/"+id(list), nil)
	dropped := between(page.Body.String(), `data-testid="dropped"`, "</section>")
	if !strings.Contains(dropped, "zz salt") {
		t.Fatalf("salt not listed as dropped:\n%s", page.Body)
	}
	if strings.Contains(between(page.Body.String(), `data-testid="list"`, "Left off"), "zz salt") {
		t.Fatal("pantry salt is still on the list")
	}

	wantStatus(t, f.do(http.MethodPost, fmt.Sprintf("/%d/addback", list), url.Values{"ingredient": {id(f.salt)}}), http.StatusSeeOther)
	page = f.do(http.MethodGet, "/"+id(list), nil)
	if strings.Contains(page.Body.String(), `data-testid="dropped"`) {
		t.Errorf("salt is still listed as dropped after add-back:\n%s", page.Body)
	}
	wantBody(t, page, `data-testid="item-name">zz salt`)
}

func between(s, from, to string) string {
	i := strings.Index(s, from)
	if i < 0 {
		return ""
	}
	s = s[i:]
	if j := strings.Index(s, to); j >= 0 {
		return s[:j]
	}
	return s
}

func TestPantryPageSearchAddAndRemove(t *testing.T) {
	f := newFixture(t)
	page := f.do(http.MethodGet, "/pantry", nil)
	wantStatus(t, page, http.StatusOK)
	wantBody(t, page, "The pantry is empty.")

	page = f.do(http.MethodGet, "/pantry?q=SALT", nil)
	wantBody(t, page, "zz salt")
	if strings.Contains(page.Body.String(), "zz milk") {
		t.Error("search for salt matched milk")
	}

	rec := f.do(http.MethodPost, "/pantry/add", url.Values{"ingredient": {id(f.salt)}, "q": {"salt"}})
	wantStatus(t, rec, http.StatusSeeOther)
	if loc := rec.Header().Get("Location"); loc != Pattern+"/pantry?q=salt" {
		t.Errorf("Location = %q, want the search kept", loc)
	}
	page = f.do(http.MethodGet, "/pantry", nil)
	wantBody(t, page, `data-testid="pantry-item"`, "zz salt")

	wantStatus(t, f.do(http.MethodPost, "/pantry/remove", url.Values{"ingredient": {id(f.salt)}}), http.StatusSeeOther)
	wantBody(t, f.do(http.MethodGet, "/pantry", nil), "The pantry is empty.")

	// An ingredient that is not in the catalog is a client error, not a crash.
	wantStatus(t, f.do(http.MethodPost, "/pantry/add", url.Values{"ingredient": {"999999"}}), http.StatusBadRequest)
	wantStatus(t, f.do(http.MethodPost, "/pantry/add", url.Values{"ingredient": {"abc"}}), http.StatusBadRequest)
}

func TestUnknownListIsNotFoundOnEveryRoute(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{"/999", "/999/live", "/999/events", "/999/export.txt"} {
		wantStatus(t, f.do(http.MethodGet, path, nil), http.StatusNotFound)
	}
	for _, path := range []string{"/999/recipes", "/999/manual", "/999/check", "/999/regenerate", "/999/addback"} {
		rec := f.do(http.MethodPost, path, url.Values{"recipe": {id(f.cakes)}, "text": {"x"}, "key": {"t:x"}, "checked": {"1"}, "ingredient": {id(f.salt)}})
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404; body %s", path, rec.Code, rec.Body)
		}
	}
	wantStatus(t, f.do(http.MethodGet, "/abc", nil), http.StatusBadRequest)
}

func TestEmptyListPageAndExport(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Empty")
	wantBody(t, f.do(http.MethodGet, "/"+id(list), nil), "Nothing on this list yet.")
	wantBody(t, f.do(http.MethodGet, "/"+id(list)+"/live", nil), "Nothing on this list yet.")
	rec := f.do(http.MethodGet, "/"+id(list)+"/export.txt", nil)
	wantStatus(t, rec, http.StatusOK)
	if rec.Body.Len() != 0 {
		t.Errorf("export of an empty list = %q", rec.Body)
	}
}

func TestExportIsPlainTextGroupedByAisle(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.addRecipe(list, f.cakes, "1")
	f.addRecipe(list, f.waffl, "1")
	rec := f.do(http.MethodGet, "/"+id(list)+"/export.txt", nil)
	wantStatus(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q", ct)
	}
	view, _ := f.shop.Load(context.Background(), list)
	want, err := view.Generated().Text()
	if err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != want || !strings.Contains(want, "- zz flour: 3 cup") {
		t.Errorf("export = %q, want the store's text %q", rec.Body, want)
	}
}

func checkForm(key, v string) url.Values { return url.Values{"key": {key}, "checked": {v}} }

func TestCheckTogglesThroughTheStore(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.addRecipe(list, f.waffl, "1")
	key := fmt.Sprintf("i:%d", f.flour)

	rec := f.do(http.MethodPost, "/"+id(list)+"/check", checkForm(key, "1"), "X-Requested-With", "fetch")
	wantStatus(t, rec, http.StatusNoContent)
	view, _ := f.shop.Load(context.Background(), list)
	if !view.Aisles[0].Items[0].Checked {
		t.Fatal("item not checked in the store")
	}
	wantBody(t, f.do(http.MethodGet, "/"+id(list)+"/live", nil), `aria-checked="true"`)

	// Without the script header the answer is a redirect back to the view.
	rec = f.do(http.MethodPost, "/"+id(list)+"/check", checkForm(key, "0"))
	wantStatus(t, rec, http.StatusSeeOther)
	if v, _ := f.shop.Load(context.Background(), list); v.Aisles[0].Items[0].Checked {
		t.Fatal("item still checked after unchecking")
	}

	wantStatus(t, f.do(http.MethodPost, "/"+id(list)+"/check", checkForm("garbage", "1")), http.StatusBadRequest)
	wantStatus(t, f.do(http.MethodPost, "/"+id(list)+"/check", checkForm("", "1")), http.StatusBadRequest)
}

func TestChecksSurviveRegeneration(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.addRecipe(list, f.waffl, "1")
	f.addRecipe(list, f.cakes, "1")
	flour, milk := fmt.Sprintf("i:%d", f.flour), fmt.Sprintf("i:%d", f.milk)
	wantStatus(t, f.do(http.MethodPost, "/"+id(list)+"/check", checkForm(flour, "1")), http.StatusSeeOther)
	wantStatus(t, f.do(http.MethodPost, "/"+id(list)+"/check", checkForm(milk, "1")), http.StatusSeeOther)

	// Changing a multiplier changes flour's amount; removing the pancakes removes
	// milk. Flour stays checked, milk's check goes with it.
	view, _ := f.shop.Load(context.Background(), list)
	for _, e := range view.Entries {
		switch e.RecipeID {
		case f.waffl:
			wantStatus(t, f.do(http.MethodPost, fmt.Sprintf("/%d/recipes/%d", list, e.ID), url.Values{"multiplier": {"3"}}), http.StatusSeeOther)
		case f.cakes:
			wantStatus(t, f.do(http.MethodPost, fmt.Sprintf("/%d/recipes/%d/remove", list, e.ID), url.Values{}), http.StatusSeeOther)
		}
	}
	wantStatus(t, f.do(http.MethodPost, "/"+id(list)+"/regenerate", url.Values{}), http.StatusSeeOther)
	page := f.do(http.MethodGet, "/"+id(list)+"/live", nil)
	wantBody(t, page, `aria-checked="true"`, "6 cup")

	wantStatus(t, f.do(http.MethodPost, "/"+id(list)+"/recipes", url.Values{"recipe": {id(f.cakes)}, "multiplier": {"1"}}), http.StatusSeeOther)
	live := f.do(http.MethodGet, "/"+id(list)+"/live", nil).Body.String()
	if !strings.Contains(between(live, `data-key="i:`+id(f.flour), "</li>"), `data-checked="true"`) {
		t.Error("flour lost its check")
	}
	if strings.Contains(between(live, `data-key="i:`+id(f.milk), "</li>"), `data-checked="true"`) {
		t.Error("milk came back checked after its recipe was removed and re-added")
	}
}

func TestDOMIDsAreUniqueWhereKeysAreNot(t *testing.T) {
	f := newFixture(t)
	// Two identical unresolved lines of one recipe share an ItemKey.
	unresolved := recipe.IngredientLine{Rule: quantity.RuleLinear, Raw: "a pinch of luck"}
	r := f.recipe("Zz Lucky", unresolved, unresolved)
	list := f.newList("Week")
	f.addRecipe(list, r, "1")
	body := f.do(http.MethodGet, "/"+id(list)+"/live", nil).Body.String()
	for _, want := range []string{`id="item-0-0"`, `id="item-0-1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in:\n%s", want, body)
		}
	}
}

func TestAssetsAreServedFromTheBinary(t *testing.T) {
	f := newFixture(t)
	js := f.do(http.MethodGet, "/shopping.js", nil)
	wantStatus(t, js, http.StatusOK)
	if !strings.Contains(js.Header().Get("Content-Type"), "javascript") {
		t.Errorf("Content-Type = %q", js.Header().Get("Content-Type"))
	}
	wantStatus(t, f.do(http.MethodGet, "/shopping.css", nil), http.StatusOK)

	list := f.newList("Week")
	for _, path := range []string{"/", "/pantry", "/" + id(list), "/" + id(list) + "/live"} {
		body := f.do(http.MethodGet, path, nil).Body.String()
		for _, bad := range []string{"http://", "https://", "//cdn"} {
			if strings.Contains(body, bad) {
				t.Errorf("%s references %q", path, bad)
			}
		}
	}
}

// --- event stream ---

// sseClient reads server-sent events from a running test server.
type sseClient struct {
	t      *testing.T
	cancel context.CancelFunc
	events chan string
	done   chan struct{}
}

func openStream(t *testing.T, base string, list int64) *sseClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s%s/%d/events", base, Pattern, list), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		cancel()
		t.Fatalf("stream: status %d, Content-Type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	c := &sseClient{t: t, cancel: cancel, events: make(chan string, 16), done: make(chan struct{})}
	go func() {
		defer close(c.done)
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("close event stream: %v", err)
			}
		}()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		var frame strings.Builder
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				if frame.Len() > 0 {
					c.events <- frame.String()
					frame.Reset()
				}
				continue
			}
			frame.WriteString(line + "\n")
		}
	}()
	t.Cleanup(c.close)
	return c
}

func (c *sseClient) close() {
	c.cancel()
	<-c.done
}

// next returns the next frame, or fails after the wait.
func (c *sseClient) next(wait time.Duration) string {
	c.t.Helper()
	select {
	case e := <-c.events:
		return e
	case <-time.After(wait):
		c.t.Fatal("no event within the wait")
		return ""
	}
}

func (f *fixture) streamServer(writeTimeout time.Duration) *httptest.Server {
	srv := httptest.NewUnstartedServer(f.srv)
	srv.Config.WriteTimeout = writeTimeout
	srv.Start()
	f.t.Cleanup(srv.Close)
	return srv
}

func TestEventsSendFullStateThenEveryCheck(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.addRecipe(list, f.waffl, "1")
	srv := f.streamServer(0)

	c := openStream(t, srv.URL, list)
	first := c.next(5 * time.Second)
	if !strings.HasPrefix(first, "event: list\n") || !strings.Contains(first, "zz flour") || !strings.Contains(first, `data-checked="false"`) {
		t.Fatalf("first event = %q", first)
	}

	if err := f.shop.SetChecked(context.Background(), list, domain.ItemKey("i:"+id(f.flour)), true); err != nil {
		t.Fatal(err)
	}
	if got := c.next(5 * time.Second); !strings.Contains(got, `data-checked="true"`) {
		t.Fatalf("event after a check = %q", got)
	}

	// A view that was away (a dropped connection) starts from the whole current
	// state when it reconnects.
	c.close()
	again := openStream(t, srv.URL, list)
	if got := again.next(5 * time.Second); !strings.Contains(got, `data-checked="true"`) || !strings.Contains(got, "zz flour") {
		t.Fatalf("resync event = %q", got)
	}
}

func TestEventsOnlyForTheWatchedList(t *testing.T) {
	f := newFixture(t)
	a, b := f.newList("A"), f.newList("B")
	f.addRecipe(a, f.waffl, "1")
	f.addRecipe(b, f.waffl, "1")
	srv := f.streamServer(0)
	c := openStream(t, srv.URL, a)
	c.next(5 * time.Second)
	if err := f.shop.SetChecked(context.Background(), b, domain.ItemKey("i:"+id(f.flour)), true); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-c.events:
		t.Fatalf("list A's stream got an event for list B: %q", e)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestPantryChangeReachesEveryOpenList(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.addRecipe(list, f.cakes, "1")
	srv := f.streamServer(0)
	c := openStream(t, srv.URL, list)
	if first := c.next(5 * time.Second); !strings.Contains(first, "zz salt") {
		t.Fatalf("salt missing before the pantry change: %q", first)
	}
	wantStatus(t, f.do(http.MethodPost, "/pantry/add", url.Values{"ingredient": {id(f.salt)}}), http.StatusSeeOther)
	if got := c.next(5 * time.Second); strings.Contains(got, "zz salt") {
		t.Fatalf("salt still listed after it joined the pantry: %q", got)
	}
}

func TestEventStreamEndsAndUnsubscribesWhenTheClientLeaves(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	srv := f.streamServer(0)

	var streams []*sseClient
	for range 3 {
		c := openStream(t, srv.URL, list)
		c.next(5 * time.Second)
		streams = append(streams, c)
	}
	if n := f.h.hub.count(); n != 3 {
		t.Fatalf("open subscriptions = %d, want 3", n)
	}
	for _, c := range streams {
		c.close()
	}
	deadline := time.Now().Add(5 * time.Second)
	for f.h.hub.count() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d subscriptions leaked after every client disconnected", f.h.hub.count())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEventStreamSurvivesTheServerWriteTimeout(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.addRecipe(list, f.waffl, "1")
	srv := f.streamServer(300 * time.Millisecond)

	c := openStream(t, srv.URL, list)
	c.next(5 * time.Second)
	time.Sleep(900 * time.Millisecond) // three times the write timeout
	if err := f.shop.SetChecked(context.Background(), list, domain.ItemKey("i:"+id(f.flour)), true); err != nil {
		t.Fatal(err)
	}
	if got := c.next(5 * time.Second); !strings.Contains(got, `data-checked="true"`) {
		t.Fatalf("event after the write timeout = %q", got)
	}
}

func TestEventStreamSendsHeartbeats(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.h.heartbeat = 50 * time.Millisecond
	srv := f.streamServer(0)
	c := openStream(t, srv.URL, list)
	c.next(5 * time.Second)
	if got := c.next(5 * time.Second); !strings.HasPrefix(got, ": keepalive") {
		t.Fatalf("second frame = %q, want a keepalive comment", got)
	}
}

func TestEventStreamTellsViewersWhenTheListIsDeleted(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	srv := f.streamServer(0)
	c := openStream(t, srv.URL, list)
	c.next(5 * time.Second)
	if err := f.shop.DeleteList(context.Background(), list); err != nil {
		t.Fatal(err)
	}
	if got := c.next(5 * time.Second); !strings.HasPrefix(got, "event: gone") {
		t.Fatalf("event after delete = %q", got)
	}
}

func TestEventFrameSplitsMultilineData(t *testing.T) {
	got := string(eventFrame("list", "a\nb"))
	if want := "event: list\ndata: a\ndata: b\n\n"; got != want {
		t.Errorf("frame = %q, want %q", got, want)
	}
}

func (f *fixture) optionalRecipe() int64 {
	f.t.Helper()
	cup := quantity.MustUnit("cup")
	return f.recipe("Zz Optional", f.line(f.flour, 1, cup, false), f.line(f.milk, 1, cup, true))
}

func TestOptionalLinesToggleOnTheBuildAndLivePages(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.addRecipe(list, f.optionalRecipe(), "1")
	item := `data-testid="item-name">zz milk`

	page := f.do(http.MethodGet, "/"+id(list), nil)
	if strings.Contains(page.Body.String(), item) || strings.Contains(page.Body.String(), "checked") && strings.Contains(between(page.Body.String(), `name="optional"`, ">"), "checked") {
		t.Fatalf("optional milk listed by default, or the box is ticked:\n%s", page.Body)
	}

	wantStatus(t, f.do(http.MethodPost, "/"+id(list)+"/optional", url.Values{"optional": {"1"}}), http.StatusSeeOther)
	page = f.do(http.MethodGet, "/"+id(list), nil)
	wantBody(t, page, item)
	if !strings.Contains(between(page.Body.String(), `name="optional"`, ">"), "checked") {
		t.Error("the box is not ticked after including optional lines")
	}
	wantBody(t, f.do(http.MethodGet, "/"+id(list)+"/live", nil), item)

	// The box left unticked sends no value: that is "exclude".
	wantStatus(t, f.do(http.MethodPost, "/"+id(list)+"/optional", url.Values{}), http.StatusSeeOther)
	if strings.Contains(f.do(http.MethodGet, "/"+id(list)+"/live", nil).Body.String(), item) {
		t.Error("optional milk still on the phone view after excluding")
	}
	wantStatus(t, f.do(http.MethodPost, "/999/optional", url.Values{"optional": {"1"}}), http.StatusNotFound)
}

func TestOptionalToggleReachesAnOpenPhoneView(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.addRecipe(list, f.optionalRecipe(), "1")
	srv := f.streamServer(0)
	c := openStream(t, srv.URL, list)
	if first := c.next(5 * time.Second); strings.Contains(first, "zz milk") {
		t.Fatalf("optional milk in the first event: %q", first)
	}
	wantStatus(t, f.do(http.MethodPost, "/"+id(list)+"/optional", url.Values{"optional": {"1"}}), http.StatusSeeOther)
	if got := c.next(5 * time.Second); !strings.Contains(got, "zz milk") {
		t.Fatalf("event after including optional lines = %q", got)
	}
}

func TestOptionalToggleKeepsChecks(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.addRecipe(list, f.optionalRecipe(), "1")
	wantStatus(t, f.do(http.MethodPost, "/"+id(list)+"/optional", url.Values{"optional": {"1"}}), http.StatusSeeOther)
	wantStatus(t, f.do(http.MethodPost, "/"+id(list)+"/check", checkForm("i:"+id(f.flour), "1")), http.StatusSeeOther)
	wantStatus(t, f.do(http.MethodPost, "/"+id(list)+"/check", checkForm("i:"+id(f.milk), "1")), http.StatusSeeOther)
	wantStatus(t, f.do(http.MethodPost, "/"+id(list)+"/optional", url.Values{}), http.StatusSeeOther)
	wantBody(t, f.do(http.MethodGet, "/"+id(list)+"/live", nil), `aria-checked="true"`)
	wantStatus(t, f.do(http.MethodPost, "/"+id(list)+"/optional", url.Values{"optional": {"1"}}), http.StatusSeeOther)
	live := f.do(http.MethodGet, "/"+id(list)+"/live", nil).Body.String()
	for _, key := range []int64{f.flour, f.milk} {
		if !strings.Contains(between(live, `data-key="i:`+id(key), "</li>"), `data-checked="true"`) {
			t.Errorf("ingredient %d lost its check across the toggle", key)
		}
	}
}

// stuckWriter is a response writer for a client that has stopped reading: after
// okWrites writes, a write blocks until the write deadline passes. With no
// deadline set it blocks for far longer than any test waits.
type stuckWriter struct {
	mu       sync.Mutex
	header   http.Header
	deadline time.Time
	writes   int
	okWrites int
}

func (w *stuckWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}
func (w *stuckWriter) WriteHeader(int) {}
func (w *stuckWriter) Flush()          {}
func (w *stuckWriter) SetWriteDeadline(t time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deadline = t
	return nil
}

func (w *stuckWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.writes++
	n := w.writes
	w.mu.Unlock()
	if n <= w.okWrites {
		return len(p), nil
	}
	// Blocked, like a write into a full socket buffer: it ends when the deadline
	// passes, and the deadline can be moved while it waits, as on a real
	// connection. Without one it gives up after a minute, long after any test.
	giveUp := time.Now().Add(time.Minute)
	for time.Now().Before(giveUp) {
		w.mu.Lock()
		d := w.deadline
		w.mu.Unlock()
		if !d.IsZero() && !time.Now().Before(d) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	return 0, os.ErrDeadlineExceeded
}

func (w *stuckWriter) writeCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writes
}

func TestEventStreamDropsAClientThatStopsReading(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.h.heartbeat = 40 * time.Millisecond
	w := &stuckWriter{okWrites: 1} // the list goes out; the first keepalive blocks
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s/%d/events", Pattern, list), nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		f.srv.ServeHTTP(w, req)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a stuck write held the stream open: no rolling write deadline")
	}
	if n := f.h.hub.count(); n != 0 {
		t.Fatalf("%d subscriptions left after the client was dropped", n)
	}
}

// Shutdown cancels the request context. A write already blocked on a client
// that stopped reading must end then, not when its rolling deadline, which is
// longer than the shutdown grace period, finally passes.
func TestCancelEndsAStreamBlockedMidWrite(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	f.h.heartbeat = time.Hour // the rolling deadline is two hours away
	w := &stuckWriter{okWrites: 1}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s/%d/events", Pattern, list), nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		f.srv.ServeHTTP(w, req)
	}()
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(3 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	waitFor("the first list", func() bool { return w.writeCount() >= 1 })
	// A change makes the handler write again, and that write blocks.
	if err := f.shop.SetChecked(context.Background(), list, domain.ItemKey("t:x"), true); err != nil {
		t.Fatal(err)
	}
	waitFor("a blocked write", func() bool { return w.writeCount() >= 2 })

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelling the request did not end a stream blocked in a write")
	}
	if n := f.h.hub.count(); n != 0 {
		t.Fatalf("%d subscriptions left", n)
	}
}

func TestEventFrameNeverLeavesALineBreakInsideAField(t *testing.T) {
	got := string(eventFrame("list", "a\rb\r\nc\nd"))
	if want := "event: list\ndata: a\ndata: b\ndata: c\ndata: d\n\n"; got != want {
		t.Errorf("frame = %q, want %q", got, want)
	}
}

func TestCarriageReturnInAnItemDoesNotBreakTheStream(t *testing.T) {
	f := newFixture(t)
	list := f.newList("Week")
	rec := f.do(http.MethodPost, fmt.Sprintf("/%d/manual", list), url.Values{"text": {"line one\rline two"}})
	wantStatus(t, rec, http.StatusSeeOther)
	srv := f.streamServer(0)
	c := openStream(t, srv.URL, list)
	first := c.next(5 * time.Second)
	if strings.Contains(first, "\r") {
		t.Fatalf("a bare carriage return reached the event stream, where it ends a line: %q", first)
	}
	if !strings.Contains(first, "line one") || !strings.Contains(first, "line two") {
		t.Fatalf("item text lost: %q", first)
	}
}
