package web

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/profile"
	"github.com/kaecyra/gobbler/internal/web/layout"
	webprofile "github.com/kaecyra/gobbler/internal/web/profile"
)

// avatarSource tells the layout how to find the signed-in viewer's uploaded
// avatar for the header (ADR-00032, ADR-00033). The lookup is lazy: it is one
// profile read, made only when a page renders the header with a signed-in
// viewer, so a signed-out visitor, a fragment and a redirect cost nothing.
func avatarSource(d *sql.DB, log *slog.Logger) func(http.Handler) http.Handler {
	profiles := profile.New(d)
	find := func(ctx context.Context) string {
		email, ok := auth.EmailFromContext(ctx)
		if !ok {
			return ""
		}
		p, err := profiles.ByEmail(ctx, email)
		if err != nil {
			// A person who never opened their profile has none: not a failure.
			// Anything else falls back to the default avatar, and is logged.
			if !errors.Is(err, profile.ErrNotFound) {
				log.WarnContext(ctx, "read profile for the header avatar", "error", err.Error())
			}
			return ""
		}
		// Through the visibility rule, as every view of a profile field is.
		if key := p.As(profile.Self).Avatar; key != "" {
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

// avatarsFor is the avatar service of the profile pages: the one the server
// built and started, or a local-directory one with no resumption when the
// caller gave none.
func avatarsFor(d Deps) *profile.Avatars {
	if d.Avatars != nil {
		return d.Avatars
	}
	local := media.NewLocal(filepath.Join(d.Config.Server.DataDir, media.LocalDirName))
	return profile.NewAvatars(context.Background(), profile.New(d.DB), local, d.Logger)
}
