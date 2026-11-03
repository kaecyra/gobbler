package profile

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// The personal API keys section (keys.templ): keys that act as the profile's
// person (ADR-00031). The profile page links to a page of their own, which
// lists them, makes them and revokes them. All of it is self-only: the owner
// is the profile's person, which selfOnly has shown is the viewer, and a
// request never names whose keys to touch.

const keyIDParam = "keyID"

// keysRoutes registers the section's routes under /{address}/keys.
func (h *handler) keysRoutes(r chi.Router) {
	r.Get("/keys", h.keysPage)
	r.Post("/keys", h.createKey)
	r.Post("/keys/{"+keyIDParam+"}/revoke", h.revokeKey)
}

// keys is the key store over the page's database.
func (h *handler) keys() *apikey.Store { return apikey.New(h.DB) }

func selfSubject(r *http.Request) (subject, error) {
	s, ok := subjectFrom(r.Context())
	if !ok || !s.Self() {
		return subject{}, fmt.Errorf("keys without the profile's person: %w", layout.ErrNotFound)
	}
	return s, nil
}

func (h *handler) keysPage(w http.ResponseWriter, r *http.Request) {
	s, err := selfSubject(r)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	v := newView(s)
	keys, err := h.keys().ListPersonal(r.Context(), s.Profile.Email)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	rows := layout.NewAPIKeyRows(keys, time.Now(), false, v.keyRevokePath)
	layout.Render(w, r, http.StatusOK, keysPage(v, rows))
}

func (h *handler) createKey(w http.ResponseWriter, r *http.Request) {
	s, err := selfSubject(r)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	life, err := apikey.LifetimeFor(r.PostFormValue("lifetime"))
	var secret string
	if err == nil {
		secret, err = h.keys().CreatePersonal(r.Context(), s.Profile.Email, r.PostFormValue("name"), life.Duration())
	}
	if err != nil {
		if reason, ok := apikey.InvalidReason(err); ok {
			layout.Render(w, r, http.StatusBadRequest, layout.ErrorPage(http.StatusBadRequest, reason))
			return
		}
		layout.WriteError(w, r, err)
		return
	}
	// The secret is on this response only: it must not be stored by a cache or
	// the browser's history.
	w.Header().Set("Cache-Control", "no-store")
	layout.Render(w, r, http.StatusOK, layout.APIKeyCreatedPage(layout.APIKeyCreated{
		Name: r.PostFormValue("name"), Secret: secret,
		BackHref: newView(s).sectionPath("keys"), BackLabel: "Back to your keys",
	}))
}

func (h *handler) revokeKey(w http.ResponseWriter, r *http.Request) {
	s, err := selfSubject(r)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, keyIDParam), 10, 64)
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("key id %q: %w", chi.URLParam(r, keyIDParam), layout.ErrNotFound))
		return
	}
	if err := h.keys().RevokePersonal(r.Context(), s.Profile.Email, id); err != nil {
		layout.WriteError(w, r, err)
		return
	}
	http.Redirect(w, r, Pattern+"/"+s.Profile.Address+"/keys", http.StatusSeeOther)
}

func (v pageView) keyRevokePath(id int64) string {
	return v.sectionPath("keys") + "/" + strconv.FormatInt(id, 10) + "/revoke"
}
