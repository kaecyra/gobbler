package auth

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
)

// serve runs one request through a's middleware, with the given paths public,
// wrapping a handler that records the context email, and returns the status
// and that email.
func serve(a *Authenticator, req *http.Request, public ...string) (int, string) {
	var got string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = EmailFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	a.Middleware(func(p string) bool { return slices.Contains(public, p) })(next).ServeHTTP(rec, req)
	return rec.Code, got
}

func TestPublicPath(t *testing.T) {
	a, _ := sessionAuth(t, signInConfig(), fakeSessions{emails: map[string]string{"good": "tim@example.com"}})
	cases := []struct {
		name, path, token string
		wantCode          int
		wantEmail         string
	}{
		{"public, no session", "/canada", "", http.StatusOK, ""},
		{"public, live session keeps the identity", "/canada", "good", http.StatusOK, "tim@example.com"},
		{"public, unknown session is served without identity", "/canada", "unknown", http.StatusOK, ""},
		{"not public, no session", "/recipes", "", http.StatusUnauthorized, ""},
		{"not public, unknown session", "/recipes", "unknown", http.StatusUnauthorized, ""},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, c.path, nil)
		if c.token != "" {
			req = withSession(req, c.token)
		}
		code, email := serve(a, req, "/canada")
		if code != c.wantCode || email != c.wantEmail {
			t.Errorf("%s: = %d %q, want %d %q", c.name, code, email, c.wantCode, c.wantEmail)
		}
	}
}

func TestNilPublicMakesNothingPublic(t *testing.T) {
	a, _ := sessionAuth(t, signInConfig(), fakeSessions{})
	rec := httptest.NewRecorder()
	a.Middleware(nil)(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no session with nil public = %d, want 401", rec.Code)
	}
}

func TestDevBypass(t *testing.T) {
	cfg := config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}}
	a, err := New(cfg, WithLogger(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))))
	if err != nil {
		t.Fatal(err)
	}
	if code, got := serve(a, httptest.NewRequest(http.MethodGet, "/", nil)); code != http.StatusOK || got != DevEmail {
		t.Fatalf("got %d %q, want 200 %q", code, got, DevEmail)
	}
}

func TestDevBypassRefusesNonLoopback(t *testing.T) {
	for _, addr := range []string{":8080", "0.0.0.0:8080", "192.168.1.5:8080", "example.com:80", "garbage"} {
		cfg := config.Config{Server: config.Server{ListenAddr: addr, DevAuthBypass: true}}
		if _, err := New(cfg); err == nil {
			t.Errorf("New accepted bypass on %q", addr)
		}
	}
}

// Without the bypass the session store is the only identity source, so New
// refuses to build an Authenticator that could admit no one.
func TestNewRequiresAnIdentitySource(t *testing.T) {
	for name, cfg := range map[string]config.Config{
		"bare config":          {Server: config.Server{ListenAddr: "0.0.0.0:8080"}},
		"sign-in, no sessions": signInConfig(),
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: New succeeded without the bypass or a session store", name)
		}
	}
	if _, err := New(signInConfig(), WithSessions(fakeSessions{}, "")); err == nil {
		t.Error("New succeeded without a sign-in path")
	}
}

// A loopback listener alone never turns the bypass on.
func TestNoBypassWithoutFlag(t *testing.T) {
	cfg := signInConfig()
	cfg.Server.ListenAddr = "127.0.0.1:8080"
	a, _ := sessionAuth(t, cfg, fakeSessions{})
	if code, email := serve(a, httptest.NewRequest(http.MethodPost, "/recipes", nil)); code != http.StatusUnauthorized || email != "" {
		t.Fatalf("loopback request without flag admitted: %d %q", code, email)
	}
}

func TestEmailFromContextEmpty(t *testing.T) {
	if _, ok := EmailFromContext(httptest.NewRequest(http.MethodGet, "/", nil).Context()); ok {
		t.Fatal("email present on an unauthenticated context")
	}
}

func devAuth(t *testing.T) *Authenticator {
	t.Helper()
	cfg := config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}}
	a, err := New(cfg, WithLogger(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func identityOf(a *Authenticator, header string) (int, string) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if header != "" {
		req.Header.Set(DevIdentityHeader, header)
	}
	return serve(a, req)
}

func TestDevBypassIdentityIsSwitchablePerRequest(t *testing.T) {
	a := devAuth(t)
	if code, got := identityOf(a, DevOtherEmail); code != http.StatusOK || got != DevOtherEmail {
		t.Errorf("override: got %d %q, want 200 %q", code, got, DevOtherEmail)
	}
	if code, got := identityOf(a, "stranger@example.com"); code != http.StatusOK || got != "stranger@example.com" {
		t.Errorf("override to a non-member: got %d %q", code, got)
	}
	if code, got := identityOf(a, ""); code != http.StatusOK || got != DevEmail {
		t.Errorf("no override: got %d %q, want %q", code, got, DevEmail)
	}
}

func TestDevBypassRefusesMalformedIdentity(t *testing.T) {
	a := devAuth(t)
	for _, v := range []string{"not-an-email", "Name <a@example.com>", "UPPER@example.com", "a@example.com, b@example.com"} {
		if code, got := identityOf(a, v); code != http.StatusForbidden || got != "" {
			t.Errorf("%q: got %d %q, want 403 and no identity", v, code, got)
		}
	}
}

// The override must not exist outside the bypass: with sessions, a client
// that sends it is still just a request without a session, and it cannot
// change the identity of one with a session.
func TestDevIdentityHeaderIgnoredWithoutBypass(t *testing.T) {
	a, _ := sessionAuth(t, signInConfig(), fakeSessions{emails: map[string]string{"good": "tim@example.com"}})
	req := httptest.NewRequest(http.MethodPost, "/recipes", nil)
	req.Header.Set(DevIdentityHeader, DevEmail)
	if code, _ := serve(a, req); code != http.StatusUnauthorized {
		t.Fatalf("identity header admitted a request without a session: %d", code)
	}
	req = withSession(httptest.NewRequest(http.MethodPost, "/recipes", nil), "good")
	req.Header.Set(DevIdentityHeader, "someone.else@example.com")
	if code, email := serve(a, req); code != http.StatusOK || email != "tim@example.com" {
		t.Fatalf("identity header changed a verified identity: %d %q", code, email)
	}
}
