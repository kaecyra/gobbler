package profile

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	apiprofile "github.com/kaecyra/gobbler/internal/api/profile"
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

func currentToken(r *http.Request) string {
	if c, err := r.Cookie(auth.SessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// sessionError gives a refusal of the session service the web kind the status
// table maps: no sessions to end is a missing page, and no current session to
// keep is a bad request.
func sessionError(err error) error {
	switch {
	case errors.Is(err, apiprofile.ErrNoSessions):
		return fmt.Errorf("sessions are not in use: %w: %w", layout.ErrNotFound, err)
	case errors.Is(err, apiprofile.ErrNoCurrentSession):
		return fmt.Errorf("%w: %w", layout.ErrInvalid, err)
	}
	return err
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
	list, kept, err := h.Profile.ListSessions(r.Context(), callerOf(r), currentToken(r))
	if err != nil {
		return v, err
	}
	v.Kept = kept
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
	// The person is the Caller, which selfOnly has shown is the profile's; the
	// request names only which of their sessions to end. One that is not
	// theirs, or is already gone, ends nothing and is not an error: the page
	// they return to shows what is left.
	if err := h.Profile.EndSession(r.Context(), callerOf(r), r.PostFormValue("id")); err != nil {
		layout.WriteError(w, r, sessionError(err))
		return
	}
	redirectTo(w, r, s.Profile.Address)
}

func (h *handler) sessionEndOthers(w http.ResponseWriter, r *http.Request) {
	s, ok := h.selfSubject(w, r)
	if !ok {
		return
	}
	// The service checks that the cookie's token is one of this person's live
	// sessions, or "every other" would be every one.
	if err := h.Profile.EndOtherSessions(r.Context(), callerOf(r), currentToken(r)); err != nil {
		layout.WriteError(w, r, sessionError(err))
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
