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

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/web/layout/layouttest"
)

func devConfig() config.Config {
	return config.Config{Server: config.Server{ListenAddr: "127.0.0.1:0", DevAuthBypass: true}}
}

func prodConfig() config.Config {
	return config.Config{
		Server: config.Server{ListenAddr: "0.0.0.0:8080"},
		Access: config.Access{TeamDomain: "team.cloudflareaccess.com", AUD: "aud-tag"},
	}
}

func testRouter(t *testing.T, cfg config.Config, d Deps) *chi.Mux {
	t.Helper()
	quiet := slog.New(slog.NewJSONHandler(io.Discard, nil))
	a, err := auth.New(cfg, auth.WithLogger(quiet))
	if err != nil {
		t.Fatal(err)
	}
	d.Config, d.Auth = cfg, a
	if d.DB == nil {
		d.DB = testDB(t)
	}
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

// Every registered route except health refuses a request with no token; an
// unregistered path is refused too, so route existence is not disclosed.
func TestEveryRouteButHealthRequiresAToken(t *testing.T) {
	r := testRouter(t, prodConfig(), Deps{})
	var checked int
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		path := strings.ReplaceAll(route, "*", "x")
		req := httptest.NewRequest(method, path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		want := http.StatusForbidden
		if route == HealthPath {
			want = http.StatusOK
		}
		if rec.Code != want {
			t.Errorf("%s %s = %d, want %d", method, route, rec.Code, want)
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 3 {
		t.Fatalf("walked only %d routes; the matrix is not exercising the router", checked)
	}
	if rec := get(r, "/no/such/route"); rec.Code != http.StatusForbidden {
		t.Errorf("unregistered path = %d, want 403", rec.Code)
	}
	if rec := get(r, "/", auth.HeaderName, "not-a-jwt"); rec.Code != http.StatusForbidden {
		t.Errorf("garbage token = %d, want 403", rec.Code)
	}
}

func TestAuthenticatedPagesRenderLayout(t *testing.T) {
	r := testRouter(t, devConfig(), Deps{})
	rec := get(r, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("/ = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `<main id="content">`) {
		t.Errorf("home page does not use the layout:\n%s", rec.Body.String())
	}
}

func TestUnknownPathRendersNotFoundPage(t *testing.T) {
	rec := get(testRouter(t, devConfig(), Deps{}), "/nope")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "404") {
		t.Errorf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestStaticAssetsServedFromEmbeddedFS(t *testing.T) {
	r := testRouter(t, devConfig(), Deps{})
	for _, tc := range []struct{ path, wantPrefix, wantInBody string }{
		{"/static/app.css", "text/css", "--accent"},
		{"/static/htmx.min.js", "text/javascript", "htmx"},
	} {
		rec := get(r, tc.path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", tc.path, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, tc.wantPrefix) {
			t.Errorf("%s content type = %q, want prefix %q", tc.path, ct, tc.wantPrefix)
		}
		if !strings.Contains(rec.Body.String(), tc.wantInBody) {
			t.Errorf("%s body lacks %q", tc.path, tc.wantInBody)
		}
	}
	if rec := get(r, "/static/missing.js"); rec.Code != http.StatusNotFound {
		t.Errorf("missing asset = %d, want 404", rec.Code)
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
	get(r, "/?token=querysecret", auth.HeaderName, "jwtsecret", "Authorization", "bearersecret", "Cookie", auth.CookieName+"=cookiesecret")
	out := buf.String()
	if !strings.Contains(out, `"path":"/"`) || !strings.Contains(out, `"status":200`) {
		t.Fatalf("request not logged as expected: %s", out)
	}
	for _, secret := range []string{"jwtsecret", "bearersecret", "cookiesecret", "querysecret"} {
		if strings.Contains(out, secret) {
			t.Errorf("log leaks %q: %s", secret, out)
		}
	}
}

func TestRefusedRequestsAreLogged(t *testing.T) {
	var buf bytes.Buffer
	r := testRouter(t, prodConfig(), Deps{Logger: slog.New(slog.NewJSONHandler(&buf, nil))})
	get(r, "/")
	if !strings.Contains(buf.String(), `"status":403`) {
		t.Errorf("403 not in request log: %s", buf.String())
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
