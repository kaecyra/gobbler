package recipeview

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/kaecyra/gobbler/internal/api/caller"
	"github.com/kaecyra/gobbler/internal/ingest"
)

// A recipe with a source URL can be re-imported from its source (ADR-00042).
// The ingest screens own the flow; this page only starts it, and says when one
// is waiting.

// reimportView is what the page shows of the recipe's pending re-import.
type reimportView struct {
	Token string
	// Host is where the source is, "example.com".
	Host string
	// Stale says the recipe was changed after the re-import started: it cannot
	// be saved, and the banner offers Discard only.
	Stale bool
}

// staleReimportBanner is shown when the recipe changed after the re-import
// started.
const staleReimportBanner = "The recipe changed since the re-import, so it cannot be saved. Discard it and re-import again."

// reimportStartHref is where the button posts. It is the ingest screen's, which
// fetches the source, drafts it and lands on the review screen.
func reimportStartHref(id int64) string { return "/ingest/reimport/" + strconv.FormatInt(id, 10) }

// reimportReviewHref is the ingest review screen of the pending re-import.
func reimportReviewHref(token string) string { return "/ingest/" + url.PathEscape(token) }

// reimportDiscardHref turns the pending re-import down; the ingest screen
// returns to the recipe.
func reimportDiscardHref(token string) string { return reimportReviewHref(token) + "/cancel" }

// reimportFor returns what the page shows of the recipe's pending re-import,
// nil when it has none.
func reimportFor(ctx context.Context, d Deps, id int64, sourceURL string, version int64) (*reimportView, error) {
	p, found, err := d.Recipes.PendingReimport(ctx, caller.FromContext(ctx), id)
	if err != nil {
		return nil, fmt.Errorf("load pending re-import of recipe %d: %w", id, err)
	}
	if !found {
		return nil, nil //nolint:nilnil // nil is "no re-import waiting"
	}
	return &reimportView{Token: p.Token, Host: ingest.SourceHost(sourceURL), Stale: p.StaleAt(version)}, nil
}
