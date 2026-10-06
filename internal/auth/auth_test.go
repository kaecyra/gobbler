package auth

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/config"
)

const (
	testTeam = "team.cloudflareaccess.com"
	testAUD  = "aud-tag-123"
	testKid  = "kid-1"
)

var (
	t0 = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

	keyOnce  sync.Once
	keyA     *rsa.PrivateKey
	keyOther *rsa.PrivateKey
)

func testKeys(t *testing.T) (*rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	keyOnce.Do(func() {
		var err error
		if keyA, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
		if keyOther, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
	})
	return keyA, keyOther
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func jsonB64(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b64(b)
}

type tokenSpec struct {
	alg    string
	kid    string
	claims map[string]any
	key    *rsa.PrivateKey // signs with this key; nil means unsigned
}

func goodClaims() map[string]any {
	return map[string]any{
		"iss":   "https://" + testTeam,
		"aud":   []string{testAUD},
		"email": "tim@example.com",
		"exp":   t0.Add(time.Hour).Unix(),
		"nbf":   t0.Add(-time.Minute).Unix(),
	}
}

func (s tokenSpec) sign(t *testing.T) string {
	t.Helper()
	signing := jsonB64(t, map[string]string{"alg": s.alg, "kid": s.kid, "typ": "JWT"}) + "." + jsonB64(t, s.claims)
	if s.key == nil {
		return signing + "."
	}
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + b64(sig)
}

func goodToken(t *testing.T, mutate func(*tokenSpec)) string {
	a, _ := testKeys(t)
	s := tokenSpec{alg: "RS256", kid: testKid, claims: goodClaims(), key: a}
	if mutate != nil {
		mutate(&s)
	}
	return s.sign(t)
}

type jwksServer struct {
	*httptest.Server
	hits   atomic.Int64
	status atomic.Int64
	mu     sync.Mutex
	kids   map[string]*rsa.PublicKey
	// gate, when set, makes each request announce itself on entered and then
	// block until gate is closed.
	gate    chan struct{}
	entered chan struct{}
}

func (j *jwksServer) setKeys(kids map[string]*rsa.PublicKey) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.kids = kids
}

// block makes every JWKS request wait until release is called. release is
// idempotent and also runs at cleanup, so a failing test cannot leave handlers
// stuck and hang server shutdown.
func (j *jwksServer) block(t *testing.T) (entered <-chan struct{}, release func()) {
	gate, ent := make(chan struct{}), make(chan struct{}, 8)
	j.mu.Lock()
	j.gate, j.entered = gate, ent
	j.mu.Unlock()
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	return ent, release
}

func newJWKS(t *testing.T) *jwksServer {
	t.Helper()
	a, _ := testKeys(t)
	j := &jwksServer{kids: map[string]*rsa.PublicKey{testKid: &a.PublicKey}}
	j.status.Store(http.StatusOK)
	j.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		j.hits.Add(1)
		j.mu.Lock()
		gate, entered := j.gate, j.entered
		j.mu.Unlock()
		if gate != nil {
			entered <- struct{}{}
			<-gate
		}
		if st := int(j.status.Load()); st != http.StatusOK {
			http.Error(w, "boom", st)
			return
		}
		j.mu.Lock()
		defer j.mu.Unlock()
		type jwk struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		}
		var out struct {
			Keys []jwk `json:"keys"`
		}
		for kid, k := range j.kids {
			out.Keys = append(out.Keys, jwk{kid, "RSA", "RS256", b64(k.N.Bytes()), b64(big.NewInt(int64(k.E)).Bytes())})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(j.Close)
	return j
}

func prodConfig() config.Config {
	return config.Config{
		Server: config.Server{ListenAddr: "0.0.0.0:8080"},
		Access: config.Access{TeamDomain: testTeam, AUD: testAUD},
	}
}

type harness struct {
	auth *Authenticator
	jwks *jwksServer
	logs *bytes.Buffer
	now  *atomic.Pointer[time.Time]
}

func (h *harness) setNow(tm time.Time) { h.now.Store(&tm) }

