package web

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// draftCounter is what the nav's count needs from the recipe service
// (*apirecipes.Service satisfies it).
type draftCounter interface {
	WaitingDrafts(ctx context.Context, c caller.Caller) (int, error)
}

// waitingDrafts tells the layout how to count the signed-in member's
// household's drafts waiting for review, for the badge on the import entry of
// the nav (ADR-00049). Like the avatar, the count is lazy: one COUNT query,
// made only when a page renders the nav for a member of a household.
func waitingDrafts(drafts draftCounter, log *slog.Logger) func(http.Handler) http.Handler {
	count := func(ctx context.Context) int {
		c := caller.FromContext(ctx)
		if _, member := apihouseholds.FromContext(ctx); !member || c.Person == "" {
			return 0
		}
		n, err := drafts.WaitingDrafts(ctx, c)
		if err != nil {
			// The badge is a reminder on a page that works without it: the
			// page is shown with no badge, and the failure is logged.
			log.WarnContext(ctx, "count the drafts waiting for review for the nav", "error", err.Error())
			return 0
		}
		return n
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(layout.WithWaitingDrafts(r.Context(), count)))
		})
	}
}
