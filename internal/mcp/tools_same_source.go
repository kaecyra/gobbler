package mcp

import (
	"context"

	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/recipepath"
)

// sameSourceOut is what the household already holds from the address an
// import or preview reads: its saved recipes and its pending drafts whose
// source address is the same, fragments ignored (ADR-00048). Both are left
// out when there are none. It is reported, never a refusal; the agent decides.
type sameSourceOut struct {
	ExistingRecipes []existingRecipeOut `json:"existing_recipes,omitempty" jsonschema:"the household's saved recipes imported from the same address, ignoring any #fragment"`
	PendingDrafts   []pendingDraftOut   `json:"pending_drafts,omitempty" jsonschema:"the household's drafts waiting for review read from the same address, ignoring any #fragment"`
}

type existingRecipeOut struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Path is the recipe's page (ADR-00047).
	Path string `json:"path"`
}

type pendingDraftOut struct {
	Token      string `json:"token"`
	Name       string `json:"name"`
	ReviewPath string `json:"review_path"`
}

// sameSource is what the household rc names already holds from rawURL.
func (t *tools) sameSource(ctx context.Context, rc apirecipes.Caller, rawURL string) (sameSourceOut, error) {
	same, err := t.Recipes.SameSource(ctx, rc, rawURL)
	if err != nil {
		return sameSourceOut{}, err
	}
	var out sameSourceOut
	for _, r := range same.Recipes {
		out.ExistingRecipes = append(out.ExistingRecipes, existingRecipeOut{ID: r.ID, Name: r.Name, Path: recipepath.View(r.ID, r.Name)})
	}
	for _, d := range same.Drafts {
		out.PendingDrafts = append(out.PendingDrafts, pendingDraftOut{Token: d.Token, Name: d.Name, ReviewPath: ReviewPath + d.Token})
	}
	return out, nil
}
