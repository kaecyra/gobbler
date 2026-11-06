package recipeview

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/store"
)

// fakeReaper records the recipes whose picture objects were collected and
// removed, standing in for *picture.Reaper (ADR-00039).
type fakeReaper struct{ collected, removed []int64 }

func (f *fakeReaper) CollectRecipe(_ context.Context, _ *sql.Tx, _, id int64) ([]string, error) {
	f.collected = append(f.collected, id)
	return []string{fmt.Sprintf("recipe-%d", id)}, nil
}

func (f *fakeReaper) Remove(_ context.Context, keys []string) {
	for _, k := range keys {
		var id int64
		if _, err := fmt.Sscanf(k, "recipe-%d", &id); err == nil {
			f.removed = append(f.removed, id)
		}
	}
}

// devMembers admits the one household the test context names.
type devMembers struct{ h household.Household }

func (m devMembers) ForEmail(context.Context, string) (household.Household, error) { return m.h, nil }

// serviceFor is the recipe service over rs for the household ctx names, with
// reaper standing in for the media store.
func serviceFor(ctx context.Context, t *testing.T, rs *store.Store, reaper apirecipes.Reaper) *apirecipes.Service {
	t.Helper()
	h, ok := household.FromContext(ctx)
	if !ok {
		t.Fatal("no household on the test context")
	}
	svc, err := apirecipes.New(apirecipes.Deps{Recipes: rs, Catalog: catalog.New(nil), Members: devMembers{h}, Reaper: reaper})
	if err != nil {
		t.Fatalf("recipes service: %v", err)
	}
	return svc
}

func deleteRequest(ctx context.Context, t *testing.T, rs *store.Store, reaper apirecipes.Reaper, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	r := chi.NewRouter()
	r.Mount(DeletePattern, DeleteRoutes(DeleteDeps{Recipes: serviceFor(ctx, t, rs, reaper)}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func TestDeletePageNamesTheRecipeAndWhatGoesWithIt(t *testing.T) {
	ctx, rs, _ := storeEnv(t)
	id, err := rs.Create(ctx, testRecipe(t))
	if err != nil {
		t.Fatal(err)
	}
	rec := deleteRequest(ctx, t, rs, nil, http.MethodGet, fmt.Sprintf("/recipes/%d/delete", id), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	mustContain(t, body, html.EscapeString(testRecipe(t).Name), "history", "notes", "made-it", "picture", `name="confirm"`, `data-testid="delete-confirm"`)
}

func TestDeleteRefusedWithoutTheTypedWord(t *testing.T) {
	ctx, rs, _ := storeEnv(t)
	id, err := rs.Create(ctx, testRecipe(t))
	if err != nil {
		t.Fatal(err)
	}
	reaper := &fakeReaper{}
	for _, typed := range []string{"", "delete", "DELET", "yes"} {
		rec := deleteRequest(ctx, t, rs, reaper, http.MethodPost, fmt.Sprintf("/recipes/%d/delete", id), url.Values{"confirm": {typed}})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("confirm %q: status = %d, want 400", typed, rec.Code)
		}
	}
	if _, err := rs.Get(ctx, id); err != nil {
		t.Fatalf("recipe was deleted without the confirmation: %v", err)
	}
	if len(reaper.removed) != 0 {
		t.Errorf("picture objects removed without the confirmation: %v", reaper.removed)
	}
}

func TestDeleteRemovesTheRecipeAndItsPictureAndKeepsAClone(t *testing.T) {
	ctx, rs, _ := storeEnv(t)
	id, err := rs.Create(ctx, testRecipe(t))
	if err != nil {
		t.Fatal(err)
	}
	cloneID, err := rs.Clone(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	reaper := &fakeReaper{}
	rec := deleteRequest(ctx, t, rs, reaper, http.MethodPost, fmt.Sprintf("/recipes/%d/delete", id), url.Values{"confirm": {"DELETE"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/recipes" {
		t.Fatalf("status = %d, Location = %q, want 303 to /recipes", rec.Code, rec.Header().Get("Location"))
	}
	if _, err := rs.Get(ctx, id); err == nil {
		t.Error("recipe still exists after the delete")
	}
	if len(reaper.removed) != 1 || reaper.removed[0] != id {
		t.Errorf("picture objects removed for %v, want [%d]", reaper.removed, id)
	}
	if _, err := rs.Get(ctx, cloneID); err != nil {
		t.Errorf("clone did not survive: %v", err)
	}
}

func TestDeleteOfAMissingRecipeIsNotFound(t *testing.T) {
	ctx, rs, _ := storeEnv(t)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := deleteRequest(ctx, t, rs, nil, method, "/recipes/999/delete", url.Values{"confirm": {"DELETE"}})
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", method, rec.Code)
		}
	}
}

func TestRecipePageOffersDelete(t *testing.T) {
	_, body := serve(t, fakeRecipes{1: testRecipe(t)}, "/recipes/1")
	mustContain(t, body, `data-testid="delete-link"`, `href="/recipes/1/delete"`, "Delete recipe")
}

// failingDeleter finds every recipe and fails every delete with err.
type failingDeleter struct {
	*apirecipes.Service
	err error
}

func (f failingDeleter) Delete(context.Context, apirecipes.Caller, int64) error {
	return f.err
}

func TestDeleteOfARecipeOnAShoppingListSaysSoWithA409(t *testing.T) {
	ctx, rs, _ := storeEnv(t)
	id, err := rs.Create(ctx, testRecipe(t))
	if err != nil {
		t.Fatal(err)
	}
	d := failingDeleter{Service: serviceFor(ctx, t, rs, nil), err: fmt.Errorf("delete recipe %d: %w", id, store.ErrOnShoppingList)}
	r := chi.NewRouter()
	r.Mount(DeletePattern, DeleteRoutes(DeleteDeps{Recipes: d}))
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/recipes/%d/delete", id), strings.NewReader(url.Values{"confirm": {"DELETE"}}.Encode())).WithContext(ctx)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	mustContain(t, rec.Body.String(), "on a shopping list", "must be removed from it before it can be deleted")
}
