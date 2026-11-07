package profile

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apiprofile "github.com/kaecyra/gobbler/internal/api/profile"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// subject is the profile a request is about and how its viewer stands to the
// person. The subject middleware puts it on the request; sections read it with
// subjectFrom.
type subject = apiprofile.Subject

type subjectKey struct{}

func subjectFrom(ctx context.Context) (subject, bool) {
	s, ok := ctx.Value(subjectKey{}).(subject)
	return s, ok
}

// callerOf is who the request is from: the verified identity the middleware
// stored. The service checks it (ADR-00043).
func callerOf(r *http.Request) apiprofile.Caller { return caller.FromContext(r.Context()) }

// subject loads the profile the address names and works out the viewer's
// relation to its person. An address that names nothing is 404 for everyone:
// the response says nothing about which addresses exist.
func (h *handler) subject(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.EmailFromContext(r.Context()); !ok {
			layout.WriteError(w, r, fmt.Errorf("no identity on the request: %w", layout.ErrNotFound))
			return
		}
		s, err := h.Profile.Subject(r.Context(), callerOf(r), chi.URLParam(r, "address"))
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), subjectKey{}, s)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// selfOnly refuses a viewer who is not the profile's person with the same 404
// as an unknown address, so another member learns nothing from probing a
// section. It must run after the subject middleware.
func selfOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := subjectFrom(r.Context())
		if !ok || !s.Self() {
			layout.WriteError(w, r, fmt.Errorf("profile section of another person: %w", layout.ErrNotFound))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// own sends the viewer to their own profile, creating it the first time.
func (h *handler) own(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.EmailFromContext(r.Context()); !ok {
		layout.WriteError(w, r, fmt.Errorf("no identity on the request: %w", layout.ErrNotFound))
		return
	}
	p, err := h.Profile.Own(r.Context(), callerOf(r))
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	redirectTo(w, r, p.Address)
}

func (h *handler) show(w http.ResponseWriter, r *http.Request) {
	s, ok := subjectFrom(r.Context())
	if !ok {
		layout.WriteError(w, r, fmt.Errorf("no profile on the request: %w", layout.ErrNotFound))
		return
	}
	v := newView(s)
	if v.Self {
		name, err := h.Profile.HouseholdName(r.Context(), callerOf(r))
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		v.Household = name
	}
	layout.Render(w, r, http.StatusOK, page(v))
}
