package profile

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/oauth"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// The connected apps section (grants.templ): the OAuth grants that act as the
// profile's person (ADR-00034), listed and revoked beside the API keys. Like
// the keys it is self-only: the owner is the profile's person, which selfOnly
// has shown is the viewer, and a request never names whose grants to touch.

const grantIDParam = "grantID"

// grantsRoutes registers the section's routes under /{address}/grants.
func (h *handler) grantsRoutes(r chi.Router) {
	r.Get("/grants", h.grantsPage)
	r.Post("/grants/{"+grantIDParam+"}/revoke", h.revokeGrant)
}

// grantRow is one grant as the list shows it.
type grantRow struct {
	Name       string
	ReturnHost string
	Made       string
	RevokePath string
}

func (h *handler) grantsPage(w http.ResponseWriter, r *http.Request) {
	s, err := selfSubject(r)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	v := newView(s)
	grants, err := oauth.New(h.DB).ListGrants(r.Context(), s.Profile.Email)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	rows := make([]grantRow, 0, len(grants))
	for _, g := range grants {
		rows = append(rows, grantRow{
			Name: g.ClientName, ReturnHost: g.ReturnHost, Made: g.CreatedText(),
			RevokePath: v.sectionPath("grants") + "/" + strconv.FormatInt(g.ID, 10) + "/revoke",
		})
	}
	layout.Render(w, r, http.StatusOK, grantsPage(v, rows))
}

func (h *handler) revokeGrant(w http.ResponseWriter, r *http.Request) {
	s, err := selfSubject(r)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, grantIDParam), 10, 64)
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("grant id %q: %w", chi.URLParam(r, grantIDParam), layout.ErrNotFound))
		return
	}
	if err := oauth.New(h.DB).RevokeGrant(r.Context(), s.Profile.Email, id); err != nil {
		layout.WriteError(w, r, err)
		return
	}
	http.Redirect(w, r, newView(s).sectionPath("grants"), http.StatusSeeOther)
}
