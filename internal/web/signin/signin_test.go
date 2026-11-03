package signin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/session"
	"github.com/kaecyra/gobbler/internal/web/layout"
	"github.com/kaecyra/gobbler/internal/web/signin/signintest"
)

const baseURL = "https://gobbler.test"

type harness struct {
	iss      *signintest.Issuer
	svc      *Service
	sessions *session.Store
	app      http.Handler
	logs     *bytes.Buffer
}

func newHarness(t *testing.T) *harness {
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
	sessions, err := session.New(d, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{iss: signintest.New(t), sessions: sessions, logs: &bytes.Buffer{}}
	h.svc = h.service(t, h.iss.URL)
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+layout.SignInPath, h.svc.SignIn)
	mux.HandleFunc("GET "+layout.CallbackPath, h.svc.Callback)
	mux.HandleFunc("POST "+layout.SignOutPath, h.svc.SignOut)
	h.app = mux
	return h
}

func (h *harness) service(t *testing.T, issuer string) *Service {
	t.Helper()
	s, err := New(Settings{
		Issuer: issuer, ClientID: signintest.ClientID, ClientSecret: signintest.ClientSecret,
		PublicBaseURL: baseURL, Lifetime: 24 * time.Hour,
	}, h.sessions, WithLogger(slog.New(slog.NewJSONHandler(h.logs, nil))))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (h *harness) serve(r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.app.ServeHTTP(rec, r)
	return rec
}

func cookieNamed(res *http.Response, name string) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// start runs GET /login and returns the issuer URL it redirects to and the
// sign-in cookie it sets.
func (h *harness) start(t *testing.T, next string) (string, *http.Cookie) {
	t.Helper()
	target := layout.SignInPath
	if next != "" {
		target += "?next=" + url.QueryEscape(next)
	}
	rec := h.serve(httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login: status %d, body %s", rec.Code, rec.Body)
	}
	c := cookieNamed(rec.Result(), PendingCookie)
	if c == nil {
		t.Fatal("login set no sign-in cookie")
	}
	return rec.Header().Get("Location"), c
}

// callback sends the issuer's redirect back to the app with cookies.
func (h *harness) callback(back *url.URL, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, back.RequestURI(), nil)
	for _, c := range cookies {
		r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	return h.serve(r)
}

// signIn runs a whole sign-in and returns the callback's answer.
func (h *harness) signIn(t *testing.T, next string) *httptest.ResponseRecorder {
	t.Helper()
	authURL, pending := h.start(t, next)
	return h.callback(h.iss.Authorize(t, authURL), pending)
}

func TestSignInStartsWithPKCEStateAndNonce(t *testing.T) {
	h := newHarness(t)
	authURL, c := h.start(t, "/recipes")
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(authURL, h.iss.URL+"/authorize?") {
		t.Errorf("redirect %q is not the discovered authorization endpoint", authURL)
	}
	q := u.Query()
	p := decodePending(t, c.Value)
	for k, want := range map[string]string{
		"client_id": signintest.ClientID, "redirect_uri": baseURL + layout.CallbackPath, "response_type": "code",
		"scope": "openid email", "code_challenge_method": "S256", "state": p.State, "nonce": p.Nonce,
	} {
		if got := q.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if q.Get("code_challenge") == "" || q.Get("code_challenge") == p.Verifier {
		t.Error("the challenge must be derived from the verifier, never the verifier itself")
	}
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" || c.MaxAge != 600 {
		t.Errorf("sign-in cookie attributes: %+v", c)
	}
	if p.Next != "/recipes" {
		t.Errorf("next = %q", p.Next)
	}
}

func TestSignInStartsASession(t *testing.T) {
	h := newHarness(t)
	rec := h.signIn(t, "/recipes/7?scale=2")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/recipes/7?scale=2" {
		t.Fatalf("callback: %d to %q, body %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	res := rec.Result()
	c := cookieNamed(res, auth.SessionCookie)
	if c == nil || c.Value == "" {
		t.Fatal("no session cookie")
	}
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" || c.MaxAge != int((24*time.Hour).Seconds()) {
		t.Errorf("session cookie attributes: %+v", c)
	}
	if p := cookieNamed(res, PendingCookie); p == nil || p.MaxAge >= 0 {
		t.Errorf("the sign-in cookie must be cleared: %+v", p)
	}
	email, err := h.sessions.Lookup(context.Background(), c.Value)
	if err != nil || email != "member@example.com" {
		t.Errorf("session email = %q, %v; want the verified email, lower-cased", email, err)
	}
}

func TestSignInAgainReplacesTheSession(t *testing.T) {
	h := newHarness(t)
	old := cookieNamed(h.signIn(t, "/").Result(), auth.SessionCookie)
	authURL, pending := h.start(t, "/")
	rec := h.callback(h.iss.Authorize(t, authURL), pending, old)
	if cookieNamed(rec.Result(), auth.SessionCookie) == nil {
		t.Fatal("no new session")
	}
	if _, err := h.sessions.Lookup(context.Background(), old.Value); !errors.Is(err, session.ErrNoSession) {
		t.Errorf("old session still live: %v", err)
	}
}

func decodePending(t *testing.T, v string) pending {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil {
		t.Fatal(err)
	}
	var p pending
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func encodePending(t *testing.T, p pending) string {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// Every check of ADR-00030 obligation 1 refuses the sign-in when it fails,
// and no session is started.
func TestCallbackRefuses(t *testing.T) {
	type tamper struct {
		// issuer edits the fake issuer before the sign-in.
		issuer func(i *signintest.Issuer)
		// back edits the callback URL the issuer sends the browser to.
		back func(q url.Values)
		// cookie edits the sign-in cookie the browser sends back; returning
		// nil sends none.
		cookie func(t *testing.T, c *http.Cookie) *http.Cookie
	}
	claim := func(k string, v any) func(i *signintest.Issuer) {
		return func(i *signintest.Issuer) { i.Claims = func(c map[string]any) { c[k] = v } }
	}
	tests := []struct {
		name   string
		tamper tamper
		status int
	}{
		{"no sign-in cookie", tamper{cookie: func(*testing.T, *http.Cookie) *http.Cookie { return nil }}, http.StatusBadRequest},
		{"malformed sign-in cookie", tamper{cookie: func(_ *testing.T, c *http.Cookie) *http.Cookie {
			c.Value = "not-base64!"
			return c
		}}, http.StatusBadRequest},
		{"state does not match", tamper{back: func(q url.Values) { q.Set("state", "forged") }}, http.StatusBadRequest},
		{"state missing", tamper{back: func(q url.Values) { q.Del("state") }}, http.StatusBadRequest},
		{"code missing", tamper{back: func(q url.Values) { q.Del("code") }}, http.StatusBadRequest},
		{"issuer reports an error", tamper{back: func(q url.Values) { q.Del("code"); q.Set("error", "access_denied") }}, http.StatusForbidden},
		{"PKCE verifier does not match", tamper{cookie: func(t *testing.T, c *http.Cookie) *http.Cookie {
			p := decodePending(t, c.Value)
			p.Verifier = "a-different-verifier-of-the-required-length-0123456789"
			c.Value = encodePending(t, p)
			return c
		}}, http.StatusBadRequest},
		{"nonce does not match", tamper{issuer: claim("nonce", "replayed")}, http.StatusBadRequest},
		{"nonce missing", tamper{issuer: func(i *signintest.Issuer) { i.Claims = func(c map[string]any) { delete(c, "nonce") } }}, http.StatusBadRequest},
		{"bad signature", tamper{issuer: func(i *signintest.Issuer) { i.WrongKey = true }}, http.StatusBadRequest},
		{"wrong issuer", tamper{issuer: claim("iss", "https://issuer.elsewhere.test")}, http.StatusBadRequest},
		{"wrong audience", tamper{issuer: claim("aud", "another-client")}, http.StatusBadRequest},
		{"expired", tamper{issuer: func(i *signintest.Issuer) {
			i.Claims = func(c map[string]any) {
				c["iat"] = time.Now().Add(-2 * time.Hour).Unix()
				c["exp"] = time.Now().Add(-time.Hour).Unix()
			}
		}}, http.StatusBadRequest},
		{"email not verified", tamper{issuer: claim("email_verified", false)}, http.StatusForbidden},
		{"email_verified missing", tamper{issuer: func(i *signintest.Issuer) {
			i.Claims = func(c map[string]any) { delete(c, "email_verified") }
		}}, http.StatusForbidden},
		{"email_verified a string", tamper{issuer: claim("email_verified", "true")}, http.StatusForbidden},
		{"email not an address", tamper{issuer: claim("email", "Someone <x@example.com>")}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			if tt.tamper.issuer != nil {
				h.iss.Set(tt.tamper.issuer)
			}
			authURL, pending := h.start(t, "/recipes")
			back := h.iss.Authorize(t, authURL)
			if tt.tamper.back != nil {
				q := back.Query()
				tt.tamper.back(q)
				back.RawQuery = q.Encode()
			}
			var cookies []*http.Cookie
			if tt.tamper.cookie == nil {
				cookies = append(cookies, pending)
			} else if c := tt.tamper.cookie(t, pending); c != nil {
				cookies = append(cookies, c)
			}
			rec := h.callback(back, cookies...)
			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d; body %s", rec.Code, tt.status, rec.Body)
			}
			if c := cookieNamed(rec.Result(), auth.SessionCookie); c != nil {
				t.Errorf("a refused sign-in set a session cookie: %+v", c)
			}
			if n, err := h.sessions.DeleteForEmail(context.Background(), "member@example.com"); err != nil || n != 0 {
				t.Errorf("a refused sign-in left %d sessions (%v)", n, err)
			}
			if !strings.Contains(h.logs.String(), "sign-in failed") {
				t.Errorf("the refusal was not logged: %s", h.logs)
			}
		})
	}
}

// A code is good for one exchange: replaying the callback is refused.
func TestCallbackReplayRefused(t *testing.T) {
	h := newHarness(t)
	authURL, pending := h.start(t, "/")
	back := h.iss.Authorize(t, authURL)
	if rec := h.callback(back, pending); rec.Code != http.StatusSeeOther {
		t.Fatalf("first callback: %d", rec.Code)
	}
	if rec := h.callback(back, pending); rec.Code != http.StatusBadRequest {
		t.Errorf("replayed callback: %d, want 400", rec.Code)
	}
}

func TestSignOutEndsTheSession(t *testing.T) {
	h := newHarness(t)
	c := cookieNamed(h.signIn(t, "/").Result(), auth.SessionCookie)
	r := httptest.NewRequest(http.MethodPost, layout.SignOutPath, nil)
	r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	rec := h.serve(r)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("sign out: %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if cleared := cookieNamed(rec.Result(), auth.SessionCookie); cleared == nil || cleared.MaxAge >= 0 || !cleared.Secure || cleared.Path != "/" {
		t.Errorf("session cookie not cleared: %+v", cleared)
	}
	if _, err := h.sessions.Lookup(context.Background(), c.Value); !errors.Is(err, session.ErrNoSession) {
		t.Errorf("session survives sign-out: %v", err)
	}
}

func TestSignInUnavailableWhenDiscoveryFails(t *testing.T) {
	h := newHarness(t)
	h.svc = h.service(t, h.iss.URL+"/not-an-issuer")
	rec := httptest.NewRecorder()
	h.svc.SignIn(rec, httptest.NewRequest(http.MethodGet, layout.SignInPath, nil))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
	if cookieNamed(rec.Result(), PendingCookie) != nil {
		t.Error("a sign-in that never started set its cookie")
	}
	if !strings.Contains(h.logs.String(), "sign-in unavailable") {
		t.Errorf("not logged: %s", h.logs)
	}
}

// refuseToken fails every request to the issuer's token endpoint, as a
// network fault would, and passes the rest through.
type refuseToken struct{}

func (refuseToken) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/token" {
		return nil, errors.New("connection refused")
	}
	return http.DefaultTransport.RoundTrip(r)
}

