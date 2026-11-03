package profile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	domain "github.com/kaecyra/gobbler/internal/profile"
	"github.com/kaecyra/gobbler/internal/session"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// sessionSite is the profile routes behind the real session identity: no dev
// bypass, so a request is whoever its cookie's live session says, and an ended
// session is refused as it is in production.
type sessionSite struct {
	t        *testing.T
	h        http.Handler
	sessions *session.Store
}

func newSessionSite(t *testing.T) *sessionSite { return buildSessionSite(t, false) }

// buildSessionSite wires the routes over a session store. With bypass the
// identity is the dev header and the cookie plays no part in it.
func buildSessionSite(t *testing.T, bypass bool) *sessionSite {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	sessions, err := session.New(d, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cfg, opts := config.Config{}, []auth.Option{auth.WithSessions(sessions, layout.SignInPath)}
	if bypass {
		cfg = config.Config{Server: config.Server{ListenAddr: "127.0.0.1:0", DevAuthBypass: true}}
	}
	a, err := auth.New(cfg, opts...)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Use(a.Middleware(layout.IsPublic))
	r.Mount(Pattern, Routes(Deps{Profiles: domain.New(d), Households: fakeHouseholds{}, DB: d, Sessions: sessions}))
	return &sessionSite{t: t, h: r, sessions: sessions}
}

// signIn starts a session for email as the browser ua and returns its token.
func (s *sessionSite) signIn(email, ua, ip string) string {
	s.t.Helper()
	tok, err := s.sessions.Create(context.Background(), email, session.Client{UserAgent: ua, IP: ip})
	if err != nil {
		s.t.Fatal(err)
	}
	return tok
}

func (s *sessionSite) do(method, target, token string, form url.Values, hx bool) *httptest.ResponseRecorder {
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: token})
	if hx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	return rec
}

func (s *sessionSite) addressOf(token string) string {
	s.t.Helper()
	rec := s.do(http.MethodGet, "/profile", token, nil, false)
	if rec.Code != http.StatusSeeOther {
		s.t.Fatalf("GET /profile = %d", rec.Code)
	}
	return rec.Header().Get("Location")
}

const (
	firefox = "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0"
	safari  = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15"
)

