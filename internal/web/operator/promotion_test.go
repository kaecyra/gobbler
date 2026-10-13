package operator

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

type promotionFixture struct {
	t     *testing.T
	d     *sql.DB
	cat   *catalog.Store
	alpha context.Context
	beta  context.Context
}

func newPromotionFixture(t *testing.T) *promotionFixture {
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
	f := &promotionFixture{t: t, d: d, cat: catalog.New(d)}
	f.alpha = f.household("Alpha Secret Household")
	f.beta = f.household("Beta Secret Household")
	return f
}

func (f *promotionFixture) household(name string) context.Context {
	f.t.Helper()
	h, err := household.New(f.d).Create(context.Background(), name, strings.ReplaceAll(strings.ToLower(name), " ", "")+"@example.com")
	if err != nil {
		f.t.Fatal(err)
	}
	return household.WithContext(context.Background(), h)
}

func (f *promotionFixture) suggested(ctx context.Context, name string, aliases ...string) catalog.Ingredient {
	f.t.Helper()
	in, err := f.cat.CreateIngredient(ctx, catalog.Ingredient{Name: name, Aisle: "dairy", Aliases: aliases, Reviewed: true})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.cat.SuggestIngredient(ctx, in.ID); err != nil {
		f.t.Fatal(err)
	}
	return in
}

func (f *promotionFixture) do(operator bool, method, target string) *httptest.ResponseRecorder {
	f.t.Helper()
	root := chi.NewRouter()
	root.Mount(PromotionPattern, PromotionRoutes(PromotionDeps{Catalog: f.cat}))
	req := httptest.NewRequest(method, PromotionPattern+target, strings.NewReader(url.Values{}.Encode()))
	ctx := verified(f.t, req)
	if operator {
		ctx = layout.WithOperator(ctx)
	}
	w := httptest.NewRecorder()
	root.ServeHTTP(w, req.WithContext(ctx))
	return w
}

// verified returns req's context after the auth middleware, in dev-bypass
// mode, has put a verified identity on it.
func verified(t *testing.T, req *http.Request) context.Context {
	t.Helper()
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:0", DataDir: t.TempDir(), DevAuthBypass: true}})
	if err != nil {
		t.Fatal(err)
	}
	var got context.Context
	a.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Context() })).
		ServeHTTP(httptest.NewRecorder(), req)
	if got == nil {
		t.Fatal("auth middleware did not pass the request on")
	}
	return got
}

func TestOperatorReviewsSuggestionsWithoutSeeingHouseholds(t *testing.T) {
	f := newPromotionFixture(t)
	ghee := f.suggested(f.alpha, "zz ghee", "zz clarified butter")
	f.suggested(f.beta, "zz clarified butter")
	if _, err := f.cat.CreateIngredient(f.alpha, catalog.Ingredient{Name: "zz private", Aisle: "x"}); err != nil {
		t.Fatal(err)
	}

	list := f.do(true, http.MethodGet, "/")
	body := list.Body.String()
	if list.Code != http.StatusOK || !strings.Contains(body, "zz ghee") || !strings.Contains(body, "zz clarified butter") {
		t.Fatalf("list = %d\n%s", list.Code, body)
	}
	for _, leak := range []string{"zz private", "Secret", "secret"} {
		if strings.Contains(body, leak) {
			t.Errorf("the list shows %q", leak)
		}
	}

	review := f.do(true, http.MethodGet, "/"+itoa(ghee.ID))
	body = review.Body.String()
	if review.Code != http.StatusOK || !strings.Contains(body, `data-testid="duplicate-count">1<`) {
		t.Fatalf("review = %d, want one duplicate\n%s", review.Code, body)
	}
	if strings.Contains(body, "Secret") {
		t.Error("the review names a household")
	}
}

func TestPromoteMakesTheEntryCentralForEveryone(t *testing.T) {
	f := newPromotionFixture(t)
	ghee := f.suggested(f.alpha, "zz ghee")

	w := f.do(true, http.MethodPost, "/"+itoa(ghee.ID))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("promote = %d\n%s", w.Code, w.Body)
	}
	if m, ok, err := f.cat.MatchIngredient(f.beta, "zz ghee"); err != nil || !ok || !m.Central || m.ID != ghee.ID {
		t.Errorf("another household matches %+v ok=%v err=%v, want the promoted entry", m, ok, err)
	}
	if body := f.do(true, http.MethodGet, "/").Body.String(); strings.Contains(body, "zz ghee") {
		t.Error("a promoted entry is still listed as waiting")
	}
}

func TestOnlyTheOperatorReachesThePromotionPages(t *testing.T) {
	f := newPromotionFixture(t)
	ghee := f.suggested(f.alpha, "zz ghee")
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/"}, {http.MethodGet, "/" + itoa(ghee.ID)}, {http.MethodPost, "/" + itoa(ghee.ID)},
	} {
		if w := f.do(false, c.method, c.path); w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "zz ghee") {
			t.Errorf("%s %s as a non-operator = %d", c.method, c.path, w.Code)
		}
	}
	if g, err := f.cat.GetIngredient(f.alpha, ghee.ID); err != nil || g.Central {
		t.Errorf("a refused request promoted the entry: %+v, %v", g, err)
	}
}

func TestPromotionPagesRefuseWhatNobodySuggested(t *testing.T) {
	f := newPromotionFixture(t)
	private, err := f.cat.CreateIngredient(f.alpha, catalog.Ingredient{Name: "zz private", Aisle: "x"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/" + itoa(private.ID), http.StatusNotFound},
		{http.MethodPost, "/" + itoa(private.ID), http.StatusNotFound},
		{http.MethodGet, "/abc", http.StatusBadRequest},
		{http.MethodGet, "/987654", http.StatusNotFound},
	} {
		if w := f.do(true, c.method, c.path); w.Code != c.want || strings.Contains(w.Body.String(), "zz private") {
			t.Errorf("%s %s = %d, want %d", c.method, c.path, w.Code, c.want)
		}
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
