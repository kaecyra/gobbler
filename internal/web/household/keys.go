package household

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// The household keys section (keys.templ): API keys that act for the household
// rather than as a member (ADR-00031). Any member may make or revoke one, so
// the household is the request's, never one the request names (ADR-00018).

const (
	keysPath   = Pattern + "/keys"
	keyIDParam = "keyID"
)

func (h *handler) keyRoutes(r chi.Router) {
	r.Post("/keys", h.createKey)
	r.Post("/keys/{"+keyIDParam+"}/revoke", h.revokeKey)
}

func keyRevokePath(id int64) string {
	return keysPath + "/" + strconv.FormatInt(id, 10) + "/revoke"
}

func (h *handler) createKey(w http.ResponseWriter, r *http.Request) {
	email, hh, err := actor(r)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	life, err := apikey.LifetimeFor(r.PostFormValue("lifetime"))
	if err != nil {
		h.keyRefused(w, r, err)
		return
	}
	secret, err := h.Keys.CreateHousehold(r.Context(), hh.ID, email, r.PostFormValue("name"), life.Duration())
	if err != nil {
		h.keyRefused(w, r, err)
		return
	}
	// The secret is on this response only: it must not be stored by a cache or
	// the browser's history.
	w.Header().Set("Cache-Control", "no-store")
	layout.Render(w, r, http.StatusOK, keyCreatedPage(createdView{Name: r.PostFormValue("name"), Secret: secret, Household: hh.Name}))
}

// keyRefused answers input the store refused with its reason, and anything
// else as the failure it is.
func (h *handler) keyRefused(w http.ResponseWriter, r *http.Request, err error) {
	if reason, ok := apikey.InvalidReason(err); ok {
		layout.Render(w, r, http.StatusBadRequest, layout.ErrorPage(http.StatusBadRequest, reason))
		return
	}
	layout.Fail(w, r, err)
}

func (h *handler) revokeKey(w http.ResponseWriter, r *http.Request) {
	_, hh, err := actor(r)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, keyIDParam), 10, 64)
	if err != nil {
		layout.Fail(w, r, layout.ErrNotFound)
		return
	}
	done(w, r, h.Keys.RevokeHousehold(r.Context(), hh.ID, id))
}

// keyRow is one key as the section lists it.
type keyRow struct {
	ID        int64
	Name      string
	Prefix    string
	CreatedBy string
	Created   string
	Expiry    string
	Used      string
}

func newKeyRows(keys []apikey.Key, now time.Time) []keyRow {
	rows := make([]keyRow, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, keyRow{
			ID: k.ID, Name: k.Name, Prefix: k.Prefix, CreatedBy: k.CreatedBy,
			Created: k.CreatedText(), Expiry: k.ExpiryText(now), Used: k.UsedText(),
		})
	}
	return rows
}

// createdView is the page that shows a new key's secret, once.
type createdView struct {
	Name      string
	Secret    string
	Household string
}