func newHarness(t *testing.T, extra ...Option) *harness {
	t.Helper()
	h := &harness{jwks: newJWKS(t), logs: &bytes.Buffer{}, now: &atomic.Pointer[time.Time]{}}
	h.setNow(t0)
	opts := append([]Option{
		withCertsURL(h.jwks.URL),
		withClock(func() time.Time { return *h.now.Load() }),
		WithLogger(slog.New(slog.NewJSONHandler(h.logs, nil))),
	}, extra...)
	a, err := New(prodConfig(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	h.auth = a
	return h
}

// serve runs one request through the middleware wrapping a handler that
// records the context email, and returns the status and that email.
func serve(h *harness, req *http.Request, exempt ...string) (int, string) {
	var got string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = EmailFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	h.auth.Middleware(exempt...)(next).ServeHTTP(rec, req)
	return rec.Code, got
}

func withHeader(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/recipes", nil)
	if token != "" {
		r.Header.Set(HeaderName, token)
	}
	return r
}

func TestValidTokenPutsEmailInContext(t *testing.T) {
	h := newHarness(t)
	code, email := serve(h, withHeader(goodToken(t, nil)))
	if code != http.StatusOK || email != "tim@example.com" {
		t.Fatalf("got %d %q, want 200 tim@example.com", code, email)
	}
}

func TestAudAsPlainString(t *testing.T) {
	h := newHarness(t)
	tok := goodToken(t, func(s *tokenSpec) { s.claims["aud"] = testAUD })
	if code, _ := serve(h, withHeader(tok)); code != http.StatusOK {
		t.Fatalf("string aud rejected: %d", code)
	}
}

func TestCookieFallback(t *testing.T) {
	h := newHarness(t)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: goodToken(t, nil)})
	code, email := serve(h, r)
	if code != http.StatusOK || email != "tim@example.com" {
		t.Fatalf("got %d %q", code, email)
	}
}

func TestHeaderWinsOverCookie(t *testing.T) {
	h := newHarness(t)
	r := withHeader("garbage")
	r.AddCookie(&http.Cookie{Name: CookieName, Value: goodToken(t, nil)})
	if code, _ := serve(h, r); code != http.StatusForbidden {
		t.Fatalf("bad header with good cookie: got %d, want 403", code)
	}
}

func TestRejections(t *testing.T) {
	_, other := testKeys(t)
	cases := []struct {
		name  string
		token func(t *testing.T) string
	}{
		{"missing token", func(*testing.T) string { return "" }},
		{"malformed token", func(*testing.T) string { return "not-a-jwt" }},
		{"malformed parts", func(*testing.T) string { return "a.b.c" }},
		{"unsigned token", func(t *testing.T) string {
			return goodToken(t, func(s *tokenSpec) { s.key = nil })
		}},
		{"wrong signature", func(t *testing.T) string {
			return goodToken(t, func(s *tokenSpec) { s.key = other })
		}},
		{"expired", func(t *testing.T) string {
			return goodToken(t, func(s *tokenSpec) { s.claims["exp"] = t0.Add(-time.Hour).Unix() })
		}},
		{"no exp", func(t *testing.T) string {
			return goodToken(t, func(s *tokenSpec) { delete(s.claims, "exp") })
		}},
		{"not yet valid", func(t *testing.T) string {
			return goodToken(t, func(s *tokenSpec) { s.claims["nbf"] = t0.Add(time.Hour).Unix() })
		}},
		{"wrong aud", func(t *testing.T) string {
			return goodToken(t, func(s *tokenSpec) { s.claims["aud"] = []string{"other"} })
		}},
		{"wrong iss", func(t *testing.T) string {
			return goodToken(t, func(s *tokenSpec) { s.claims["iss"] = "https://evil.cloudflareaccess.com" })
		}},
		{"no email", func(t *testing.T) string {
			return goodToken(t, func(s *tokenSpec) { delete(s.claims, "email") })
		}},
		{"alg none", func(t *testing.T) string {
			return goodToken(t, func(s *tokenSpec) { s.alg = "none"; s.key = nil })
		}},
		{"alg none with valid signature bytes", func(t *testing.T) string {
			return goodToken(t, func(s *tokenSpec) { s.alg = "none" })
		}},
		{"alg HS256", func(t *testing.T) string {
			return goodToken(t, func(s *tokenSpec) { s.alg = "HS256" })
		}},
		{"unknown kid", func(t *testing.T) string {
			return goodToken(t, func(s *tokenSpec) { s.kid = "nope" })
		}},
		{"missing kid", func(t *testing.T) string {
			return goodToken(t, func(s *tokenSpec) { s.kid = "" })
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			code, email := serve(h, withHeader(tc.token(t)))
			if code != http.StatusForbidden || email != "" {
				t.Fatalf("got %d %q, want 403 and no identity", code, email)
			}
		})
	}
}

