package mcp_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// picturePage serves a recipe page naming one picture large enough to keep.
func picturePage(t *testing.T) *httptest.Server {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2200, 700))
	for y := range 700 {
		for x := range 2200 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	var pic bytes.Buffer
	if err := png.Encode(&pic, img); err != nil {
		t.Fatal(err)
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".png") {
			_, _ = w.Write(pic.Bytes())
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(w, `<html><head><meta property="og:image" content="%[1]s/hero.png"><script type="application/ld+json">
{"@context":"https://schema.org","@type":"Recipe","name":"Pancakes","image":["%[1]s/hero.png"],"recipeIngredient":["1 cup flour"],"recipeInstructions":["Mix."]}
</script></head><body></body></html>`, srv.URL)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// ADR-00051: the tool offers what it found on the edit page and changes no
// recipe.
func TestFindRecipePicturesOffersThemAndChangesNoRecipe(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	srv := picturePage(t)
	ctx := s.as(s.smiths, "ann@example.com")
	id, err := s.rec.Create(ctx, recipe.Recipe{Name: "Pancakes", SourceURL: srv.URL + "/pancakes",
		Components: []recipe.Component{{Lines: []recipe.IngredientLine{s.flourLine(2)}, Steps: []recipe.Step{{ID: "s1", Text: "Mix."}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.d.Exec(`INSERT INTO recipe_pictures (recipe_id, household_id, key, updated_at) VALUES (?, ?, 'recipes/1/v.webp', 'x')`, id, s.smiths.ID); err != nil {
		t.Fatal(err)
	}
	secret, key := s.personalKey("ann@example.com")
	cs := s.connect(secret)
	var out struct {
		Found    int    `json:"found"`
		EditPath string `json:"edit_path"`
		Note     string `json:"note"`
	}
	if msg := call(t, cs, "find_recipe_pictures", map[string]any{"recipe_id": id}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Found != 1 || out.EditPath != fmt.Sprintf("/recipes/%d/edit", id) || !strings.Contains(out.Note, "unchanged") || !strings.Contains(out.Note, "for 14 days") {
		t.Errorf("result = %+v, want 1 found, the edit path and a note", out)
	}
	var offered int
	if err := s.d.QueryRow(`SELECT count(*) FROM recipe_found_pictures WHERE recipe_id = ?`, id).Scan(&offered); err != nil || offered != 1 {
		t.Errorf("offered rows = %d (%v), want 1", offered, err)
	}
	info, err := s.rec.Info(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != 1 || s.pictureKey(id) != "recipes/1/v.webp" {
		t.Errorf("recipe at version %d with picture %q; want both unchanged", info.Version, s.pictureKey(id))
	}
	var proposals int
	if err := s.d.QueryRow(`SELECT count(*) FROM proposals`).Scan(&proposals); err != nil || proposals != 0 {
		t.Errorf("proposals = %d (%v), want none: the tool needs no review", proposals, err)
	}
	w, err := s.keys.Writes(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if len(w) != 1 || w[0].Tool != "find_recipe_pictures" {
		t.Errorf("writes = %+v, want the search recorded", w)
	}
}

func TestFindRecipePicturesRefusesAnotherHouseholdsRecipe(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	id := s.sourcedRecipe("ann@example.com")
	secret, _ := s.personalKey("jo@example.com")
	cs := s.connect(secret)
	if msg := call(t, cs, "find_recipe_pictures", map[string]any{"recipe_id": id}, nil); msg != "not found" {
		t.Errorf("another household's recipe = %q, want not found", msg)
	}
}
