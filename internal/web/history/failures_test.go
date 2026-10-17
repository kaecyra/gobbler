package history

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

var errDisk = errors.New("database is locked")

// flaky is the real store with chosen reads and writes failing, the one
// boundary these handlers talk to. A failure must be a server error page, never
// an empty history or a half-built version.
type flaky struct {
	*store.Store
	revisions, version, clone, restore bool
}

func (f flaky) ListRevisions(ctx context.Context, id int64) ([]store.Revision, error) {
	if f.revisions {
		return nil, errDisk
	}
	return f.Store.ListRevisions(ctx, id)
}

func (f flaky) GetVersion(ctx context.Context, id, version int64) (recipe.Recipe, error) {
	if f.version {
		return recipe.Recipe{}, errDisk
	}
	return f.Store.GetVersion(ctx, id, version)
}

func (f flaky) Clone(ctx context.Context, id int64) (int64, error) {
	if f.clone {
		return 0, errDisk
	}
	return f.Store.Clone(ctx, id)
}

func (f flaky) Restore(ctx context.Context, id, version int64) error {
	if f.restore {
		return errDisk
	}
	return f.Store.Restore(ctx, id, version)
}

// brokenCatalog fails every lookup, as a catalog database that went away would.
type brokenCatalog struct{}

func (brokenCatalog) GetIngredient(context.Context, int64) (catalog.Ingredient, error) {
	return catalog.Ingredient{}, errDisk
}

func (brokenCatalog) GetEquipment(context.Context, int64) (catalog.Equipment, error) {
	return catalog.Equipment{}, errDisk
}

func TestStoreFailuresAreServerErrorsNotEmptyPages(t *testing.T) {
	e := newEnv(t)
	id := e.create(e.doc("Loaf v1", "3"))
	e.update(id, e.doc("Loaf v2", "4"))
	confirm := url.Values{confirmField: {confirmValue}}
	base := "/recipes/" + itoa(id)

	for _, tc := range []struct {
		name   string
		store  flaky
		method string
		path   string
	}{
		{"list revisions", flaky{Store: e.store, revisions: true}, "GET", base + "/history"},
		{"list current version", flaky{Store: e.store, version: true}, "GET", base + "/history"},
		{"show version", flaky{Store: e.store, version: true}, "GET", base + "/history/1"},
		{"restore", flaky{Store: e.store, restore: true}, "POST", base + "/history/1/restore"},
		{"clone", flaky{Store: e.store, clone: true}, "POST", base + "/clone"},
	} {
		router := e.routerFor(Deps{Recipes: tc.store, Catalog: catalog.New(nil), Thumbnails: e.thumbs})
		var rec = e.post(router, tc.path, confirm)
		if tc.method == "GET" {
			rec = e.doOn(router, tc.path)
		}
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status = %d, want 500", tc.name, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "database is locked") {
			t.Errorf("%s: the page leaks the cause:\n%s", tc.name, rec.Body.String())
		}
	}
	// The failed restore changed nothing.
	if e.current(id).Name != "Loaf v2" {
		t.Error("a failed restore altered the recipe")
	}
}

func TestVersionPageNeedsTheCatalogItReads(t *testing.T) {
	e := newEnv(t)
	id := e.create(e.doc("Loaf v1", "3"))
	e.update(id, e.doc("Loaf v2", "4"))
	router := e.routerFor(Deps{Recipes: e.store, Catalog: brokenCatalog{}, Thumbnails: e.thumbs})
	if rec := e.doOn(router, "/recipes/"+itoa(id)+"/history/1"); rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for a version whose ingredients cannot be resolved", rec.Code)
	}

	// With no lines and no equipment there is nothing to look up, so the same
	// broken catalog still serves the page.
	bare := recipe.Recipe{Name: "Toast", Components: []recipe.Component{{Steps: []recipe.Step{{ID: "a", Text: "Toast it."}}}}}
	bid := e.create(bare)
	e.update(bid, bare)
	if rec := e.doOn(router, "/recipes/"+itoa(bid)+"/history/1"); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for a version with no ingredients", rec.Code)
	}
}

func TestVersionOfStepsAloneHasNoIngredientsOrEquipmentHeading(t *testing.T) {
	e := newEnv(t)
	bare := recipe.Recipe{Name: "Toast", Components: []recipe.Component{{Steps: []recipe.Step{{ID: "a", Text: "Toast it."}}}}}
	id := e.create(bare)
	e.update(id, bare)
	_, body := e.get("/recipes/" + itoa(id) + "/history/1")
	for _, bad := range []string{"Ingredients", "Equipment"} {
		if strings.Contains(body, bad) {
			t.Errorf("version of steps alone shows a %q heading:\n%s", bad, body)
		}
	}
	if !strings.Contains(body, "Toast it.") {
		t.Errorf("version page lacks its step:\n%s", body)
	}
}

func TestMalformedIdsAndFormsAreRefusedBeforeAnyStoreCall(t *testing.T) {
	e := newEnv(t)
	id := e.create(e.doc("Loaf v1", "3"))
	e.update(id, e.doc("Loaf v2", "4"))
	confirm := url.Values{confirmField: {confirmValue}}
	for _, path := range []string{
		"/recipes/abc/history/1/restore",
		"/recipes/" + itoa(id) + "/history/x/restore",
		"/recipes/-1/clone",
	} {
		if rec := e.do(http.MethodPost, path, confirm); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, rec.Code)
		}
	}
	if rec := e.do(http.MethodGet, "/recipes/abc/history/1", nil); rec.Code != http.StatusNotFound {
		t.Errorf("GET version of recipe abc = %d, want 404", rec.Code)
	}
	if e.current(id).Name != "Loaf v2" {
		t.Error("a refused request altered the recipe")
	}
}

// doOn is a GET through router, with logging kept quiet.
func (e *env) doOn(router chi.Router, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(prev)
	router.ServeHTTP(rec, req)
	return rec
}
