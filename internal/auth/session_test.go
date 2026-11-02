package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/session"
)

// fakeSessions maps tokens to emails; any other token is no session.
type fakeSessions struct {
	emails map[string]string
	err    error
}

func (f fakeSessions) Lookup(_ context.Context, token string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if e, ok := f.emails[token]; ok {
		return e, nil
	}
	return "", fmt.Errorf("look up session: %w", session.ErrNoSession)
}

func signInConfig() config.Config {
	return config.Config{
		Server: config.Server{ListenAddr: "0.0.0.0:8080"},
		OIDC:   config.OIDC{Issuer: "https://issuer.test", ClientID: "c", PublicBaseURL: "https://gobbler.test"},
	}
}

func sessionAuth(t *testing.T, cfg config.Config, s SessionLookup) (*Authenticator, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	a, err := New(cfg, WithSessions(s, "/login"), WithLogger(slog.New(slog.NewJSONHandler(logs, nil))))
	if err != nil {
		t.Fatal(err)
	}
	return a, logs
}

// sessionServe serves r through the middleware with /canada public and returns
// the response and the identity the handler saw.
func sessionServe(a *Authenticator, r *http.Request) (*httptest.ResponseRecorder, string) {
	var seen string
	h := a.Middleware(func(p string) bool { return p == "/canada" })(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen, _ = EmailFromContext(r.Context())
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec, seen
}

func withSession(r *http.Request, token string) *http.Request {
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: token})
	return r
}

func TestSignInRequiresASessionStore(t *testing.T) {
	if _, err := New(signInConfig()); err == nil {
		t.Fatal("sign-in configured without a session store: want an error")
	}
}

func TestSessionIdentity(t *testing.T) {
	a, _ := sessionAuth(t, signInConfig(), fakeSessions{emails: map[string]string{"good": "tim@example.com"}})
	for _, path := range []string{"/recipes", "/canada"} {
		rec, email := sessionServe(a, withSession(httptest.NewRequest(http.MethodGet, path, nil), "good"))
		if rec.Code != http.StatusOK || email != "tim@example.com" {
			t.Errorf("%s: %d as %q", path, rec.Code, email)
		}
	}
}

// ADR-00030: a private request without a live session is sent to sign-in when
// it is a full-page GET and refused with 401 otherwise; a public one is served
// without an identity.
func TestNoSessionRedirectsOrRefuses(t *testing.T) {
	a, logs := sessionAuth(t, signInConfig(), fakeSessions{emails: map[string]string{"good": "tim@example.com"}})
	for _, token := range []string{"", "unknown"} {
		req := func(method, target string) *http.Request {
			r := httptest.NewRequest(method, target, nil)
			if token != "" {
				r = withSession(r, token)
			}
			return r
		}
		rec, _ := sessionServe(a, req(http.MethodGet, "/recipes/7?scale=2&x=%2F"))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login?next=%2Frecipes%2F7%3Fscale%3D2%26x%3D%252F" {
			t.Errorf("token %q: full-page GET = %d to %q", token, rec.Code, rec.Header().Get("Location"))
		}
		htmx := req(http.MethodGet, "/recipes")
		htmx.Header.Set("HX-Request", "true")
		for name, r := range map[string]*http.Request{"htmx GET": htmx, "POST": req(http.MethodPost, "/recipes"), "HEAD": req(http.MethodHead, "/recipes")} {
			if rec, _ := sessionServe(a, r); rec.Code != http.StatusUnauthorized {
				t.Errorf("token %q: %s = %d, want 401", token, name, rec.Code)
			}
		}
		if rec, email := sessionServe(a, req(http.MethodGet, "/canada")); rec.Code != http.StatusOK || email != "" {
			t.Errorf("token %q: public page = %d as %q", token, rec.Code, email)
		}
	}
	if !strings.Contains(logs.String(), "public request served without identity") {
		t.Errorf("an unknown session on a public page was not logged: %s", logs)
	}
	if strings.Contains(logs.String(), "unknown") {
		t.Errorf("log contains the session token: %s", logs)
	}
}

// A failure to look the session up is not "signed out": a private request
// gets 500 rather than a sign-in loop, and a public one is still served.
func TestSessionLookupFailure(t *testing.T) {
	a, logs := sessionAuth(t, signInConfig(), fakeSessions{err: errors.New("database is locked")})
	if rec, _ := sessionServe(a, withSession(httptest.NewRequest(http.MethodGet, "/recipes", nil), "SECRETTOKEN")); rec.Code != http.StatusInternalServerError {
		t.Errorf("private: %d, want 500", rec.Code)
	}
	if rec, email := sessionServe(a, withSession(httptest.NewRequest(http.MethodGet, "/canada", nil), "SECRETTOKEN")); rec.Code != http.StatusOK || email != "" {
		t.Errorf("public: %d as %q", rec.Code, email)
	}
	if !strings.Contains(logs.String(), "session lookup failed") || strings.Contains(logs.String(), "SECRETTOKEN") {
		t.Errorf("logs: %s", logs)
	}
}

// With sign-in configured the Access token is not an identity.
func TestSessionModeIgnoresAccessToken(t *testing.T) {
	a, _ := sessionAuth(t, signInConfig(), fakeSessions{})
	r := httptest.NewRequest(http.MethodPost, "/recipes", nil)
	r.Header.Set(HeaderName, goodToken(t, nil))
	if rec, email := sessionServe(a, r); rec.Code != http.StatusUnauthorized || email != "" {
		t.Errorf("%d as %q", rec.Code, email)
	}
}

// The dev bypass stays as it is (ADR-00030): it wins over a configured
// sign-in, and a session is never consulted on a bypass listener.
func TestBypassWinsOverSessions(t *testing.T) {
	cfg := signInConfig()
	cfg.Server = config.Server{ListenAddr: "127.0.0.1:0", DevAuthBypass: true}
	a, _ := sessionAuth(t, cfg, fakeSessions{err: errors.New("must not be called")})
	if rec, email := sessionServe(a, httptest.NewRequest(http.MethodGet, "/recipes", nil)); rec.Code != http.StatusOK || email != DevEmail {
		t.Errorf("%d as %q", rec.Code, email)
	}
}

// Without sign-in, Access is the identity and a refusal stays 403, whatever
// session store is given.
func TestAccessModeIgnoresSessions(t *testing.T) {
	a, err := New(prodConfig(), WithSessions(fakeSessions{emails: map[string]string{"good": "tim@example.com"}}, "/login"),
		WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	if rec, email := sessionServe(a, withSession(httptest.NewRequest(http.MethodGet, "/recipes", nil), "good")); rec.Code != http.StatusForbidden || email != "" {
		t.Errorf("%d as %q", rec.Code, email)
	}
}
