package profile

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/household"
	domain "github.com/kaecyra/gobbler/internal/profile"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// subject is the profile a request is about and how its viewer stands to the
// person. The subject middleware puts it on the request; sections read it with
// subjectFrom.
type subject struct {
	Profile  domain.Profile
	Viewer   string
	Relation domain.Relation
}

// Self reports whether the viewer is the person whose profile it is.
func (s subject) Self() bool { return s.Relation == domain.Self }

type subjectKey struct{}

func subjectFrom(ctx context.Context) (subject, bool) {
	s, ok := ctx.Value(subjectKey{}).(subject)
	return s, ok
}

// subject loads the profile the address names and works out the viewer's
// relation to its person. An address that names nothing is 404 for everyone:
// the response says nothing about which addresses exist.
func (h *handler) subject(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		viewer, ok := auth.EmailFromContext(r.Context())
		if !ok {
			layout.WriteError(w, r, fmt.Errorf("no identity on the request: %w", layout.ErrNotFound))
			return
		}
		p, err := h.Profiles.ByAddress(r.Context(), chi.URLParam(r, "address"))
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		rel, err := h.relation(r.Context(), viewer, p.Email)
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), subjectKey{}, subject{Profile: p, Viewer: viewer, Relation: rel})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// relation is how the viewer stands to the person. The viewer's household is
// the one the membership middleware stored on the request; the person's is
// read from the store. Someone with no household shares one with nobody.
func (h *handler) relation(ctx context.Context, viewer, person string) (domain.Relation, error) {
	if rel := domain.RelationBetween(viewer, person, false); rel == domain.Self {
		return rel, nil
	}
	mine, member := household.FromContext(ctx)
	if !member {
		return domain.RelationBetween(viewer, person, false), nil
	}
	theirs, err := h.Households.ForEmail(ctx, person)
	switch {
	case err == nil:
	case errors.Is(err, household.ErrNotFound), errors.Is(err, household.ErrInvalid):
		return domain.RelationBetween(viewer, person, false), nil
	default:
		return "", fmt.Errorf("household of the profile's person: %w", err)
	}
	return domain.RelationBetween(viewer, person, theirs.ID == mine.ID), nil
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
	viewer, ok := auth.EmailFromContext(r.Context())
	if !ok {
		layout.WriteError(w, r, fmt.Errorf("no identity on the request: %w", layout.ErrNotFound))
		return
	}
	p, err := h.Profiles.Ensure(r.Context(), viewer)
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
	layout.Render(w, r, http.StatusOK, page(newView(s)))
}
