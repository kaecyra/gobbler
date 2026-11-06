package mcp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// sourcedRecipe stores a recipe that was imported from a page, with a picture
// row, and returns its id.
func (s *site) sourcedRecipe(email string) int64 {
	s.t.Helper()
	ctx := s.as(s.smiths, email)
	id, err := s.rec.Create(ctx, recipe.Recipe{Name: "Pancakes", SourceURL: "https://example.test/pancakes",
		Components: []recipe.Component{{Lines: []recipe.IngredientLine{s.flourLine(2)}, Steps: []recipe.Step{{ID: "s1", Text: "Mix."}}}}})
	if err != nil {
		s.t.Fatal(err)
	}
	if _, err := s.d.Exec(`INSERT INTO recipe_pictures (recipe_id, household_id, key, updated_at) VALUES (?, ?, 'recipes/1/v.webp', 'x')`, id, s.smiths.ID); err != nil {
		s.t.Fatal(err)
	}
	return id
}

func (s *site) pictureKey(id int64) string {
	s.t.Helper()
	var k string
	if err := s.d.QueryRow(`SELECT key FROM recipe_pictures WHERE recipe_id = ?`, id).Scan(&k); err != nil {
		s.t.Fatal(err)
	}
	return k
}

// ADR-00042 obligation 10.
func TestReimportRecipeHoldsADraftForAPersonAndChangesNothing(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	id := s.sourcedRecipe("ann@example.com")
	secret, key := s.personalKey("ann@example.com")
	cs := s.connect(secret)
	var out struct {
		ReviewPath string `json:"review_path"`
		Token      string `json:"token"`
		Note       string `json:"note"`
	}
	if msg := call(t, cs, "reimport_recipe", map[string]any{"id": id}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.ReviewPath != "/ingest/"+out.Token || !strings.Contains(out.Note, "unchanged until a person") {
		t.Errorf("result = %+v, want the review path and a note that a person decides", out)
	}
	ctx := s.as(s.smiths, "ann@example.com")
	p, err := s.rec.GetProposal(ctx, out.Token)
	if err != nil || p.State != store.ProposalPending || p.Reimport == nil || p.Reimport.RecipeID != id || p.Reimport.BaseVersion != 1 {
		t.Fatalf("proposal = %+v (%v), want a pending re-import of recipe %d at version 1", p, err, id)
	}
	if p.Tool != "reimport_recipe" || p.Reason != "Re-imported from example.test" || p.Credential == nil {
		t.Errorf("provenance = %+v, want the tool, the reason and the credential", p.Provenance)
	}
	info, _ := s.rec.Info(ctx, id)
	cur, _ := s.rec.Get(ctx, id)
	if info.Version != 1 || cur.Name != "Pancakes" || s.pictureKey(id) != "recipes/1/v.webp" {
		t.Errorf("recipe at version %d, name %q, picture %q; want all unchanged", info.Version, cur.Name, s.pictureKey(id))
	}
	if w, _ := s.keys.Writes(context.Background(), key); len(w) != 1 || w[0].Tool != "reimport_recipe" {
		t.Errorf("writes = %+v, want the re-import recorded", w)
	}
	// It is the review screen's draft: the same token reads as a pending draft.
	var st proposalState
	if msg := call(t, cs, "get_recipe_proposal", map[string]any{"token": out.Token}, &st); msg != "" || st.State != "pending" {
		t.Errorf("get_recipe_proposal = %+v %q, want pending", st, msg)
	}
}

func TestNoMCPToolSavesADraft(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	secret, _ := s.personalKey("ann@example.com")
	res, err := s.connect(secret).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		for _, bad := range []string{"save", "confirm", "accept", "approve", "apply"} {
			if strings.Contains(tool.Name, bad) {
				t.Errorf("tool %s could save a draft", tool.Name)
			}
		}
	}
}

func TestReimportRecipeRefusals(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	bare := s.recipe(s.smiths, "ann@example.com", "Typed in")
	theirs := s.recipe(s.joneses, "jo@example.com", "Jones")
	secret, _ := s.personalKey("ann@example.com")
	cs := s.connect(secret)
	for name, c := range map[string]struct {
		args map[string]any
		want string
	}{
		"no source":   {map[string]any{"id": bare}, "no source address"},
		"other house": {map[string]any{"id": theirs}, "not found"},
		"missing":     {map[string]any{"id": 9999}, "not found"},
		"no id":       {map[string]any{"id": 0}, "give the recipe's id"},
		"negative":    {map[string]any{"id": -4}, "give the recipe's id"},
	} {
		if msg := call(t, cs, "reimport_recipe", c.args, nil); !strings.Contains(msg, c.want) {
			t.Errorf("%s: refusal = %q, want it to mention %q", name, msg, c.want)
		}
	}
	var n int
	if err := s.d.QueryRow(`SELECT COUNT(*) FROM proposals`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d proposals held by refused re-imports (%v), want none", n, err)
	}
}
