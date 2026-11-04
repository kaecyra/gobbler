package oauth_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/oauth"
	"github.com/kaecyra/gobbler/internal/web/layout"
	weboauth "github.com/kaecyra/gobbler/internal/web/oauth"
)

const (
	base     = "https://gobbler.test"
	resource = base + "/mcp"
	verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	redirect = "https://app.example/callback"
	ann      = "ann@example.com"
)

// stubMetadata serves metadata documents from memory.
type stubMetadata map[string]oauth.Client

func (s stubMetadata) Fetch(_ context.Context, id string) (oauth.Client, error) {
	if c, ok := s[id]; ok {
		return c, nil
	}
	return oauth.Client{}, errors.New("no such document")
}

type site struct {
	t     *testing.T
	h     http.Handler
	d     *sql.DB
	store *oauth.Store
	logs  *bytes.Buffer
}

// newSite mounts the endpoints behind the real identity middleware (the dev
// bypass, which reads the identity from a header), over a real database.
func newSite(t *testing.T, meta weboauth.MetadataSource) *site {
	t.Helper()
	return newSiteWith(t, meta, nil)
}

// newSiteWith is newSite with a chance to change the dependencies first.
func newSiteWith(t *testing.T, meta weboauth.MetadataSource, change func(*weboauth.Deps)) *site {
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
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:0", DevAuthBypass: true}})
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	deps := weboauth.Deps{
		OAuth: oauth.New(d), Metadata: meta, PublicBaseURL: base,
		Logger: slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	if change != nil {
		change(&deps)
	}
	r := chi.NewRouter()
	r.Use(a.Middleware(layout.IsPublic))
	r.Mount(weboauth.WellKnownPattern, weboauth.WellKnownRoutes(deps))
	r.Mount(weboauth.Pattern, weboauth.Routes(deps))
	return &site{t: t, h: r, d: d, store: deps.OAuth, logs: logs}
}

func (s *site) do(req *http.Request, email string) *httptest.ResponseRecorder {
	if email != "" {
		req.Header.Set(auth.DevIdentityHeader, email)
	}
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	return rec
}

func (s *site) get(target, email string) *httptest.ResponseRecorder {
	return s.do(httptest.NewRequest(http.MethodGet, target, nil), email)
}

func (s *site) postForm(target, email string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.do(req, email)
}

func (s *site) postJSON(target string, body any) *httptest.ResponseRecorder {
	b, err := json.Marshal(body)
	if err != nil {
		s.t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return s.do(req, "")
}

func jsonOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("not JSON (%d): %s", rec.Code, rec.Body)
	}
	return m
}

// register is Dynamic Client Registration, returning the client id.
func (s *site) register(redirects ...string) string {
	s.t.Helper()
	if len(redirects) == 0 {
		redirects = []string{redirect}
	}
	rec := s.postJSON(weboauth.RegisterPath, map[string]any{"client_name": "Test agent", "redirect_uris": redirects, "token_endpoint_auth_method": "none"})
	if rec.Code != http.StatusCreated {
		s.t.Fatalf("register = %d: %s", rec.Code, rec.Body)
	}
	return jsonOf(s.t, rec)["client_id"].(string)
}

// authQuery is a valid authorization request for the client.
func authQuery(clientID string) url.Values {
	return url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect}, "state": {"st4te"},
		"code_challenge": {oauth.Challenge(verifier)}, "code_challenge_method": {"S256"}, "resource": {resource},
	}
}

// approve runs the consent POST for a request and returns the redirect it
// ends in.
func (s *site) approve(q url.Values, email string) *url.URL {
	s.t.Helper()
	q.Set("decision", "allow")
	rec := s.postForm(weboauth.AuthorizePath, email, q)
	if rec.Code != http.StatusSeeOther {
		s.t.Fatalf("approve = %d: %s", rec.Code, rec.Body)
	}
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		s.t.Fatal(err)
	}
	return u
}

// codes is how many authorization codes are stored.
func (s *site) codes() int {
	s.t.Helper()
	var n int
	if err := s.d.QueryRow(`SELECT count(*) FROM oauth_codes`).Scan(&n); err != nil {
		s.t.Fatal(err)
	}
	return n
}

func (s *site) token(form url.Values) *httptest.ResponseRecorder {
	return s.postForm(weboauth.TokenPath, "", form)
}

func codeForm(clientID, code string) url.Values {
	return url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code}, "redirect_uri": {redirect}, "code_verifier": {verifier}, "resource": {resource}}
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status || jsonOf(t, rec)["error"] != code {
		t.Fatalf("response = %d %s, want %d %s", rec.Code, rec.Body, status, code)
	}
}

