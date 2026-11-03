package web

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/session"
	"github.com/kaecyra/gobbler/internal/web/canada"
	"github.com/kaecyra/gobbler/internal/web/layout"
	"github.com/kaecyra/gobbler/internal/web/layout/layouttest"
	"github.com/kaecyra/gobbler/internal/web/signin"
)

func devConfig() config.Config {
	return config.Config{Server: config.Server{ListenAddr: "127.0.0.1:0", DevAuthBypass: true}}
}

// prodConfig is a production deployment: no bypass, so identity comes only
// from the sign-in session (ADR-00030). The issuer is never contacted by the
// tests that use it: none of them starts a sign-in.
func prodConfig() config.Config {
	return config.Config{
		Server: config.Server{ListenAddr: "0.0.0.0:8080"},
		OIDC:   config.OIDC{Issuer: "https://issuer.test", ClientID: "c", PublicBaseURL: "https://gobbler.test"},
	}
}

// testRouter is the real router for cfg. When cfg configures sign-in it is
// wired as serve wires it, with the session store and sign-in.
func testRouter(t *testing.T, cfg config.Config, d Deps) *chi.Mux {
	t.Helper()
	quiet := slog.New(slog.NewJSONHandler(io.Discard, nil))
	if d.DB == nil {
		d.DB = testDB(t)
	}
	var opts []auth.Option
	if cfg.OIDC.Configured() {
		var err error
		if d.Sessions, err = session.New(d.DB, time.Hour); err != nil {
			t.Fatal(err)
		}
		if d.SignIn, err = signin.New(signin.Settings{
			Issuer: cfg.OIDC.Issuer, ClientID: cfg.OIDC.ClientID, PublicBaseURL: cfg.OIDC.PublicBaseURL, Lifetime: time.Hour,
		}, d.Sessions); err != nil {
			t.Fatal(err)
		}
		opts = append(opts, auth.WithSessions(d.Sessions, layout.SignInPath))
	}
	a, err := auth.New(cfg, append(opts, auth.WithLogger(quiet))...)
	if err != nil {
		t.Fatal(err)
	}
	d.Config, d.Auth = cfg, a
	if d.Logger == nil {
		d.Logger = quiet
	}
	r, err := newRouter(d)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// testDB is a real SQLite file migrated as the binary does.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return d
}

