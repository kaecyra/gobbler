package recipeview

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The view page answers at /recipes/{id}-{slug} and nowhere else (ADR-00047).
func TestViewURLCanonicalisation(t *testing.T) {
	recipes := fakeRecipes{7: testRecipe(t)} // "Sponge <cake>"
	const canon = "/recipes/7-sponge-cake"
	for _, tc := range []struct {
		name, path string
		code       int
		location   string
	}{
		{"canonical", canon, http.StatusOK, ""},
		{"canonical keeps its query", canon + "?scale=2", http.StatusOK, ""},
		{"plain id", "/recipes/7", http.StatusMovedPermanently, canon},
		{"plain id keeps the query", "/recipes/7?scale=2&units=metric", http.StatusMovedPermanently, canon + "?scale=2&units=metric"},
		{"stale slug", "/recipes/7-old-name", http.StatusMovedPermanently, canon},
		{"stale slug keeps the query", "/recipes/7-old-name?scale=1/2", http.StatusMovedPermanently, canon + "?scale=1/2"},
		{"trailing dash", "/recipes/7-", http.StatusMovedPermanently, canon},
		{"leading zeros", "/recipes/007-sponge-cake", http.StatusMovedPermanently, canon},
		{"no digits", "/recipes/sponge-cake", http.StatusNotFound, ""},
		{"digits then a letter", "/recipes/7sponge", http.StatusNotFound, ""},
		{"zero id", "/recipes/0-x", http.StatusNotFound, ""},
		{"id overflow", "/recipes/99999999999999999999-x", http.StatusNotFound, ""},
		{"unknown id", "/recipes/9-sponge-cake", http.StatusNotFound, ""},
		{"unknown plain id", "/recipes/9", http.StatusNotFound, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveOnce(t, recipes, testCatalog(), httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != tc.code {
				t.Fatalf("status = %d, want %d\n%s", rec.Code, tc.code, rec.Body.String())
			}
			if got := rec.Header().Get("Location"); got != tc.location {
				t.Errorf("Location = %q, want %q", got, tc.location)
			}
		})
	}
}

// A redirect names the title, so it is only given to a caller who can read the
// recipe: an id the caller cannot see answers 404 with no Location.
func TestViewRedirectNeverRevealsATitle(t *testing.T) {
	for _, path := range []string{"/recipes/9", "/recipes/9-guess", "/recipes/9-sponge-cake"} {
		rec := serveOnce(t, fakeRecipes{7: testRecipe(t)}, testCatalog(), httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound || rec.Header().Get("Location") != "" {
			t.Errorf("%s: status = %d, Location = %q; want 404 and none", path, rec.Code, rec.Header().Get("Location"))
		}
		if strings.Contains(rec.Body.String(), "Sponge") {
			t.Errorf("%s: body names another recipe", path)
		}
	}
}

// An HTMX request is redirected like any other: the browser follows a 301
// inside the request, so htmx swaps in the canonical page.
func TestViewRedirectAlsoAnswersHTMX(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/recipes/7?scale=2", nil)
	req.Header.Set("HX-Request", "true")
	rec := serveOnce(t, fakeRecipes{7: testRecipe(t)}, testCatalog(), req)
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/recipes/7-sponge-cake?scale=2" {
		t.Errorf("status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
}

// Sub-pages take a plain numeric id: a slug in front of them is not a recipe.
func TestSubRoutesKeepAPlainID(t *testing.T) {
	recipes := fakeRecipes{7: testRecipe(t)}
	for _, path := range []string{"/recipes/7-sponge-cake/video/thumbnail", "/recipes/7sponge/video/thumbnail"} {
		rec := serveOnce(t, recipes, testCatalog(), httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
		}
	}
}