func TestDiscoveryDocumentsArePublicAndDescribeTheServer(t *testing.T) {
	s := newSite(t, stubMetadata{})
	for _, p := range []string{weboauth.ResourceMetadataPath, weboauth.WellKnownPattern + "/oauth-protected-resource"} {
		rec := s.get(p, "")
		m := jsonOf(t, rec)
		if rec.Code != http.StatusOK || m["resource"] != resource || m["authorization_servers"].([]any)[0] != base {
			t.Errorf("%s = %d %s", p, rec.Code, rec.Body)
		}
	}
	rec := s.get(weboauth.ServerMetadataPath, "")
	m := jsonOf(t, rec)
	if rec.Code != http.StatusOK || m["issuer"] != base || m["authorization_endpoint"] != base+"/oauth/authorize" ||
		m["token_endpoint"] != base+"/oauth/token" || m["registration_endpoint"] != base+"/oauth/register" ||
		m["client_id_metadata_document_supported"] != true || m["code_challenge_methods_supported"].([]any)[0] != "S256" {
		t.Errorf("authorization server metadata = %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("discovery documents are not readable by a browser client")
	}
}

// Every route this package serves is public except the authorization endpoint,
// which needs a session. The routes come from the routers, so a route added
// here without a line in layout, or dropped from layout, fails this test.
func TestEveryRouteButAuthorizeIsPublic(t *testing.T) {
	r := chi.NewRouter()
	d := weboauth.Deps{}
	r.Mount(weboauth.WellKnownPattern, weboauth.WellKnownRoutes(d))
	r.Mount(weboauth.Pattern, weboauth.Routes(d))
	seen := map[string]bool{}
	err := chi.Walk(r, func(_, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		seen[pattern] = true
		if want := pattern != weboauth.AuthorizePath; layout.IsPublic(pattern) != want {
			t.Errorf("IsPublic(%s) = %v, want %v", pattern, !want, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{weboauth.ResourceMetadataPath, weboauth.WellKnownPattern + "/oauth-protected-resource", weboauth.ServerMetadataPath, weboauth.RegisterPath, weboauth.TokenPath, weboauth.AuthorizePath} {
		if !seen[p] {
			t.Errorf("%s is not a route", p)
		}
	}
}

func TestFullFlowWithARegisteredClient(t *testing.T) {
	s := newSite(t, stubMetadata{})
	clientID := s.register()

	page := s.get(weboauth.AuthorizePath+"?"+authQuery(clientID).Encode(), ann)
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, "Test agent") || !strings.Contains(body, `data-testid="consent-return-host">app.example<`) || !strings.Contains(body, ann) {
		t.Fatalf("consent page = %d:\n%s", page.Code, body)
	}
	if page.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(page.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") ||
		page.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("consent page headers = %v", page.Header())
	}

	u := s.approve(authQuery(clientID), ann)
	q := u.Query()
	if u.Host != "app.example" || u.Path != "/callback" || q.Get("state") != "st4te" || q.Get("iss") != base || q.Get("code") == "" {
		t.Fatalf("redirect = %s", u)
	}

	rec := s.token(codeForm(clientID, q.Get("code")))
	toks := jsonOf(t, rec)
	if rec.Code != http.StatusOK || toks["token_type"] != "Bearer" || toks["expires_in"] != float64(3600) ||
		rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("token = %d %s %v", rec.Code, rec.Body, rec.Header())
	}
	access, refresh := toks["access_token"].(string), toks["refresh_token"].(string)
	p, err := s.store.Verify(context.Background(), access, resource)
	if err != nil || p.Email != ann {
		t.Fatalf("Verify = %+v, %v", p, err)
	}

	rec = s.token(url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {refresh}, "resource": {resource}})
	next := jsonOf(t, rec)
	if rec.Code != http.StatusOK || next["refresh_token"] == refresh {
		t.Fatalf("refresh = %d %s", rec.Code, rec.Body)
	}
	// The retired refresh token comes back: the grant is revoked.
	wantError(t, s.token(url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {refresh}}), http.StatusBadRequest, "invalid_grant")
	if _, err := s.store.Verify(context.Background(), next["access_token"].(string), resource); !errors.Is(err, oauth.ErrInvalidToken) {
		t.Fatalf("access token after refresh reuse: %v", err)
	}
}

func TestFullFlowWithAMetadataDocumentClient(t *testing.T) {
	docURL := "https://agent.example/client.json"
	s := newSite(t, stubMetadata{docURL: {ID: docURL, Name: "Doc agent", RedirectURIs: []string{redirect, "http://127.0.0.1/cb"}}})
	page := s.get(weboauth.AuthorizePath+"?"+authQuery(docURL).Encode(), ann)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Doc agent") {
		t.Fatalf("consent page = %d:\n%s", page.Code, page.Body)
	}
	u := s.approve(authQuery(docURL), ann)
	rec := s.token(codeForm(docURL, u.Query().Get("code")))
	if rec.Code != http.StatusOK {
		t.Fatalf("token = %d %s", rec.Code, rec.Body)
	}
	// A loopback redirect on another port is the registered one.
	q := authQuery(docURL)
	q.Set("redirect_uri", "http://127.0.0.1:53211/cb")
	if page := s.get(weboauth.AuthorizePath+"?"+q.Encode(), ann); page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "127.0.0.1:53211") {
		t.Fatalf("loopback redirect = %d", page.Code)
	}
}