func TestClockLeewayIsBounded(t *testing.T) {
	for name, mutate := range map[string]func(*tokenSpec){
		"exp just past leeway": func(s *tokenSpec) { s.claims["exp"] = t0.Add(-31 * time.Second).Unix() },
		"nbf just past leeway": func(s *tokenSpec) { s.claims["nbf"] = t0.Add(31 * time.Second).Unix() },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			if code, _ := serve(h, withHeader(goodToken(t, mutate))); code != http.StatusForbidden {
				t.Fatalf("got %d, want 403", code)
			}
		})
	}
}

func TestClockLeewayAllowsSmallSkew(t *testing.T) {
	h := newHarness(t)
	tok := goodToken(t, func(s *tokenSpec) {
		s.claims["exp"] = t0.Add(-5 * time.Second).Unix()
		s.claims["nbf"] = t0.Add(5 * time.Second).Unix()
	})
	if code, _ := serve(h, withHeader(tok)); code != http.StatusOK {
		t.Fatalf("got %d, want 200 within leeway", code)
	}
}

func TestExpiryFollowsClock(t *testing.T) {
	h := newHarness(t)
	tok := goodToken(t, nil)
	if code, _ := serve(h, withHeader(tok)); code != http.StatusOK {
		t.Fatalf("fresh: %d", code)
	}
	h.setNow(t0.Add(2 * time.Hour))
	if code, _ := serve(h, withHeader(tok)); code != http.StatusForbidden {
		t.Fatalf("after expiry: %d", code)
	}
}

func TestJWKSCachedAcrossRequests(t *testing.T) {
	h := newHarness(t)
	for range 5 {
		serve(h, withHeader(goodToken(t, nil)))
	}
	if n := h.jwks.hits.Load(); n != 1 {
		t.Fatalf("JWKS fetched %d times, want once", n)
	}
}

func TestRefreshOnUnknownKidPicksUpRotation(t *testing.T) {
	h := newHarness(t, withMinRefresh(time.Minute))
	a, other := testKeys(t)
	serve(h, withHeader(goodToken(t, nil))) // warm the cache

	h.jwks.setKeys(map[string]*rsa.PublicKey{testKid: &a.PublicKey, "kid-2": &other.PublicKey})
	h.setNow(t0.Add(2 * time.Minute))
	tok := goodToken(t, func(s *tokenSpec) { s.kid = "kid-2"; s.key = other })
	if code, _ := serve(h, withHeader(tok)); code != http.StatusOK {
		t.Fatalf("rotated key rejected: %d", code)
	}
}

func TestRefreshIsRateLimited(t *testing.T) {
	h := newHarness(t, withMinRefresh(time.Minute))
	for i := range 50 {
		tok := goodToken(t, func(s *tokenSpec) { s.kid = "bogus" + string(rune('a'+i%26)) })
		if code, _ := serve(h, withHeader(tok)); code != http.StatusForbidden {
			t.Fatalf("bogus kid admitted: %d", code)
		}
	}
	if n := h.jwks.hits.Load(); n != 1 {
		t.Fatalf("JWKS fetched %d times for bogus kids inside the interval, want once", n)
	}
	h.setNow(t0.Add(2 * time.Minute))
	serve(h, withHeader(goodToken(t, func(s *tokenSpec) { s.kid = "bogus" })))
	if n := h.jwks.hits.Load(); n != 2 {
		t.Fatalf("JWKS fetched %d times after the interval, want twice", n)
	}
}

func TestJWKSFailureOnColdCacheFailsClosed(t *testing.T) {
	h := newHarness(t)
	h.jwks.status.Store(http.StatusInternalServerError)
	code, email := serve(h, withHeader(goodToken(t, nil)))
	if code != http.StatusForbidden || email != "" {
		t.Fatalf("got %d %q, want 403", code, email)
	}
	if !strings.Contains(h.logs.String(), "refresh signing keys") {
		t.Fatalf("fetch failure not logged: %s", h.logs)
	}
}

func TestJWKSUnreachableFailsClosed(t *testing.T) {
	h := newHarness(t)
	h.jwks.Close()
	if code, _ := serve(h, withHeader(goodToken(t, nil))); code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", code)
	}
}