func TestSessionListShowsBrowserOSIPAndTheCurrentSession(t *testing.T) {
	s := newSessionSite(t)
	cur := s.signIn("ann@example.com", firefox, "203.0.113.9")
	s.signIn("ann@example.com", safari, "198.51.100.4")
	s.signIn("bob@example.com", "Bobs/1", "192.0.2.77")
	path := s.addressOf(cur)

	page := s.do(http.MethodGet, path, cur, nil, false).Body.String()
	if !strings.Contains(page, `hx-get="`+path+`/sessions"`) {
		t.Errorf("the profile has no sessions slot:\n%s", page)
	}
	rec := s.do(http.MethodGet, path+"/sessions", cur, nil, true)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{"Firefox on Linux", "203.0.113.9", "Safari on macOS", "198.51.100.4", "(this session)", "started", "last used"} {
		if !strings.Contains(body, want) {
			t.Errorf("list lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "192.0.2.77") || strings.Contains(body, "Bobs/1") {
		t.Errorf("list shows another person's session:\n%s", body)
	}
	if n := strings.Count(body, `data-testid="session-end"`); n != 1 {
		t.Errorf("%d end buttons, want one for the other session only", n)
	}
	if !strings.Contains(body, `data-testid="sessions-end-others"`) {
		t.Error("no end-every-other button")
	}
	if strings.Contains(body, "<html") {
		t.Error("an HTMX request got a whole page")
	}
	if full := s.do(http.MethodGet, path+"/sessions", cur, nil, false); !strings.Contains(full.Body.String(), "<html") || !strings.Contains(full.Body.String(), "Firefox on Linux") {
		t.Errorf("a direct visit did not get the list on a page:\n%s", full.Body)
	}
}

var endID = regexp.MustCompile(`name="id" value="([0-9a-f]{64})"`)

// An ended session is refused on its next request.
func TestEndingASessionRefusesItOnItsNextRequest(t *testing.T) {
	s := newSessionSite(t)
	cur := s.signIn("ann@example.com", firefox, "203.0.113.9")
	laptop := s.signIn("ann@example.com", safari, "198.51.100.4")
	path := s.addressOf(cur)
	if rec := s.do(http.MethodGet, path, laptop, nil, false); rec.Code != http.StatusOK {
		t.Fatalf("the other session before it is ended = %d", rec.Code)
	}

	m := endID.FindStringSubmatch(s.do(http.MethodGet, path+"/sessions", cur, nil, true).Body.String())
	if m == nil {
		t.Fatal("no end form in the list")
	}
	rec := s.do(http.MethodPost, path+"/sessions/end", cur, url.Values{"id": {m[1]}}, false)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path {
		t.Fatalf("end = %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := s.do(http.MethodGet, path, laptop, nil, false); got.Code != http.StatusSeeOther || !strings.HasPrefix(got.Header().Get("Location"), layout.SignInPath) {
		t.Errorf("the ended session's next request = %d to %q, want sign-in", got.Code, got.Header().Get("Location"))
	}
	if got := s.do(http.MethodGet, path, cur, nil, false); got.Code != http.StatusOK {
		t.Errorf("the current session after ending the other = %d", got.Code)
	}
}

// "End every other session" keeps only the current one.
func TestEndingEveryOtherSessionKeepsOnlyTheCurrentOne(t *testing.T) {
	s := newSessionSite(t)
	cur := s.signIn("ann@example.com", firefox, "203.0.113.9")
	a := s.signIn("ann@example.com", safari, "198.51.100.4")
	b := s.signIn("ann@example.com", "Other/1", "")
	bob := s.signIn("bob@example.com", "Bobs/1", "192.0.2.77")
	path := s.addressOf(cur)

	rec := s.do(http.MethodPost, path+"/sessions/end-others", cur, url.Values{}, false)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path {
		t.Fatalf("end others = %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	for name, tok := range map[string]string{"a": a, "b": b} {
		if got := s.do(http.MethodGet, "/profile", tok, nil, false); got.Code != http.StatusSeeOther || !strings.HasPrefix(got.Header().Get("Location"), layout.SignInPath) {
			t.Errorf("session %s after end-others = %d to %q, want sign-in", name, got.Code, got.Header().Get("Location"))
		}
	}
	for name, tok := range map[string]string{"current": cur, "bob": bob} {
		if got := s.do(http.MethodGet, "/profile", tok, nil, false); !strings.HasPrefix(got.Header().Get("Location"), "/profile/") {
			t.Errorf("%s session was ended: %d to %q", name, got.Code, got.Header().Get("Location"))
		}
	}
	list := s.do(http.MethodGet, path+"/sessions", cur, nil, true).Body.String()
	if strings.Contains(list, "sessions-end-others") || strings.Contains(list, "session-end\"") {
		t.Errorf("with one session left the list still offers ending others:\n%s", list)
	}
}

// ADR-00032 obligation 2: another member's view of a profile has no sessions,
// and the session routes are 404 for them, whatever the id.
func TestAnotherPersonSeesNoSessionsAndCannotEndAny(t *testing.T) {
	s := newSessionSite(t)
	ann := s.signIn("ann@example.com", firefox, "203.0.113.9")
	annTwo := s.signIn("ann@example.com", safari, "198.51.100.4")
	bob := s.signIn("bob@example.com", "Bobs/1", "192.0.2.77")
	path := s.addressOf(ann)

	page := s.do(http.MethodGet, path, bob, nil, false)
	if page.Code != http.StatusOK {
		t.Fatalf("bob's view of ann's profile = %d", page.Code)
	}
	for _, leak := range []string{"Sessions", "sessions", "203.0.113.9", "198.51.100.4", "Firefox"} {
		if strings.Contains(page.Body.String(), leak) {
			t.Errorf("another person's view of the profile contains %q", leak)
		}
	}
	for _, c := range []struct {
		method, tail string
		form         url.Values
	}{
		{http.MethodGet, "/sessions", nil},
		{http.MethodPost, "/sessions/end", url.Values{"id": {"00"}}},
		{http.MethodPost, "/sessions/end-others", url.Values{}},
	} {
		if rec := s.do(c.method, path+c.tail, bob, c.form, false); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s as another person = %d, want 404", c.method, c.tail, rec.Code)
		}
	}
	for _, tok := range []string{ann, annTwo} {
		if got := s.do(http.MethodGet, "/profile", tok, nil, false); !strings.HasPrefix(got.Header().Get("Location"), "/profile/") {
			t.Errorf("ann's session was ended by bob: %d", got.Code)
		}
	}
}

// Without a live session of its own the request has no "current" one, as under
// the dev bypass, so ending the others is refused rather than ending all.
func TestEndOthersWithoutACurrentSessionIsRefused(t *testing.T) {
	s := buildSessionSite(t, true)
	tok := s.signIn("ann@example.com", firefox, "")
	req := func(method, target string, cookie bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, nil)
		r.Header.Set(auth.DevIdentityHeader, "ann@example.com")
		if cookie {
			r.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: tok})
		}
		rec := httptest.NewRecorder()
		s.h.ServeHTTP(rec, r)
		return rec
	}
	path := req(http.MethodGet, "/profile", false).Header().Get("Location")
	s.signIn("ann@example.com", safari, "")
	if rec := req(http.MethodPost, path+"/sessions/end-others", false); rec.Code != http.StatusBadRequest {
		t.Fatalf("end others without a current session = %d, want 400", rec.Code)
	}
	if list, _ := s.sessions.ListForEmail(context.Background(), "ann@example.com", ""); len(list) != 2 {
		t.Errorf("%d sessions left, want both", len(list))
	}
	if rec := req(http.MethodPost, path+"/sessions/end-others", true); rec.Code != http.StatusSeeOther {
		t.Fatalf("end others with a current session = %d", rec.Code)
	}
	if list, _ := s.sessions.ListForEmail(context.Background(), "ann@example.com", tok); len(list) != 1 || !list[0].Current {
		t.Errorf("sessions left: %+v", list)
	}
}

// With sign-in sessions not in use (the dev bypass without sign-in) the list
// says so and the routes that end sessions find nothing.
func TestSessionsSectionWithoutSessionStore(t *testing.T) {
	s := newSite(t, fakeHouseholds{})
	path := s.do(http.MethodGet, "/profile", "ann@example.com", nil).Header().Get("Location")
	req := httptest.NewRequest(http.MethodGet, path+"/sessions", nil)
	req.Header.Set(auth.DevIdentityHeader, "ann@example.com")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "not in use") {
		t.Errorf("list = %d:\n%s", rec.Code, rec.Body)
	}
	for _, tail := range []string{"/sessions/end", "/sessions/end-others"} {
		if rec := s.do(http.MethodPost, path+tail, "ann@example.com", url.Values{"id": {"00"}}); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", tail, rec.Code)
		}
	}
}
