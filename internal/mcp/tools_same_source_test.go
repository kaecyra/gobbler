package mcp_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipepath"
)

// sameSource is the duplicate report import_recipe and preview_import give
// with a url (ADR-00048).
type sameSource struct {
	ExistingRecipes []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Path string `json:"path"`
	} `json:"existing_recipes"`
	PendingDrafts []struct {
		Token      string `json:"token"`
		Name       string `json:"name"`
		ReviewPath string `json:"review_path"`
	} `json:"pending_drafts"`
	ReviewPath string `json:"review_path"`
}

func (r sameSource) recipeIDs() []int64 {
	var ids []int64
	for _, x := range r.ExistingRecipes {
		ids = append(ids, x.ID)
	}
	return ids
}

func (r sameSource) draftTokens() []string {
	var tokens []string
	for _, x := range r.PendingDrafts {
		tokens = append(tokens, x.Token)
	}
	return tokens
}

// sourced saves a recipe in h imported from sourceURL.
func (s *site) sourced(h household.Household, email, name, sourceURL string) int64 {
	s.t.Helper()
	id, err := s.rec.Create(s.as(h, email), recipe.Recipe{
		Name: name, SourceURL: sourceURL,
		Components: []recipe.Component{{Steps: []recipe.Step{{ID: "s1", Text: "Mix."}}}},
	})
	if err != nil {
		s.t.Fatal(err)
	}
	return id
}

// ADR-00048 obligation 8: import_recipe given a url reports the household's
// saved recipe and pending draft from the same address, ignoring a fragment,
// and nothing for another address or from another household.
func TestImportReportsWhatTheHouseholdHoldsFromTheAddress(t *testing.T) {
	s := previewSite(t)
	p := servePreviewPage(t)
	saved := s.sourced(s.smiths, "ann@example.com", "Saved Toast", p.url()+"#recipe")
	s.sourced(s.joneses, "jo@example.com", "Their Toast", p.url())
	ann, _ := s.personalKey("ann@example.com")
	jo, _ := s.personalKey("jo@example.com")
	if msg := call(t, s.connect(jo), "import_recipe", map[string]any{"url": p.url()}, nil); msg != "" {
		t.Fatal(msg)
	}
	cs := s.connect(ann)

	var first sameSource
	if msg := call(t, cs, "import_recipe", map[string]any{"url": p.url() + "#step-1"}, &first); msg != "" {
		t.Fatal(msg)
	}
	if got := first.recipeIDs(); !reflect.DeepEqual(got, []int64{saved}) {
		t.Errorf("first import reports recipes %v, want only the Smiths' %d", got, saved)
	}
	if r := first.ExistingRecipes; len(r) == 1 && (r[0].Name != "Saved Toast" || r[0].Path != recipepath.View(saved, "Saved Toast")) {
		t.Errorf("reported recipe = %+v, want its name and view path", r[0])
	}
	if len(first.PendingDrafts) != 0 {
		t.Errorf("first import reports drafts %v, want none: not itself, nor the Joneses' draft", first.draftTokens())
	}
	firstToken := strings.TrimPrefix(first.ReviewPath, "/ingest/")

	var second sameSource
	if msg := call(t, cs, "import_recipe", map[string]any{"url": p.url()}, &second); msg != "" {
		t.Fatal(msg)
	}
	if got := second.recipeIDs(); !reflect.DeepEqual(got, []int64{saved}) {
		t.Errorf("second import reports recipes %v, want %d", got, saved)
	}
	if got := second.draftTokens(); !reflect.DeepEqual(got, []string{firstToken}) {
		t.Errorf("second import reports drafts %v, want only the first import %s", got, firstToken)
	}
	if d := second.PendingDrafts; len(d) == 1 && (d[0].Name != "Flour Toast" || d[0].ReviewPath != first.ReviewPath) {
		t.Errorf("reported draft = %+v, want its name and review path", d[0])
	}

	var other map[string]any
	if msg := call(t, cs, "import_recipe", map[string]any{"url": p.srv.URL + "/other-toast"}, &other); msg != "" {
		t.Fatal(msg)
	}
	for _, k := range []string{"existing_recipes", "pending_drafts"} {
		if v, ok := other[k]; ok {
			t.Errorf("import of another address reports %s = %v", k, v)
		}
	}
}

// ADR-00048 obligation 8: preview_import given a url reports the household's
// saved recipe and pending draft from the same address, ignoring a fragment,
// and nothing for another address or from another household.
func TestPreviewReportsWhatTheHouseholdHoldsFromTheAddress(t *testing.T) {
	s := previewSite(t)
	p := servePreviewPage(t)
	saved := s.sourced(s.smiths, "ann@example.com", "Saved Toast", p.url())
	s.sourced(s.joneses, "jo@example.com", "Their Toast", p.url())
	ann, _ := s.personalKey("ann@example.com")
	jo, _ := s.personalKey("jo@example.com")
	if msg := call(t, s.connect(jo), "import_recipe", map[string]any{"url": p.url()}, nil); msg != "" {
		t.Fatal(msg)
	}
	cs := s.connect(ann)
	var held sameSource
	if msg := call(t, cs, "import_recipe", map[string]any{"url": p.url() + "#jump"}, &held); msg != "" {
		t.Fatal(msg)
	}
	heldToken := strings.TrimPrefix(held.ReviewPath, "/ingest/")

	var got sameSource
	if msg := call(t, cs, "preview_import", map[string]any{"url": p.url() + "#recipe"}, &got); msg != "" {
		t.Fatal(msg)
	}
	if ids := got.recipeIDs(); !reflect.DeepEqual(ids, []int64{saved}) {
		t.Errorf("preview reports recipes %v, want only the Smiths' %d", ids, saved)
	}
	if r := got.ExistingRecipes; len(r) == 1 && r[0].Path != recipepath.View(saved, "Saved Toast") {
		t.Errorf("reported recipe path = %q, want its view path", r[0].Path)
	}
	if tokens := got.draftTokens(); !reflect.DeepEqual(tokens, []string{heldToken}) {
		t.Errorf("preview reports drafts %v, want only the Smiths' %s", tokens, heldToken)
	}

	var other map[string]any
	if msg := call(t, cs, "preview_import", map[string]any{"url": p.srv.URL + "/other-toast"}, &other); msg != "" {
		t.Fatal(msg)
	}
	for _, k := range []string{"existing_recipes", "pending_drafts"} {
		if v, ok := other[k]; ok {
			t.Errorf("preview of another address reports %s = %v", k, v)
		}
	}
}
