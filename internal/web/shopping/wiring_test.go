package shopping_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/web"
)

// A merge started from /catalog must re-point the shopping rows that reference
// the merged ingredient. The real router wires the shopping store's merge hooks
// onto the catalog store that merges; without them the merge is refused by the
// foreign keys of the pantry and the manual items.
func TestCatalogMergeRepointsShoppingRows(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{Server: config.Server{ListenAddr: "127.0.0.1:0", DataDir: t.TempDir(), DevAuthBypass: true}}
	d, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(cfg, auth.WithLogger(slog.New(slog.NewJSONHandler(io.Discard, nil))))
	if err != nil {
		t.Fatal(err)
	}
	router, err := web.New(web.Deps{Config: cfg, DB: d, Auth: a})
	if err != nil {
		t.Fatal(err)
	}

	// The ingredients are the dev household's own, as the router resolves it
	// for the dev identity: a household merges only its own entries.
	if err := household.New(d).Seed(ctx, "Dev household", auth.DevEmail); err != nil {
		t.Fatal(err)
	}
	dev, err := household.New(d).ForEmail(ctx, auth.DevEmail)
	if err != nil {
		t.Fatal(err)
	}
	ctx = household.WithContext(ctx, dev)
	cat := catalog.New(d)
	loser, err := cat.CreateIngredient(ctx, catalog.Ingredient{Name: "zz scallion", Aisle: "produce"})
	if err != nil {
		t.Fatal(err)
	}
	winner, err := cat.CreateIngredient(ctx, catalog.Ingredient{Name: "zz green onion", Aisle: "produce"})
	if err != nil {
		t.Fatal(err)
	}
	do := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	loserID, winnerID := strconv.FormatInt(loser.ID, 10), strconv.FormatInt(winner.ID, 10)

	// The loser is in the pantry and on a list as a manual item.
	if rec := do(http.MethodPost, "/shopping/pantry/add", url.Values{"ingredient": {loserID}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("add to pantry: %d %s", rec.Code, rec.Body)
	}
	rec := do(http.MethodPost, "/shopping", url.Values{"name": {"Week"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create list: %d %s", rec.Code, rec.Body)
	}
	list := rec.Header().Get("Location")
	if rec := do(http.MethodPost, list+"/manual", url.Values{"ingredient": {loserID}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("add manual item: %d %s", rec.Code, rec.Body)
	}

	// The confirmation counts the pantry row and the manual item.
	confirm := httptest.NewRecorder()
	router.ServeHTTP(confirm, httptest.NewRequest(http.MethodGet, "/catalog/merge?source="+loserID+"&target="+winnerID, nil))
	if body := confirm.Body.String(); !strings.Contains(body, `data-testid="merge-uses">2<`) {
		t.Errorf("merge confirmation does not count the shopping rows:\n%s", body)
	}

	if rec := do(http.MethodPost, "/catalog/merge", url.Values{"source": {loserID}, "target": {winnerID}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("merge: status %d, body %s", rec.Code, rec.Body)
	}

	pantry := httptest.NewRecorder()
	router.ServeHTTP(pantry, httptest.NewRequest(http.MethodGet, "/shopping/pantry", nil))
	if body := pantry.Body.String(); !strings.Contains(body, "zz green onion") || strings.Contains(body, "zz scallion") {
		t.Errorf("pantry after the merge should hold the winner only:\n%s", body)
	}
	page := httptest.NewRecorder()
	router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, list, nil))
	if body := page.Body.String(); !strings.Contains(body, "zz green onion") || strings.Contains(body, "zz scallion") {
		t.Errorf("list after the merge should hold the winner only:\n%s", body)
	}
}