func TestJWKSFailureThenRecovery(t *testing.T) {
	h := newHarness(t, withMinRefresh(time.Minute))
	h.jwks.status.Store(http.StatusInternalServerError)
	serve(h, withHeader(goodToken(t, nil)))
	h.jwks.status.Store(http.StatusOK)
	h.setNow(t0.Add(2 * time.Minute))
	if code, _ := serve(h, withHeader(goodToken(t, nil))); code != http.StatusOK {
		t.Fatalf("no recovery after JWKS came back: %d", code)
	}
}

func TestExemptPath(t *testing.T) {
	h := newHarness(t)
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	if code, _ := serve(h, r, "/healthz"); code != http.StatusOK {
		t.Fatalf("exempt path without token: %d", code)
	}
	r = httptest.NewRequest(http.MethodGet, "/recipes", nil)
	if code, _ := serve(h, r, "/healthz"); code != http.StatusForbidden {
		t.Fatalf("non-exempt path without token: %d", code)
	}
	r = httptest.NewRequest(http.MethodGet, "/healthz/extra", nil)
	if code, _ := serve(h, r, "/healthz"); code != http.StatusForbidden {
		t.Fatalf("exemption must be an exact match: %d", code)
	}
}

func TestTokenNeverLogged(t *testing.T) {
	_, other := testKeys(t)
	secret := "SECRETMARKER"
	bad := []string{
		goodToken(t, func(s *tokenSpec) { s.key = other; s.claims["email"] = secret }),
		goodToken(t, func(s *tokenSpec) { s.claims["aud"] = []string{secret}; s.claims["email"] = secret }),
		goodToken(t, func(s *tokenSpec) { s.claims["exp"] = 1; s.claims["email"] = secret }),
		goodToken(t, func(s *tokenSpec) { s.kid = secret }),
		goodToken(t, func(s *tokenSpec) { s.alg = secret }),
		b64([]byte(secret)) + "." + b64([]byte(secret)) + "." + b64([]byte(secret)),
		secret,
	}
	for _, viaCookie := range []bool{false, true} {
		h := newHarness(t)
		for _, tok := range bad {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if viaCookie {
				r.AddCookie(&http.Cookie{Name: CookieName, Value: tok})
			} else {
				r.Header.Set(HeaderName, tok)
			}
			if code, _ := serve(h, r); code != http.StatusForbidden {
				t.Fatalf("got %d, want 403", code)
			}
		}
		out := h.logs.String()
		if out == "" {
			t.Fatal("expected denials to be logged")
		}
		for _, tok := range append(bad, strings.Split(bad[0], ".")...) {
			if strings.Contains(out, tok) {
				t.Fatalf("log contains token material %q", tok)
			}
		}
		if strings.Contains(out, secret) || strings.Contains(out, b64([]byte(secret))) {
			t.Fatalf("log contains token content: %s", out)
		}
	}
}