// A token endpoint that cannot be reached is a 502, and the log says why
// without quoting the code.
func TestCallbackLogsWhyTheTokenEndpointIsUnreachable(t *testing.T) {
	h := newHarness(t)
	svc, err := New(Settings{
		Issuer: h.iss.URL, ClientID: signintest.ClientID, ClientSecret: signintest.ClientSecret,
		PublicBaseURL: baseURL, Lifetime: 24 * time.Hour,
	}, h.sessions, WithLogger(slog.New(slog.NewJSONHandler(h.logs, nil))),
		WithHTTPClient(&http.Client{Transport: refuseToken{}}))
	if err != nil {
		t.Fatal(err)
	}
	start := httptest.NewRecorder()
	svc.SignIn(start, httptest.NewRequest(http.MethodGet, layout.SignInPath, nil))
	pending := cookieNamed(start.Result(), PendingCookie)
	if pending == nil {
		t.Fatalf("login: status %d, no sign-in cookie", start.Code)
	}
	back := h.iss.Authorize(t, start.Header().Get("Location"))
	r := httptest.NewRequest(http.MethodGet, back.RequestURI(), nil)
	r.AddCookie(&http.Cookie{Name: pending.Name, Value: pending.Value})
	rec := httptest.NewRecorder()
	svc.Callback(rec, r)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
	logs := h.logs.String()
	if !strings.Contains(logs, "token endpoint unreachable") || !strings.Contains(logs, "connection refused") {
		t.Errorf("the log does not say why: %s", logs)
	}
	if code := back.Query().Get("code"); code == "" || strings.Contains(logs, code) {
		t.Errorf("the log quotes the code %q, or there was none: %s", code, logs)
	}
}

