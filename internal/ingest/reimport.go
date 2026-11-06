package ingest

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// A re-import runs ingestion's whole flow on a saved recipe's source URL and
// holds the result as a draft that replaces the recipe when a person saves it
// on the review screen (ADR-00042). Only the save differs from an import
// (Saver.SaveProposed).

// Reimport names the recipe a draft replaces and the version of it the
// re-import started from. The store refuses the draft, and its save, once the
// recipe is at another version.
type Reimport struct {
	RecipeID, BaseVersion int64
}

// ErrNoSourceURL is returned by Reimporter.Start for a recipe with no source
// URL to fetch again, such as one made from pasted text or a photo.
var ErrNoSourceURL = errors.New("this recipe has no source address to import again")

// ReimportRecipes is what a Reimporter reads of the saved recipe;
// *store.Store satisfies it.
type ReimportRecipes interface {
	Get(ctx context.Context, id int64) (recipe.Recipe, error)
	Info(ctx context.Context, id int64) (store.Info, error)
	// PendingReimport finds the re-import this one will supersede.
	PendingReimport(ctx context.Context, id int64) (store.Proposal, bool, error)
}

// URLDrafter drafts the recipe on a page; *Pipeline satisfies it. It is the
// rule-first pipeline, so a re-import reaches the LLM only after the rules
// (ADR-00006).
type URLDrafter interface {
	FromURL(ctx context.Context, rawURL string) (Draft, error)
}

// Reimporter starts re-imports.
type Reimporter struct {
	recipes ReimportRecipes
	drafter URLDrafter
	drafts  *Drafts
}

// NewReimporter returns a Reimporter that drafts with drafter and holds the
// draft in drafts.
func NewReimporter(r ReimportRecipes, d URLDrafter, drafts *Drafts) *Reimporter {
	return &Reimporter{recipes: r, drafter: d, drafts: drafts}
}

// SourceHost is the host of a recipe's source URL, empty when it has none.
func SourceHost(sourceURL string) string {
	u, err := url.Parse(sourceURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// Start fetches recipe id's source again, drafts it and holds the draft for
// review, returning its token. A fetch or pipeline failure is returned and
// nothing is held. The draft keeps the recipe's spice level, which no source
// carries, and starts with "Keep current picture". It supersedes the recipe's
// pending edit proposal or re-import. by.Reason is set to say where the
// re-import came from.
func (r *Reimporter) Start(ctx context.Context, id int64, by store.Provenance) (string, error) {
	// The version is read before the recipe: an edit between the two leaves the
	// draft older than the recipe, which the store refuses at save.
	info, err := r.recipes.Info(ctx, id)
	if err != nil {
		return "", fmt.Errorf("read recipe %d to re-import: %w", id, err)
	}
	rec, err := r.recipes.Get(ctx, id)
	if err != nil {
		return "", fmt.Errorf("read recipe %d to re-import: %w", id, err)
	}
	if rec.SourceURL == "" {
		return "", fmt.Errorf("re-import recipe %d: %w", id, ErrNoSourceURL)
	}
	d, err := r.drafter.FromURL(ctx, rec.SourceURL)
	if err != nil {
		return "", fmt.Errorf("re-import recipe %d from %s: %w", id, rec.SourceURL, err)
	}
	d.Reimport = &Reimport{RecipeID: id, BaseVersion: info.Version}
	d.Picture = PictureKeep
	if d.SpiceLevel == nil {
		d.SpiceLevel = rec.SpiceLevel
	}
	by.Reason = "Re-imported from " + SourceHost(rec.SourceURL)
	old, found, err := r.recipes.PendingReimport(ctx, id)
	if err != nil {
		return "", fmt.Errorf("read the pending re-import of recipe %d: %w", id, err)
	}
	token, err := r.drafts.Add(ctx, d, by)
	if err != nil {
		return "", fmt.Errorf("hold re-import of recipe %d: %w", id, err)
	}
	if found {
		// Adding the new draft superseded the old one; its candidates are
		// deleted now rather than when the proposal is pruned (ADR-00042).
		r.drafts.dropCandidates(ctx, old.ID)
	}
	return token, nil
}