func TestDenyingReturnsAccessDeniedAndIssuesNothing(t *testing.T) {
	s := newSite(t, stubMetadata{})
	clientID := s.register()
	q := authQuery(clientID)
	q.Set("decision", "deny")
	rec := s.postForm(weboauth.AuthorizePath, ann, q)
	u, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || u.Query().Get("error") != "access_denied" || u.Query().Get("state") != "st4te" || u.Query().Get("code") != "" {
		t.Fatalf("deny = %d to %s", rec.Code, rec.Header().Get("Location"))
	}
	// Anything but "allow" is a denial.
	q.Set("decision", "")
	if rec := s.postForm(weboauth.AuthorizePath, ann, q); !strings.Contains(rec.Header().Get("Location"), "access_denied") {
		t.Fatalf("empty decision = %d to %s", rec.Code, rec.Header().Get("Location"))
	}
	if n := s.codes(); n != 0 {
		t.Fatalf("%d codes issued", n)
	}
}

// Each refusal of the authorization request that can be made to the client.
func TestAuthorizationRequestRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(url.Values)
		want string
	}{
		"no PKCE challenge":     {func(q url.Values) { q.Del("code_challenge") }, "invalid_request"},
		"no challenge method":   {func(q url.Values) { q.Del("code_challenge_method") }, "invalid_request"},
		"plain challenge":       {func(q url.Values) { q.Set("code_challenge_method", "plain") }, "invalid_request"},
		"malformed challenge":   {func(q url.Values) { q.Set("code_challenge", "short") }, "invalid_request"},
		"no resource":           {func(q url.Values) { q.Del("resource") }, "invalid_request"},
		"wrong resource":        {func(q url.Values) { q.Set("resource", "https://other.example/mcp") }, "invalid_target"},
		"resource with a slash": {func(q url.Values) { q.Set("resource", resource+"/") }, "invalid_target"},
		"implicit flow":         {func(q url.Values) { q.Set("response_type", "token") }, "unsupported_response_type"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSite(t, stubMetadata{})
			q := authQuery(s.register())
			tc.edit(q)
			for _, rec := range []*httptest.ResponseRecorder{
				s.get(weboauth.AuthorizePath+"?"+q.Encode(), ann),
				func() *httptest.ResponseRecorder {
					q.Set("decision", "allow")
					return s.postForm(weboauth.AuthorizePath, ann, q)
				}(),
			} {
				u, err := url.Parse(rec.Header().Get("Location"))
				if rec.Code != http.StatusSeeOther || err != nil || u.Host != "app.example" {
					t.Fatalf("response = %d %q", rec.Code, rec.Header().Get("Location"))
				}
				if u.Query().Get("error") != tc.want || u.Query().Get("state") != "st4te" || u.Query().Get("code") != "" {
					t.Fatalf("redirect = %s, want error %s with the state and no code", u, tc.want)
				}
			}
		})
	}
}

