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

	"github.com/kaecyra/gobbler/internal/store"
)

type fakeLibrary struct {
	got     store.SearchQuery
	results []store.Summary
	tags    []store.TagCount
	err     error
}

func (f *fakeLibrary) Search(_ context.Context, q store.SearchQuery) ([]store.Summary, error) {
	f.got = q
	return f.results, f.err
}

func (f *fakeLibrary) Tags(context.Context) ([]store.TagCount, error) { return f.tags, nil }

func get(t *testing.T, lib *fakeLibrary, target string, header map[string]string) (int, string, http.Header) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	Routes(Deps{Library: lib}).ServeHTTP(rec, req)
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	return rec.Code, string(body), rec.Header()
}

func five() *int { n := 4; return &n }

func TestListShowsRecipeFields(t *testing.T) {
	lib := &fakeLibrary{
		results: []store.Summary{{ID: 7, Name: "Pancakes", Description: "Fluffy", Rating: five(), Tags: []string{"breakfast"},
			Prep: 10 * time.Minute, Cook: 20 * time.Minute, Total: 30 * time.Minute}},
		tags: []store.TagCount{{Tag: "breakfast", Recipes: 1}},
	}
	code, body, _ := get(t, lib, "/", nil)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	for _, want := range []string{`href="/recipes/7"`, "Pancakes", "Fluffy", "4 of 5", "Prep 10 min", "Cook 20 min", "Total 30 min", "breakfast", "<form"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestQueryAndTagsReachTheStore(t *testing.T) {
	lib := &fakeLibrary{tags: []store.TagCount{{Tag: "a b", Recipes: 1}, {Tag: "quick", Recipes: 2}}}
	_, body, _ := get(t, lib, "/?q=soup&tag=quick&tag=a+b", nil)
	if lib.got.Text != "soup" || len(lib.got.Tags) != 2 || lib.got.Tags[0] != "quick" || lib.got.Tags[1] != "a b" {
		t.Fatalf("store query = %+v", lib.got)
	}
	// The selected tags survive a new search as hidden fields, and each chip's
	// link toggles its tag while keeping the text.
	if !strings.Contains(body, `type="hidden" name="tag" value="quick"`) {
		t.Error("selected tag is not carried as a hidden field")
	}
	if !strings.Contains(body, `href="/recipes?q=soup&amp;tag=quick"`) {
		t.Errorf("chip for the selected tag \"a b\" should link to the page without it:\n%s", body)
	}
	if !strings.Contains(body, `aria-pressed="true"`) {
		t.Error("selected chip is not marked pressed")
	}
}

func TestEmptyStates(t *testing.T) {
	_, body, _ := get(t, &fakeLibrary{}, "/", nil)
	if !strings.Contains(body, "No recipes yet") {
		t.Error("empty library shows no empty state")
	}
	_, body, _ = get(t, &fakeLibrary{}, "/?q=zebra", nil)
	if !strings.Contains(body, "No recipes match") || strings.Contains(body, "No recipes yet") {
		t.Error("empty search should say nothing matched")
	}
}

func TestHTMXRequestGetsOnlyTheResults(t *testing.T) {
	lib := &fakeLibrary{results: []store.Summary{{ID: 1, Name: "Soup"}}}
	_, full, _ := get(t, lib, "/?q=soup", nil)
	_, frag, h := get(t, lib, "/?q=soup", map[string]string{"HX-Request": "true"})
	if !strings.Contains(full, "<html") || strings.Contains(frag, "<html") {
		t.Errorf("full page has <html>: %v, fragment has <html>: %v", strings.Contains(full, "<html"), strings.Contains(frag, "<html"))
	}
	if !strings.Contains(frag, "Soup") {
		t.Error("fragment lacks the results")
	}
	if !strings.Contains(h.Get("Vary"), "HX-Request") {
		t.Errorf("Vary = %q, want HX-Request", h.Get("Vary"))
	}
	_, boosted, _ := get(t, lib, "/", map[string]string{"HX-Request": "true", "HX-Boosted": "true"})
	if !strings.Contains(boosted, "<html") {
		t.Error("a boosted request must get the whole page")
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
	code, body, _ := get(t, &fakeLibrary{err: errors.New("disk on fire")}, "/", nil)
	if code != http.StatusInternalServerError || strings.Contains(body, "disk on fire") {
		t.Fatalf("status %d, body leaks cause: %v", code, strings.Contains(body, "disk on fire"))
	}
}
