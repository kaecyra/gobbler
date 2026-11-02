package web

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/session"
	"github.com/kaecyra/gobbler/internal/web/layout"
	"github.com/kaecyra/gobbler/internal/web/signin"
	"github.com/kaecyra/gobbler/internal/web/signin/signintest"
)

// signInSite is the real router with sign-in configured (ADR-00030), signing
// in against a fake issuer the test serves itself.
type signInSite struct {
	r        *chi.Mux
	db       *sql.DB
	iss      *signintest.Issuer
	sessions *session.Store
	logs     *bytes.Buffer
}

func newSignInSite(t *testing.T, bypass bool, lifetime time.Duration) *signInSite {
	t.Helper()
	s := &signInSite{db: testDB(t), iss: signintest.New(t), logs: &bytes.Buffer{}}
	cfg := config.Config{
		Server: config.Server{ListenAddr: "0.0.0.0:8080"},
		OIDC:   config.OIDC{Issuer: s.iss.URL, ClientID: signintest.ClientID, PublicBaseURL: "https://gobbler.test"},
	}
	if bypass {
		cfg.Server = config.Server{ListenAddr: "127.0.0.1:0", DevAuthBypass: true}
	}
	logger := slog.New(slog.NewJSONHandler(s.logs, nil))
	var err error
	if s.sessions, err = session.New(s.db, lifetime); err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(cfg, auth.WithSessions(s.sessions, layout.SignInPath), auth.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	si, err := signin.New(signin.Settings{
		Issuer: s.iss.URL, ClientID: signintest.ClientID, ClientSecret: signintest.ClientSecret,
		PublicBaseURL: "https://gobbler.test", Lifetime: lifetime,
	}, s.sessions, signin.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	if s.r, err = newRouter(Deps{Config: cfg, DB: s.db, Auth: a, SignIn: si, Sessions: s.sessions, Logger: logger}); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *signInSite) do(method, target string, cookie *http.Cookie, hdr ...string) *httptest.ResponseRecorder {
	var body io.Reader
	if method == http.MethodPost && strings.Contains(target, "?") {
		target, body = splitForm(target)
	}
	r := httptest.NewRequest(method, target, body)
	if body != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		r.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	s.r.ServeHTTP(rec, r)
	return rec
}

// splitForm turns "/path?a=b" into the path and a form body.
func splitForm(target string) (string, io.Reader) {
	path, form, _ := strings.Cut(target, "?")
	return path, strings.NewReader(form)
}

// signIn follows a browser through sign-in as email, starting at the page it
// asked for, and returns where it lands and the session cookie.
func (s *signInSite) signIn(t *testing.T, email, page string) (string, *http.Cookie) {
	t.Helper()
	s.iss.Set(func(i *signintest.Issuer) { i.Email = email })
	rec := s.do(http.MethodGet, page, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("%s without a session = %d, want a redirect to sign-in", page, rec.Code)
	}
	rec = s.do(http.MethodGet, rec.Header().Get("Location"), nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sign-in = %d, body %s", rec.Code, rec.Body)
	}
	pending := cookieOf(rec, signin.PendingCookie)
	back := s.iss.Authorize(t, rec.Header().Get("Location"))
	rec = s.do(http.MethodGet, back.RequestURI(), pending)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("callback = %d, body %s", rec.Code, rec.Body)
	}
	c := cookieOf(rec, auth.SessionCookie)
	if c == nil {
		t.Fatal("callback set no session cookie")
	}
	return rec.Header().Get("Location"), c
}

func cookieOf(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func seedMember(t *testing.T, d *sql.DB, name, email string) {
	t.Helper()
	if err := household.New(d).Seed(context.Background(), name, email); err != nil {
		t.Fatal(err)
	}
}

// The whole flow through the real router: a private page sends the browser to
// sign-in, the callback starts a session, the page is served with the
// identity, and signing out ends the session so its cookie is refused.
func TestSignInThroughTheRouter(t *testing.T) {
	s := newSignInSite(t, false, time.Hour)
	seedMember(t, s.db, "Alpha", "ann@example.com")

	landed, c := s.signIn(t, "Ann@Example.com", "/household?tab=members")
	if landed != "/household?tab=members" {
		t.Fatalf("landed on %q, want the page asked for", landed)
	}
	rec := s.do(http.MethodGet, "/household", c)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "ann@example.com") {
		t.Fatalf("members page = %d, body %s", rec.Code, body)
	}
	if !strings.Contains(body, `action="/logout"`) || strings.Contains(body, `href="/login"`) {
		t.Error("a signed-in page must offer sign-out and not sign-in")
	}

	rec = s.do(http.MethodPost, layout.SignOutPath, c)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("sign out = %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := s.do(http.MethodGet, "/household", c); rec.Code != http.StatusSeeOther {
		t.Errorf("the signed-out cookie = %d, want a redirect to sign-in", rec.Code)
	}
	if rec := s.do(http.MethodPost, "/household/leave", c); rec.Code != http.StatusUnauthorized {
		t.Errorf("a POST with the signed-out cookie = %d, want 401", rec.Code)
	}
}

// An expired session is refused like no session (ADR-00030 obligation 2).
func TestExpiredSessionRefusedByTheRouter(t *testing.T) {
	s := newSignInSite(t, false, time.Millisecond)
	seedMember(t, s.db, "Alpha", "ann@example.com")
	_, c := s.signIn(t, "ann@example.com", "/recipes")
	time.Sleep(5 * time.Millisecond)
	if rec := s.do(http.MethodGet, "/recipes", c); rec.Code != http.StatusSeeOther ||
		!strings.HasPrefix(rec.Header().Get("Location"), layout.SignInPath+"?next=") {
		t.Errorf("expired session = %d to %q, want a redirect to sign-in", rec.Code, rec.Header().Get("Location"))
	}
}

// The return target survives sign-in only when it is a path on this site.
func TestSignInReturnTargetThroughTheRouter(t *testing.T) {
	s := newSignInSite(t, false, time.Hour)
	for next, want := range map[string]string{
		"/recipes?text=pie":        "/recipes?text=pie",
		"https://evil.example/":    "/",
		"//evil.example/":          "/",
		"/\\evil.example":          "/",
		"/login?next=//evil":       "/",
		"javascript:alert(1)":      "/",
		"https:%2F%2Fevil.example": "/",
	} {
		rec := s.do(http.MethodGet, layout.SignInPath+"?next="+url.QueryEscape(next), nil)
		back := s.iss.Authorize(t, rec.Header().Get("Location"))
		rec = s.do(http.MethodGet, back.RequestURI(), cookieOf(rec, signin.PendingCookie))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
			t.Errorf("next %q: %d to %q, want %q", next, rec.Code, rec.Header().Get("Location"), want)
		}
	}
}