// A request whose client or redirect URI cannot be trusted is never redirected.
func TestUntrustedClientsAndRedirectsGetAPageNotARedirect(t *testing.T) {
	for name, edit := range map[string]func(url.Values){
		"no client":                   func(q url.Values) { q.Del("client_id") },
		"unknown client":              func(q url.Values) { q.Set("client_id", "gobc_unknown") },
		"unregistered redirect":       func(q url.Values) { q.Set("redirect_uri", "https://evil.example/callback") },
		"redirect with a longer path": func(q url.Values) { q.Set("redirect_uri", redirect+"/x") },
		"no redirect":                 func(q url.Values) { q.Del("redirect_uri") },
		"metadata url not https":      func(q url.Values) { q.Set("client_id", "http://agent.example/client.json") },
		"metadata url on loopback":    func(q url.Values) { q.Set("client_id", "https://127.0.0.1/client.json") },
		"metadata url on a private":   func(q url.Values) { q.Set("client_id", "https://10.0.0.5/client.json") },
		"metadata url on link-local":  func(q url.Values) { q.Set("client_id", "https://169.254.169.254/latest/meta-data") },
	} {
		t.Run(name, func(t *testing.T) {
			// The real fetcher: its refusal, not a stub's, is under test.
			s := newSite(t, oauth.NewMetadataFetcher())
			q := authQuery(s.register())
			edit(q)
			rec := s.get(weboauth.AuthorizePath+"?"+q.Encode(), ann)
			if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
				t.Fatalf("GET = %d, location %q", rec.Code, rec.Header().Get("Location"))
			}
			q.Set("decision", "allow")
			rec = s.postForm(weboauth.AuthorizePath, ann, q)
			if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
				t.Fatalf("POST = %d, location %q", rec.Code, rec.Header().Get("Location"))
			}
			if n := s.codes(); n != 0 {
				t.Fatalf("%d codes issued", n)
			}
		})
	}
}

func TestTheAuthorizationEndpointNeedsAnIdentity(t *testing.T) {
	// Under the dev bypass every request has one, so this checks the handler's
	// own guard by calling it without the middleware.
	d := weboauth.Deps{PublicBaseURL: base}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := httptest.NewRecorder()
		weboauth.Routes(d).ServeHTTP(rec, httptest.NewRequest(method, "/authorize", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s with no identity = %d", method, rec.Code)
		}
	}
}

func TestTokenRefusals(t *testing.T) {
	s := newSite(t, stubMetadata{})
	clientID := s.register()
	code := func() string { return s.approve(authQuery(clientID), ann).Query().Get("code") }
	bad := http.StatusBadRequest

	t.Run("a wrong PKCE verifier", func(t *testing.T) {
		f := codeForm(clientID, code())
		f.Set("code_verifier", "xBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
		wantError(t, s.token(f), bad, "invalid_grant")
	})
	t.Run("no PKCE verifier", func(t *testing.T) {
		f := codeForm(clientID, code())
		f.Del("code_verifier")
		wantError(t, s.token(f), bad, "invalid_request")
	})
	t.Run("a wrong resource", func(t *testing.T) {
		f := codeForm(clientID, code())
		f.Set("resource", "https://other.example/mcp")
		wantError(t, s.token(f), bad, "invalid_target")
	})
	t.Run("no resource", func(t *testing.T) {
		f := codeForm(clientID, code())
		f.Del("resource")
		wantError(t, s.token(f), bad, "invalid_request")
	})
	t.Run("another client", func(t *testing.T) {
		wantError(t, s.token(codeForm(s.register(), code())), bad, "invalid_grant")
	})
	t.Run("another redirect uri", func(t *testing.T) {
		f := codeForm(clientID, code())
		f.Set("redirect_uri", "https://evil.example/callback")
		wantError(t, s.token(f), bad, "invalid_grant")
	})
	t.Run("a reused code, which also revokes the grant", func(t *testing.T) {
		c := code()
		first := jsonOf(t, s.token(codeForm(clientID, c)))
		wantError(t, s.token(codeForm(clientID, c)), bad, "invalid_grant")
		if _, err := s.store.Verify(context.Background(), first["access_token"].(string), resource); !errors.Is(err, oauth.ErrInvalidToken) {
			t.Fatalf("access token after code reuse: %v", err)
		}
	})
	t.Run("an unsupported grant type", func(t *testing.T) {
		wantError(t, s.token(url.Values{"grant_type": {"password"}, "client_id": {clientID}}), bad, "unsupported_grant_type")
	})
	t.Run("parameters in the query string only", func(t *testing.T) {
		f := codeForm(clientID, code())
		req := httptest.NewRequest(http.MethodPost, weboauth.TokenPath+"?"+f.Encode(), strings.NewReader("grant_type=authorization_code"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		wantError(t, s.do(req, ""), bad, "invalid_request")
	})
	t.Run("a body past the size limit", func(t *testing.T) {
		// Otherwise a good request: only its size is wrong.
		f := codeForm(clientID, code())
		f.Set("pad", strings.Repeat("a", 20000))
		rec := s.token(f)
		wantError(t, rec, bad, "invalid_request")
		if !strings.Contains(rec.Body.String(), "not a valid form") {
			t.Fatalf("refused for another reason: %s", rec.Body)
		}
	})
	t.Run("a body that is not a form", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, weboauth.TokenPath, strings.NewReader(`{"grant_type":"authorization_code"}`))
		req.Header.Set("Content-Type", "application/json")
		wantError(t, s.do(req, ""), bad, "invalid_request")
	})
	t.Run("an access token presented as a refresh token", func(t *testing.T) {
		toks := jsonOf(t, s.token(codeForm(clientID, code())))
		wantError(t, s.token(url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {toks["access_token"].(string)}}), bad, "invalid_grant")
	})
}