func get(h http.Handler, path string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// refused reports whether rec is the refusal of a request without an identity
// (ADR-00030): a full-page GET is sent to sign-in, anything else is 401.
func refused(rec *httptest.ResponseRecorder) bool {
	if rec.Code == http.StatusSeeOther {
		return strings.HasPrefix(rec.Header().Get("Location"), layout.SignInPath+"?next=")
	}
	return rec.Code == http.StatusUnauthorized
}

// badSession is a session cookie that names no session.
const badSession = "Cookie"

func TestNewRequiresAuthenticator(t *testing.T) {
	if _, err := New(Deps{}); err == nil {
		t.Fatal("want error without an authenticator")
	}
}

func TestHealthOpenWithoutTokenAndReportsDetails(t *testing.T) {
	r := testRouter(t, prodConfig(), Deps{
		HealthDetails: func(context.Context) map[string]string { return map[string]string{"schema": "1"} },
	})
	rec := get(r, HealthPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"status":"ok"`) || !strings.Contains(body, `"schema":"1"`) {
		t.Errorf("health body = %s", body)
	}
}

func TestHealthBodyOmitsDetailsByDefault(t *testing.T) {
	body := get(testRouter(t, prodConfig(), Deps{}), HealthPath).Body.String()
	if strings.Contains(body, "details") {
		t.Errorf("health body = %s, want no details key", body)
	}
}

// An unregistered path is refused like a private one, so route existence is
// not disclosed, and an unknown session is no better than none. The matrix of
// registered routes is TestEveryRouteButThePublicPathsRequiresASession.
func TestUnknownPathAndSessionAreRefused(t *testing.T) {
	r := testRouter(t, prodConfig(), Deps{})
	if rec := get(r, "/no/such/route"); !refused(rec) {
		t.Errorf("unregistered path = %d, want a sign-in redirect", rec.Code)
	}
	if rec := get(r, "/", badSession, auth.SessionCookie+"=not-a-session"); !refused(rec) {
		t.Errorf("unknown session = %d, want a sign-in redirect", rec.Code)
	}
}

// ADR-00002 obligation 3: no page names a script, stylesheet, font or other
// asset on another origin. A plain link to another site is navigation, not a
// load, and is allowed.
func TestPagesReferenceNoThirdPartyOrigin(t *testing.T) {
	r := testRouter(t, devConfig(), Deps{})
	for _, path := range []string{"/", "/nope"} {
		layouttest.AssertNoRemoteAssets(t, path, get(r, path).Body.String())
	}
	layouttest.AssertNoRemoteCSS(t, "app.css", get(r, "/static/app.css").Body.String())
}

func TestRequestLogOmitsCredentials(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	r := testRouter(t, devConfig(), Deps{Logger: log})
	get(r, "/?token=querysecret", "Authorization", "bearersecret", "Cookie", auth.SessionCookie+"=cookiesecret")
	out := buf.String()
	if !strings.Contains(out, `"path":"/"`) || !strings.Contains(out, `"status":200`) {
		t.Fatalf("request not logged as expected: %s", out)
	}
	for _, secret := range []string{"bearersecret", "cookiesecret", "querysecret"} {
		if strings.Contains(out, secret) {
			t.Errorf("log leaks %q: %s", secret, out)
		}
	}
}

func TestRefusedRequestsAreLogged(t *testing.T) {
	var buf bytes.Buffer
	r := testRouter(t, prodConfig(), Deps{Logger: slog.New(slog.NewJSONHandler(&buf, nil))})
	get(r, "/", "HX-Request", "true")
	if !strings.Contains(buf.String(), `"status":401`) {
		t.Errorf("401 not in request log: %s", buf.String())
	}
}

func TestPanicBecomes500AndIsLogged(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	h := recoverer(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := get(h, "/")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("panic not logged: %s", buf.String())
	}
}

func TestAbortHandlerPanicPropagates(t *testing.T) {
	h := recoverer(slog.New(slog.NewJSONHandler(io.Discard, nil)))(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	defer func() {
		if v := recover(); v != http.ErrAbortHandler { //nolint:errorlint // sentinel panic value
			t.Errorf("recovered %v, want http.ErrAbortHandler", v)
		}
	}()
	get(h, "/")
}

func TestPanickingRequestIsAccessLoggedAs500(t *testing.T) {
	var buf bytes.Buffer
	r := testRouter(t, devConfig(), Deps{Logger: slog.New(slog.NewJSONHandler(&buf, nil))})
	r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	if rec := get(r, "/boom"); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.Contains(line, `"msg":"request"`) && strings.Contains(line, `"path":"/boom"`) && strings.Contains(line, `"status":500`) {
			found = true
		}
	}
	if !found {
		t.Errorf("no request log line with status 500 for the panic:\n%s", buf.String())
	}
}

func TestStaticDirectoriesAreNotListed(t *testing.T) {
	r := testRouter(t, devConfig(), Deps{})
	for _, path := range []string{"/static/", "/static/.", "/static/././", "/static/.//./"} {
		rec := get(r, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "htmx.VERSION.txt") {
			t.Errorf("%s lists directory contents", path)
		}
	}
}

func TestHealthAndCanadaArePublic(t *testing.T) {
	for _, p := range []string{HealthPath, canada.Pattern} {
		if !layout.IsPublic(p) {
			t.Errorf("%s is not on the public list", p)
		}
	}
}

// Without a session, only the public paths are served, checked on both forms
// of the path, and an unknown session is no better than none off them
// (ADR-00029).
func TestNoSessionReachesOnlyThePublicPaths(t *testing.T) {
	r := testRouter(t, prodConfig(), Deps{})
	cases := []struct {
		path string
		want int
	}{
		{"/canada", http.StatusOK},
		{"/static/app.css", http.StatusOK},
		{HealthPath, http.StatusOK},
		// The identity middleware sees the decoded /canada and lets it by
		// without a session; the membership middleware judges the raw path
		// and refuses it.
		{"/c%61nada", http.StatusForbidden},
		{"/canada/", http.StatusSeeOther},
		{"/recipes", http.StatusSeeOther},
		{"/settings", http.StatusSeeOther},
		{layout.InvitationsPath + "/1", http.StatusSeeOther},
		{layout.NotAMemberPath, http.StatusSeeOther},
	}
	for _, c := range cases {
		for name, rec := range map[string]*httptest.ResponseRecorder{
			"no session":      get(r, c.path),
			"unknown session": get(r, c.path, badSession, auth.SessionCookie+"=not-a-session"),
		} {
			if rec.Code != c.want || (c.want == http.StatusSeeOther && !refused(rec)) {
				t.Errorf("GET %s with %s = %d to %q, want %d", c.path, name, rec.Code, rec.Header().Get("Location"), c.want)
			}
		}
	}
	body := get(r, "/canada").Body.String()
	if !strings.Contains(body, "Made in Canada") || strings.Contains(body, `href="/recipes"`) {
		t.Errorf("anonymous /canada should render the page with an empty nav:\n%s", body)
	}
}