func TestDevBypass(t *testing.T) {
	cfg := config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}}
	a, err := New(cfg, WithLogger(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))))
	if err != nil {
		t.Fatal(err)
	}
	var got string
	rec := httptest.NewRecorder()
	a.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = EmailFromContext(r.Context())
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || got != DevEmail {
		t.Fatalf("got %d %q, want 200 %q", rec.Code, got, DevEmail)
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

func TestNoBypassWithoutFlag(t *testing.T) {
	h := newHarness(t)
	if code, _ := serve(h, withHeader("")); code != http.StatusForbidden {
		t.Fatalf("loopback-style request without flag admitted: %d", code)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	for name, acc := range map[string]config.Access{
		"no team":     {AUD: "a"},
		"no aud":      {TeamDomain: testTeam},
		"url as team": {TeamDomain: "https://x.example.com/path", AUD: "a"},
	} {
		if _, err := New(config.Config{Access: acc}); err == nil {
			t.Errorf("%s: New succeeded", name)
		}
	}
}

func TestDefaultCertsURL(t *testing.T) {
	a, err := New(prodConfig())
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://team.cloudflareaccess.com/cdn-cgi/access/certs"; a.keys.url != want {
		t.Fatalf("certs url %q, want %q", a.keys.url, want)
	}
}

func TestEmailFromContextEmpty(t *testing.T) {
	if _, ok := EmailFromContext(httptest.NewRequest(http.MethodGet, "/", nil).Context()); ok {
		t.Fatal("email present on an unauthenticated context")
	}
}

func TestBadJWKSDocuments(t *testing.T) {
	for name, body := range map[string]string{
		"not json":     "nope",
		"no keys":      `{"keys":[]}`,
		"bad modulus":  `{"keys":[{"kid":"kid-1","kty":"RSA","n":"!!","e":"AQAB"}]}`,
		"bad exponent": `{"keys":[{"kid":"kid-1","kty":"RSA","n":"AQAB","e":"!!"}]}`,
		"tiny exp":     `{"keys":[{"kid":"kid-1","kty":"RSA","n":"AQAB","e":"AQ"}]}`,
		"only EC":      `{"keys":[{"kid":"kid-1","kty":"EC"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer srv.Close()
			a, err := New(prodConfig(), withCertsURL(srv.URL), WithLogger(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))))
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			a.Middleware()(http.NotFoundHandler()).ServeHTTP(rec, withHeader(goodToken(t, nil)))
			if rec.Code != http.StatusForbidden {
				t.Fatalf("got %d, want 403", rec.Code)
			}
		})
	}
}

func TestCachedKidServedWhileRefreshBlocked(t *testing.T) {
	h := newHarness(t, withMinRefresh(time.Minute))
	if code, _ := serve(h, withHeader(goodToken(t, nil))); code != http.StatusOK {
		t.Fatalf("warm-up: %d", code)
	}
	entered, release := h.jwks.block(t)
	h.setNow(t0.Add(2 * time.Minute))

	done := make(chan int, 1)
	go func() {
		code, _ := serve(h, withHeader(goodToken(t, func(s *tokenSpec) { s.kid = "unknown" })))
		done <- code
	}()
	<-entered // the refresh is now blocked inside the JWKS server

	cached := make(chan int, 1)
	go func() {
		code, _ := serve(h, withHeader(goodToken(t, func(s *tokenSpec) { s.claims["exp"] = t0.Add(3 * time.Hour).Unix() })))
		cached <- code
	}()
	select {
	case code := <-cached:
		if code != http.StatusOK {
			t.Fatalf("cached-kid request got %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cached-kid request stalled behind a blocked refresh")
	}
	release()
	if code := <-done; code != http.StatusForbidden {
		t.Fatalf("unknown kid got %d, want 403", code)
	}
}

func TestConcurrentMissesShareOneFetch(t *testing.T) {
	h := newHarness(t)
	entered, release := h.jwks.block(t)

	const n = 5
	var wg sync.WaitGroup
	codes := make(chan int, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := serve(h, withHeader(goodToken(t, nil)))
			codes <- code
		}()
	}
	<-entered
	time.Sleep(50 * time.Millisecond) // let the others queue behind the flight
	release()
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != http.StatusOK {
			t.Fatalf("got %d, want 200", code)
		}
	}
	if got := h.jwks.hits.Load(); got != 1 {
		t.Fatalf("JWKS fetched %d times, want once", got)
	}
}

func TestOutageReportsFetchErrorNotUnknownKey(t *testing.T) {
	h := newHarness(t, withMinRefresh(time.Minute))
	h.jwks.status.Store(http.StatusInternalServerError)
	serve(h, withHeader(goodToken(t, nil))) // fetch fails
	h.logs.Reset()
	serve(h, withHeader(goodToken(t, nil))) // inside the rate-limit window
	out := h.logs.String()
	if !strings.Contains(out, "refresh signing keys") || strings.Contains(out, "unknown signing key") {
		t.Fatalf("outage not reported as a fetch failure: %s", out)
	}
	if n := h.jwks.hits.Load(); n != 1 {
		t.Fatalf("fetched %d times, want once", n)
	}
}

func TestFailedRefreshKeepsWarmKeysInService(t *testing.T) {
	h := newHarness(t, withMinRefresh(time.Minute))
	if code, _ := serve(h, withHeader(goodToken(t, nil))); code != http.StatusOK {
		t.Fatalf("warm-up: %d", code)
	}
	h.jwks.status.Store(http.StatusInternalServerError)
	h.setNow(t0.Add(2 * time.Minute))
	serve(h, withHeader(goodToken(t, func(s *tokenSpec) { s.kid = "unknown" }))) // failed refresh
	if code, _ := serve(h, withHeader(goodToken(t, nil))); code != http.StatusOK {
		t.Fatalf("warm key stopped working after a failed refresh: %d", code)
	}
}