// With sign-in, every route but the public paths needs a session: a full-page
// GET is sent to sign-in and returns to the page it asked for, and anything
// else, an HTMX request included, is 401. A public path is served without one.
func TestEveryRouteButThePublicPathsRequiresASession(t *testing.T) {
	s := newSignInSite(t, false, time.Hour)
	var checked int
	err := chi.Walk(s.r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		path := strings.NewReplacer("*", "x", "{", "", "}", "").Replace(route)
		rec := s.do(method, path, nil)
		switch {
		case layout.IsPublic(path):
			if rec.Code == http.StatusUnauthorized || (rec.Code == http.StatusSeeOther && strings.HasPrefix(rec.Header().Get("Location"), layout.SignInPath+"?")) {
				t.Errorf("%s %s = %d, want it served without a session", method, route, rec.Code)
			}
		case method == http.MethodGet:
			if want := layout.SignInPath + "?next=" + url.QueryEscape(path); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
				t.Errorf("GET %s = %d to %q, want 303 to %q", route, rec.Code, rec.Header().Get("Location"), want)
			}
			if rec := s.do(method, path, nil, "HX-Request", "true"); rec.Code != http.StatusUnauthorized {
				t.Errorf("HTMX GET %s = %d, want 401", route, rec.Code)
			}
		default:
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s %s = %d, want 401", method, route, rec.Code)
			}
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 10 {
		t.Fatalf("walked only %d routes", checked)
	}
}

// An anonymous visitor on a public page is offered sign-in, and the nav stays
// empty (ADR-00029).
func TestAnonymousPublicPageOffersSignIn(t *testing.T) {
	s := newSignInSite(t, false, time.Hour)
	rec := s.do(http.MethodGet, layout.CanadaPath, nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `href="/login"`) || strings.Contains(body, `action="/logout"`) {
		t.Fatalf("/canada = %d, body %s", rec.Code, body)
	}
	nav := body[strings.Index(body, `aria-label="Primary"`):strings.Index(body, "</nav>")]
	if strings.Contains(nav, "<a ") {
		t.Errorf("the anonymous nav has links: %s", nav)
	}
}

// Removing a member, or leaving, ends that person's sessions at once
// (ADR-00030), through the real members page.
func TestRemovingAMemberEndsTheirSessions(t *testing.T) {
	s := newSignInSite(t, false, time.Hour)
	seedMember(t, s.db, "Alpha", "ann@example.com")
	hs := household.New(s.db)
	ctx := context.Background()
	for _, e := range []string{"cy@example.com", "di@example.com"} {
		code, err := hs.Invite(ctx, household.Invite{Kind: household.KindJoin, Email: e, HouseholdID: mustHousehold(t, hs, "ann@example.com"), IssuedBy: "ann@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := hs.Accept(ctx, code, e, ""); err != nil {
			t.Fatal(err)
		}
	}
	_, ann := s.signIn(t, "ann@example.com", "/")
	_, cy := s.signIn(t, "cy@example.com", "/")
	_, cyPhone := s.signIn(t, "cy@example.com", "/")
	_, di := s.signIn(t, "di@example.com", "/")

	if rec := s.do(http.MethodPost, "/household/members/remove?email=cy%40example.com", ann); rec.Code != http.StatusSeeOther {
		t.Fatalf("remove = %d, body %s", rec.Code, rec.Body)
	}
	for _, c := range []*http.Cookie{cy, cyPhone} {
		if rec := s.do(http.MethodGet, "/", c); rec.Code != http.StatusSeeOther {
			t.Errorf("a removed member's session = %d, want it ended", rec.Code)
		}
	}
	if rec := s.do(http.MethodPost, "/household/leave", di); rec.Code != http.StatusSeeOther {
		t.Fatalf("leave = %d, body %s", rec.Code, rec.Body)
	}
	if rec := s.do(http.MethodGet, "/", di); rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), layout.SignInPath) {
		t.Errorf("the leaver's session = %d, want it ended", rec.Code)
	}
	if rec := s.do(http.MethodGet, "/", ann); rec.Code != http.StatusOK {
		t.Errorf("the remaining member's session = %d, want it untouched", rec.Code)
	}
}

