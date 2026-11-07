package profile

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	apiprofile "github.com/kaecyra/gobbler/internal/api/profile"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes. The layout owns the path, since
// its admission rule lets a non-member open it (layout.Admits).
const Pattern = layout.ProfilePath

// Deps is what the routes need. The profile service does every read and write
// (ADR-00043): the handlers hold no store.
type Deps struct {
	Profile *apiprofile.Service
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
		h.avatarServeRoutes(r)
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
	h.keysRoutes(r)
	h.grantsRoutes(r)
	h.avatarRoutes(r)
}

// redirectTo sends the browser to a profile page.
func redirectTo(w http.ResponseWriter, r *http.Request, address string) {
	http.Redirect(w, r, Pattern+"/"+address, http.StatusSeeOther)
}
