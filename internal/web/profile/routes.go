package profile

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	domain "github.com/kaecyra/gobbler/internal/profile"
	"github.com/kaecyra/gobbler/internal/session"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes. The layout owns the path, since
// its admission rule lets a non-member open it (layout.Admits).
const Pattern = layout.ProfilePath

// Store is what the pages use from the profile store. *profile.Store satisfies
// it.
type Store interface {
	Ensure(ctx context.Context, email string) (domain.Profile, error)
	ByAddress(ctx context.Context, address string) (domain.Profile, error)
	Rename(ctx context.Context, email, name string) error
}

// Households finds the household of an email, to tell whether the viewer and
// the person share one. *household.Store satisfies it.
type Households interface {
	ForEmail(ctx context.Context, email string) (household.Household, error)
}

// Deps is what the routes need. Profiles and Households are the page's own.
// The rest is for the sections that later nodes add, each of which builds its
// store from them (see the package documentation).
type Deps struct {
	Profiles   Store
	Households Households
	DB         *sql.DB
	Config     config.Config
	// Sessions is nil when sign-in is not mounted (package web checks).
	Sessions *session.Store
}

type handler struct{ Deps }

// Routes returns the profile routes, relative to Pattern.
//
//	GET  /                      sends the viewer to their own profile
//	GET  /{address}             a profile, as its viewer may see it
//	     /{address}/...         the sections, for the person themself only
//
// A profile is found by its address alone and the viewer's relation to its
// person is worked out from the request's identity, never from the request.
func Routes(d Deps) chi.Router {
	h := &handler{d}
	r := chi.NewRouter()
	r.Get("/", h.own)
	r.Route("/{address}", func(r chi.Router) {
		r.Use(h.subject)
		r.Get("/", h.show)
		r.Group(func(r chi.Router) {
			r.Use(selfOnly)
			h.selfRoutes(r)
		})
	})
	return r
}

// selfRoutes registers the routes of every self-only section, one line each
// (see the package documentation). They are behind selfOnly: another viewer,
// member or not, finds nothing.
func (h *handler) selfRoutes(r chi.Router) {
	h.nameRoutes(r)
	h.sessionsRoutes(r)
}

// redirectTo sends the browser to a profile page.
func redirectTo(w http.ResponseWriter, r *http.Request, address string) {
	http.Redirect(w, r, Pattern+"/"+address, http.StatusSeeOther)
}
