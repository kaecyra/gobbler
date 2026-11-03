package profile

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	domain "github.com/kaecyra/gobbler/internal/profile"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// The display name section (name.templ): the form that sets it.

// nameRoutes registers POST /{address}/name.
func (h *handler) nameRoutes(r chi.Router) {
	r.Post("/name", h.rename)
}

func (h *handler) rename(w http.ResponseWriter, r *http.Request) {
	s, ok := subjectFrom(r.Context())
	if !ok || !s.Self() {
		layout.WriteError(w, r, fmt.Errorf("rename without the profile's person: %w", layout.ErrNotFound))
		return
	}
	// The person is the profile's, which selfOnly has shown is the viewer; the
	// request never names whose name to set.
	if err := h.Profiles.Rename(r.Context(), s.Profile.Email, r.PostFormValue("name")); err != nil {
		if reason, ok := domain.InvalidReason(err); ok {
			layout.Render(w, r, http.StatusBadRequest, layout.ErrorPage(http.StatusBadRequest, reason))
			return
		}
		layout.WriteError(w, r, err)
		return
	}
	redirectTo(w, r, s.Profile.Address)
}
