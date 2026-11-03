package profile

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/session"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// The sessions section (sessions.templ, ADR-00032): the person's signed-in
// sessions, which they can end. It is self-only: the section renders for the
// profile's person alone and every route is behind selfOnly.
//
// The page only holds a slot that loads the list, so the section needs nothing
// of the page's view. The IP and the user agent in the list are shown to the
// person for their own information and are never used to allow or refuse
// anything (ADR-00027).

// errNoCurrent is an "end every other session" from a request that carries no
// live session of its own, such as under the dev bypass, where there is no
// session to keep.
var errNoCurrent = errors.New("no current session to keep")

// sessionsRoutes registers GET /{address}/sessions, which serves the list,
// and the two POSTs that end sessions.
func (h *handler) sessionsRoutes(r chi.Router) {
	r.Get("/sessions", h.sessionList)
	r.Post("/sessions/end", h.sessionEnd)
	r.Post("/sessions/end-others", h.sessionEndOthers)
}

// sessionRow is one session as the list shows it.
type sessionRow struct {
	ID       string
	Device   string
	IP       string
	Started  time.Time
	LastUsed time.Time
	Current  bool
}

// sessionsView is the list. Kept is false when sign-in sessions are not in
// use at all (the dev bypass without sign-in), where there is nothing to list.
type sessionsView struct {
	Address string
	Kept    bool
	Rows    []sessionRow
}

func (v sessionsView) path(tail string) string { return Pattern + "/" + v.Address + "/sessions" + tail }

// others is how many sessions "end every other session" would end.
func (v sessionsView) others() int {
	n := 0
	for _, r := range v.Rows {
		if !r.Current {
			n++
		}
	}
	return n
}

// hasCurrent is whether the list holds the session making the request.
func (v sessionsView) hasCurrent() bool {
	for _, r := range v.Rows {
		if r.Current {
			return true
		}
	}
	return false
}

func currentToken(r *http.Request) string {
	if c, err := r.Cookie(auth.SessionCookie); err == nil {
		return c.Value
	}
	return ""
}

func (h *handler) selfSubject(w http.ResponseWriter, r *http.Request) (subject, bool) {
	s, ok := subjectFrom(r.Context())
	if !ok || !s.Self() {
		layout.WriteError(w, r, fmt.Errorf("sessions without the profile's person: %w", layout.ErrNotFound))
		return subject{}, false
	}
	return s, true
}

func (h *handler) loadSessions(r *http.Request, s subject) (sessionsView, error) {
	v := sessionsView{Address: s.Profile.Address}
	if h.Sessions == nil {
		return v, nil
	}
	v.Kept = true
	list, err := h.Sessions.ListForEmail(r.Context(), s.Profile.Email, currentToken(r))
	if err != nil {
		return v, fmt.Errorf("list sessions: %w", err)
	}
	for _, i := range list {
		v.Rows = append(v.Rows, sessionRow{
			ID: i.ID, Device: session.ParseUserAgent(i.Client.UserAgent).String(), IP: i.Client.IP,
			Started: i.CreatedAt, LastUsed: i.LastUsedAt, Current: i.Current,
		})
	}
	return v, nil
}

// sessionList serves the list: the fragment the page's slot loads, or, for a
// browser that opened the address itself, the whole page around it.
func (h *handler) sessionList(w http.ResponseWriter, r *http.Request) {
	s, ok := h.selfSubject(w, r)
	if !ok {
		return
	}
	v, err := h.loadSessions(r, s)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	w.Header().Add("Vary", "HX-Request")
	if r.Header.Get("HX-Request") == "true" {
		layout.Render(w, r, http.StatusOK, sessionList(v))
		return
	}
	layout.Render(w, r, http.StatusOK, sessionsPage(newView(s), v))
}

func (h *handler) sessionEnd(w http.ResponseWriter, r *http.Request) {
	s, ok := h.selfSubject(w, r)
	if !ok {
		return
	}
	if h.Sessions == nil {
		layout.WriteError(w, r, fmt.Errorf("end a session without sessions: %w", layout.ErrNotFound))
		return
	}
	// The email is the profile's person, which selfOnly has shown is the
	// viewer; the request names only which of their sessions to end. One that
	// is not theirs, or is already gone, ends nothing and is not an error:
	// the page they return to shows what is left.
	if _, err := h.Sessions.End(r.Context(), s.Profile.Email, r.PostFormValue("id")); err != nil {
		layout.WriteError(w, r, err)
		return
	}
	redirectTo(w, r, s.Profile.Address)
}

func (h *handler) sessionEndOthers(w http.ResponseWriter, r *http.Request) {
	s, ok := h.selfSubject(w, r)
	if !ok {
		return
	}
	if h.Sessions == nil {
		layout.WriteError(w, r, fmt.Errorf("end sessions without sessions: %w", layout.ErrNotFound))
		return
	}
	// The cookie's token must be one of this person's live sessions, or "every
	// other" would be every one.
	v, err := h.loadSessions(r, s)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	if !v.hasCurrent() {
		layout.Render(w, r, http.StatusBadRequest, layout.ErrorPage(http.StatusBadRequest, errNoCurrent.Error()+": sign in again"))
		return
	}
	if _, err := h.Sessions.EndOthers(r.Context(), s.Profile.Email, currentToken(r)); err != nil {
		layout.WriteError(w, r, err)
		return
	}
	redirectTo(w, r, s.Profile.Address)
}

func boolAttr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