func mustHousehold(t *testing.T, hs *household.Store, email string) int64 {
	t.Helper()
	h, err := hs.ForEmail(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	return h.ID
}

// A signed-in non-member can sign out: sign-out holds no household data.
func TestNonMemberCanSignOut(t *testing.T) {
	s := newSignInSite(t, false, time.Hour)
	_, c := s.signIn(t, "stranger@example.com", "/")
	rec := s.do(http.MethodGet, "/", c)
	if !strings.Contains(rec.Body.String(), `action="/logout"`) {
		t.Errorf("the non-member's page offers no sign-out: %s", rec.Body)
	}
	if rec := s.do(http.MethodPost, layout.SignOutPath, c); rec.Code != http.StatusSeeOther {
		t.Errorf("non-member sign out = %d, want 303", rec.Code)
	}
}

// Sign-out is a state change, so a cross-site POST is refused (ADR-00016).
func TestCrossSiteSignOutRefused(t *testing.T) {
	s := newSignInSite(t, false, time.Hour)
	_, c := s.signIn(t, "stranger@example.com", "/")
	if rec := s.do(http.MethodPost, layout.SignOutPath, c, "Sec-Fetch-Site", "cross-site"); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site sign out = %d, want 403", rec.Code)
	}
	if _, err := s.sessions.Lookup(context.Background(), c.Value); err != nil {
		t.Errorf("a refused sign-out ended the session: %v", err)
	}
}

// The dev bypass stays as it is: with sign-in configured too, every request
// is still the dev identity, and the sign-in routes are mounted.
func TestBypassWithSignInKeepsTheDevIdentity(t *testing.T) {
	s := newSignInSite(t, true, time.Hour)
	if rec := s.do(http.MethodGet, "/household", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), auth.DevEmail) {
		t.Errorf("bypass /household = %d", rec.Code)
	}
	if rec := s.do(http.MethodGet, layout.SignInPath, nil); rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), s.iss.URL) {
		t.Errorf("bypass /login = %d to %q", rec.Code, rec.Header().Get("Location"))
	}
}

// ADR-00030 obligation 4, through the router: the request log, the identity
// middleware and sign-in together log no code, ID token or session token.
func TestRouterLogsNoSignInSecrets(t *testing.T) {
	s := newSignInSite(t, false, time.Hour)
	seedMember(t, s.db, "Alpha", "ann@example.com")
	_, c := s.signIn(t, "ann@example.com", "/recipes")
	s.do(http.MethodGet, "/recipes", c)
	s.do(http.MethodPost, layout.SignOutPath, c)
	s.do(http.MethodGet, "/recipes", c)
	s.do(http.MethodGet, layout.CanadaPath, c)
	out := s.logs.String()
	if !strings.Contains(out, `"path":"/auth/callback"`) {
		t.Fatalf("the callback was not logged: %s", out)
	}
	for _, secret := range append(s.iss.Secrets(), c.Value) {
		for _, part := range strings.Split(secret, ".") {
			if len(part) > 8 && strings.Contains(out, part) {
				t.Fatalf("log contains secret material %q", part)
			}
		}
	}
}

func TestNewRequiresSignInExactlyWhenConfigured(t *testing.T) {
	d := testDB(t)
	a, err := auth.New(devConfig())
	if err != nil {
		t.Fatal(err)
	}
	cfg := devConfig()
	cfg.OIDC = config.OIDC{Issuer: "https://issuer.test", ClientID: "c", PublicBaseURL: "https://gobbler.test"}
	if _, err := newRouter(Deps{Config: cfg, DB: d, Auth: a}); err == nil {
		t.Error("sign-in configured without its routes: want an error")
	}
	sessions, err := session.New(d, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	si, err := signin.New(signin.Settings{Issuer: "https://issuer.test", ClientID: "c", PublicBaseURL: "https://gobbler.test", Lifetime: time.Hour}, sessions)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newRouter(Deps{Config: devConfig(), DB: d, Auth: a, SignIn: si}); err == nil {
		t.Error("sign-in routes without sign-in configured: want an error")
	}
}