func TestRegistrationRefusals(t *testing.T) {
	s := newSite(t, stubMetadata{})
	for name, body := range map[string]map[string]any{
		"no redirect uris":    {"client_name": "x"},
		"plain http redirect": {"redirect_uris": []string{"http://app.example/cb"}},
		"javascript redirect": {"redirect_uris": []string{"javascript://x/%0Aalert(1)"}},
		"a secret method":     {"redirect_uris": []string{redirect}, "token_endpoint_auth_method": "client_secret_basic"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := s.postJSON(weboauth.RegisterPath, body)
			if rec.Code != http.StatusBadRequest || jsonOf(t, rec)["error"] == nil {
				t.Fatalf("register = %d %s", rec.Code, rec.Body)
			}
		})
	}
	req := httptest.NewRequest(http.MethodPost, weboauth.RegisterPath, strings.NewReader("client_name=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	wantError(t, s.do(req, ""), http.StatusBadRequest, "invalid_request")
	req = httptest.NewRequest(http.MethodPost, weboauth.RegisterPath, strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	wantError(t, s.do(req, ""), http.StatusBadRequest, "invalid_client_metadata")
	req = httptest.NewRequest(http.MethodPost, weboauth.RegisterPath, strings.NewReader(`{"redirect_uris":["`+redirect+`"],"pad":"`+strings.Repeat("a", 20000)+`"}`))
	req.Header.Set("Content-Type", "application/json")
	wantError(t, s.do(req, ""), http.StatusBadRequest, "invalid_client_metadata")
}

func TestRegistrationIssuesNoSecret(t *testing.T) {
	s := newSite(t, stubMetadata{})
	rec := s.postJSON(weboauth.RegisterPath, map[string]any{"client_name": "A", "redirect_uris": []string{redirect}})
	m := jsonOf(t, rec)
	var created string
	if err := s.d.QueryRow(`SELECT created_at FROM oauth_clients WHERE client_id = ?`, m["client_id"]).Scan(&created); err != nil {
		t.Fatal(err)
	}
	when, err := time.Parse("2006-01-02T15:04:05.000000000Z", created)
	if err != nil || m["client_id_issued_at"] != float64(when.Unix()) {
		t.Fatalf("client_id_issued_at = %v, stored created_at = %q (%v)", m["client_id_issued_at"], created, err)
	}
	if _, has := m["client_secret"]; has || m["token_endpoint_auth_method"] != "none" || !strings.HasPrefix(m["client_id"].(string), "gobc_") {
		t.Fatalf("registration = %v", m)
	}
}

func TestClientNameIsEscapedOnTheConsentPage(t *testing.T) {
	s := newSite(t, stubMetadata{})
	rec := s.postJSON(weboauth.RegisterPath, map[string]any{"client_name": `<script>alert(1)</script>`, "redirect_uris": []string{redirect}})
	id := jsonOf(t, rec)["client_id"].(string)
	page := s.get(weboauth.AuthorizePath+"?"+authQuery(id).Encode(), ann)
	if strings.Contains(page.Body.String(), "<script>alert(1)") || !strings.Contains(page.Body.String(), "&lt;script&gt;") {
		t.Fatalf("name not escaped:\n%s", page.Body)
	}
}

// No log line carries a code, a token, a verifier or a client secret.
func TestNoLogLineHoldsASecret(t *testing.T) {
	s := newSite(t, stubMetadata{})
	clientID := s.register()
	code := s.approve(authQuery(clientID), ann).Query().Get("code")
	toks := jsonOf(t, s.token(codeForm(clientID, code)))
	// Provoke the refusals that log.
	s.token(codeForm(clientID, code))
	f := codeForm(clientID, "goc_made-up")
	f.Set("code_verifier", "xBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	s.token(f)
	s.token(url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {toks["refresh_token"].(string)}})
	s.token(url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {toks["refresh_token"].(string)}})

	logs := s.logs.String()
	if logs == "" {
		t.Fatal("nothing was logged, so this checks nothing")
	}
	for _, secret := range []string{code, toks["access_token"].(string), toks["refresh_token"].(string), verifier, oauth.Challenge(verifier)} {
		if strings.Contains(logs, secret) {
			t.Fatalf("a log line holds %q:\n%s", secret[:8], logs)
		}
	}
}
