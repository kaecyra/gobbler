package recipeview

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/store"
)

// cloned is a recipe store whose recipe 7 was cloned from recipe 3 at version 2.
type cloned struct{ fakeRecipes }

func (c cloned) Info(ctx context.Context, id int64) (store.Info, error) {
	info, err := c.fakeRecipes.Info(ctx, id)
	info.ClonedFrom = &store.Origin{RecipeID: 3, Version: 2}
	return info, err
}

// brokenInfo loads the document but fails on its bookkeeping.
type brokenInfo struct{ fakeRecipes }

func (brokenInfo) Info(context.Context, int64) (store.Info, error) {
	return store.Info{}, errors.New("disk on fire")
}

func TestViewIncludesTheJournalFragmentWithAPlainLinkFallback(t *testing.T) {
	_, body := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	mustContain(t, body,
		`data-testid="journal-slot"`,
		`hx-get="/recipes/7/journal"`,
		`hx-trigger="load"`,
		`<a href="/recipes/7/journal"`)
}

func TestJournalSlotIsOutsideTheSwappedRecipe(t *testing.T) {
	// Scaling swaps #recipe; a journal inside it would reload and lose a
	// half-typed note on every tap.
	_, body := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	end := indexOf(t, body, "</article>")
	if slot := indexOf(t, body, `data-testid="journal-slot"`); slot < end {
		t.Errorf("journal slot is inside the recipe article")
	}
}

func indexOf(t *testing.T, s, sub string) int {
	t.Helper()
	i := strings.Index(s, sub)
	if i < 0 {
		t.Fatalf("body missing %q", sub)
	}
	return i
}

func TestViewLinksToHistoryAndOffersClone(t *testing.T) {
	_, body := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	mustContain(t, body,
		`data-testid="history-link"`, `href="/recipes/7/history"`,
		`<form method="post" action="/recipes/7/clone"`, `data-testid="clone-button"`)
	// The history and clone pages take no display state.
	_, body = serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7?scale=2&units=metric")
	mustContain(t, body, `href="/recipes/7/history"`, `action="/recipes/7/clone"`)
}

func TestViewShowsCloneOrigin(t *testing.T) {
	rec := serveRequest(t, cloned{fakeRecipes{7: testRecipe(t)}}, testCatalog(), httptest.NewRequest(http.MethodGet, "/recipes/7", nil))
	mustContain(t, rec.Body.String(),
		`data-testid="origin"`, `href="/recipes/3"`, `href="/recipes/3/history/2"`)
}

func TestViewWithoutAnOriginShowsNone(t *testing.T) {
	_, body := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	mustNotContain(t, body, `data-testid="origin"`)
}

func TestInfoFailureIsAServerError(t *testing.T) {
	rec := serveRequest(t, brokenInfo{fakeRecipes{7: testRecipe(t)}}, testCatalog(), httptest.NewRequest(http.MethodGet, "/recipes/7", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestRecipeToolsAreAnAsideOutsideTheRecipe(t *testing.T) {
	_, body := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	mustContain(t, body, `<aside aria-label="Recipe tools">`)
	if indexOf(t, body, "<aside") < indexOf(t, body, "</article>") {
		t.Error("aside is inside the recipe article")
	}
}
