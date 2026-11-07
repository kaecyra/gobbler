package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apiprofile "github.com/kaecyra/gobbler/internal/api/profile"
	"github.com/kaecyra/gobbler/internal/web/layout"
	webprofile "github.com/kaecyra/gobbler/internal/web/profile"
)

// avatarSource tells the layout how to find the signed-in viewer's uploaded
// avatar for the header (ADR-00032, ADR-00033). The lookup is lazy: it is one
// profile read, made only when a page renders the header with a signed-in
// viewer, so a signed-out visitor, a fragment and a redirect cost nothing.
func avatarSource(profiles *apiprofile.Service, log *slog.Logger) func(http.Handler) http.Handler {
	find := func(ctx context.Context) string {
		c := caller.FromContext(ctx)
		if c.Person == "" {
			return ""
		}
		p, err := profiles.Mine(ctx, c)
		if err != nil {
			// A person who never opened their profile has none: not a failure.
			// Anything else falls back to the default avatar, and is logged.
			if !errors.Is(err, apiprofile.ErrNotFound) {
				log.WarnContext(ctx, "read profile for the header avatar", "error", err.Error())
			}
			return ""
		}
		// Through the visibility rule, as every view of a profile field is.
		if key := p.As(apiprofile.Self).Avatar; key != "" {
			return webprofile.AvatarSrc(p.Address, key)
		}
		return ""
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(layout.WithAvatarSource(r.Context(), find)))
		})
	}
}