// Discovery must match the configured issuer exactly (ADR-00030).
func TestSignInRefusesAnIssuerThatNamesAnother(t *testing.T) {
	h := newHarness(t)
	h.svc = h.service(t, h.iss.URL+"/")
	rec := httptest.NewRecorder()
	h.svc.SignIn(rec, httptest.NewRequest(http.MethodGet, layout.SignInPath, nil))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestNewRefusesIncompleteSettings(t *testing.T) {
	h := newHarness(t)
	good := Settings{Issuer: "https://i.test", ClientID: "c", PublicBaseURL: baseURL, Lifetime: time.Hour}
	for name, s := range map[string]Settings{
		"no issuer":   {ClientID: "c", PublicBaseURL: baseURL, Lifetime: time.Hour},
		"no client":   {Issuer: "https://i.test", PublicBaseURL: baseURL, Lifetime: time.Hour},
		"no base URL": {Issuer: "https://i.test", ClientID: "c", Lifetime: time.Hour},
		"no lifetime": {Issuer: "https://i.test", ClientID: "c", PublicBaseURL: baseURL},
	} {
		if _, err := New(s, h.sessions); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := New(good, nil); err == nil {
		t.Error("no session store: want an error")
	}
}

// ADR-00030 obligation 4: no log line carries a session token, an
// authorization code or an ID token, whether the sign-in succeeds or fails.
func TestNothingSecretLogged(t *testing.T) {
	h := newHarness(t)
	var tokens []string
	for range 2 {
		c := cookieNamed(h.signIn(t, "/").Result(), auth.SessionCookie)
		tokens = append(tokens, c.Value)
	}
	h.iss.Set(func(i *signintest.Issuer) { i.WrongKey = true })
	h.signIn(t, "/")
	h.iss.Set(func(i *signintest.Issuer) {
		i.WrongKey = false
		i.Claims = func(c map[string]any) { c["email_verified"] = false }
	})
	h.signIn(t, "/")
	authURL, pending := h.start(t, "/")
	back := h.iss.Authorize(t, authURL)
	q := back.Query()
	q.Set("state", "forged")
	back.RawQuery = q.Encode()
	h.callback(back, pending)

	out := h.logs.String()
	if !strings.Contains(out, "signed in") || !strings.Contains(out, "sign-in failed") {
		t.Fatalf("expected sign-ins and failures to be logged: %s", out)
	}
	secrets := append(h.iss.Secrets(), tokens...)
	if len(secrets) < 6 {
		t.Fatalf("too few secrets issued to prove anything: %d", len(secrets))
	}
	for _, s := range secrets {
		for _, part := range strings.Split(s, ".") {
			if len(part) > 8 && strings.Contains(out, part) {
				t.Fatalf("log contains secret material %q", part)
			}
		}
	}
}

// ADR-00030 obligation 3: the return target is a path on this site or "/".
func TestLocalPath(t *testing.T) {
	for in, want := range map[string]string{
		"":                          "/",
		"/":                         "/",
		"/recipes":                  "/recipes",
		"/recipes/7?scale=2":        "/recipes/7?scale=2",
		"/recipes#steps":            "/recipes",
		"/a%20b":                    "/a%20b",
		"recipes":                   "/",
		"https://evil.example/":     "/",
		"http://evil.example":       "/",
		"//evil.example":            "/",
		"//evil.example/recipes":    "/",
		"///evil.example":           "/",
		"/\\evil.example":           "/",
		"\\\\evil.example":          "/",
		"/recipes\\..\\":            "/",
		"javascript:alert(1)":       "/",
		"/\tevil":                   "/",
		"/\nLocation: x":            "/",
		" /recipes":                 "/",
		"/login":                    "/",
		"/login?next=/recipes":      "/",
		"/auth/callback?code=x":     "/",
		"/%2F%2Fevil.example":       "/",
		"https:/evil.example":       "/",
		"/recipes?next=//evil":      "/recipes?next=//evil",
		"/canada":                   "/canada",
		"/invitations/Zz000000":     "/invitations/Zz000000",
		"/%0d%0aSet-Cookie:%20x=1":  "/%0d%0aSet-Cookie:%20x=1",
		"/recipes?q=%E2%9C%93#frag": "/recipes?q=%E2%9C%93",
	} {
		if got := LocalPath(in); got != want {
			t.Errorf("LocalPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// ADR-00032: the callback records the browser's user agent and its address,
// the address from CF-Connecting-IP behind the tunnel.
func TestSignInRecordsTheUserAgentAndIP(t *testing.T) {
	h := newHarness(t)
	authURL, pending := h.start(t, "/")
	r := httptest.NewRequest(http.MethodGet, h.iss.Authorize(t, authURL).RequestURI(), nil)
	r.AddCookie(&http.Cookie{Name: pending.Name, Value: pending.Value})
	r.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0")
	r.Header.Set("CF-Connecting-IP", "203.0.113.9")
	r.RemoteAddr = "172.18.0.2:44000"
	c := cookieNamed(h.serve(r).Result(), auth.SessionCookie)
	if c == nil {
		t.Fatal("no session cookie")
	}
	got, err := h.sessions.ListForEmail(context.Background(), "member@example.com", c.Value)
	if err != nil || len(got) != 1 {
		t.Fatalf("sessions = %v, %v", got, err)
	}
	if got[0].Client.IP != "203.0.113.9" || !strings.Contains(got[0].Client.UserAgent, "Firefox/130.0") || !got[0].Current {
		t.Errorf("recorded %+v", got[0])
	}
}
