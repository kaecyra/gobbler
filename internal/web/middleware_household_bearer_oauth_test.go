package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/mcp"
	"github.com/kaecyra/gobbler/internal/oauth"
)

// metadataChallenge is the WWW-Authenticate of every 401 from /mcp under
// prodConfig: Bearer, pointing at the Protected Resource Metadata (RFC 9728).
const metadataChallenge = `Bearer resource_metadata="https://gobbler.test/.well-known/oauth-protected-resource/mcp"`

const testVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

// token approves and exchanges a grant for email, bound to resource, and
// returns its access token.
func (s *bearerSite) token(t *testing.T, email, resource string) string {
	t.Helper()
	ctx := context.Background()
	store := oauth.New(s.db)
	const redirect = "https://app.example/cb"
	code, err := store.IssueCode(ctx, oauth.AuthRequest{
		Owner: email, ClientID: "gobc_test", ClientName: "Test", RedirectURI: redirect,
		CodeChallenge: oauth.Challenge(testVerifier), Resource: resource,
	})
	if err != nil {
		t.Fatal(err)
	}
	toks, err := store.ExchangeCode(ctx, oauth.ExchangeRequest{Code: code, ClientID: "gobc_test", RedirectURI: redirect, Verifier: testVerifier, Resource: resource})
	if err != nil {
		t.Fatal(err)
	}
	return toks.Access
}

func TestAnOAuthTokenOpensMCPForItsOwnersHousehold(t *testing.T) {
	s := newBearerSite(t)
	s.member(t, "Smiths", "ann@example.com")
	tok := s.token(t, "ann@example.com", "https://gobbler.test/mcp")
	if rec := s.mcp(tok, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"serverInfo"`) {
		t.Errorf("initialize with a token = %d %s", rec.Code, rec.Body.String())
	}
}

func TestAnOAuthTokenForAnotherAudienceIsRefused(t *testing.T) {
	s := newBearerSite(t)
	s.member(t, "Smiths", "ann@example.com")
	tok := s.token(t, "ann@example.com", "https://elsewhere.test/mcp")
	rec := s.mcp(tok, nil)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") != metadataChallenge {
		t.Errorf("= %d %q, want 401 %s", rec.Code, rec.Header().Get("WWW-Authenticate"), metadataChallenge)
	}
}

func TestAnOAuthTokenOfANonMemberIsForbidden(t *testing.T) {
	s := newBearerSite(t)
	s.member(t, "Smiths", "ann@example.com")
	tok := s.token(t, "nobody@example.com", "https://gobbler.test/mcp")
	if rec := s.mcp(tok, nil); rec.Code != http.StatusForbidden {
		t.Errorf("= %d, want 403", rec.Code)
	}
}

func TestAnOAuthTokenDoesNotAuthenticateAnHTMLPage(t *testing.T) {
	s := newBearerSite(t)
	s.member(t, "Smiths", "ann@example.com")
	tok := s.token(t, "ann@example.com", "https://gobbler.test/mcp")
	for _, p := range []string{"/recipes", "/oauth/authorize?x=1"} {
		r := httptest.NewRequest(http.MethodGet, p, nil)
		r.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		s.r.ServeHTTP(rec, r)
		if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
			t.Errorf("GET %s with only a token = %d %q, want the sign-in redirect", p, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// With no public base URL only the development bypass knows the origin, and
// takes it from the request; otherwise no origin is known and none is claimed.
func TestSiteOf(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/mcp", nil)
	cases := map[string]struct {
		cfg  config.Config
		want string
	}{
		"configured": {config.Config{OIDC: config.OIDC{PublicBaseURL: "https://gobbler.test/"}}, "https://gobbler.test"},
		"dev bypass": {config.Config{Server: config.Server{DevAuthBypass: true}}, "http://127.0.0.1:8080"},
		"unknown":    {config.Config{}, ""},
	}
	for name, c := range cases {
		got := siteOf(c.cfg)(req)
		if got.Origin != c.want || (c.want == "") != (got.MetadataURL == "") {
			t.Errorf("%s: %+v, want origin %q", name, got, c.want)
		}
		if c.want != "" && got.MetadataURL != c.want+"/.well-known/oauth-protected-resource/mcp" {
			t.Errorf("%s: metadata URL %q", name, got.MetadataURL)
		}
	}
}

// The fallback of admitBearer, reached only if the bearer layer is unwired,
// refuses with the same challenge, and so fails closed.
func TestAdmitBearerWithoutACredentialIs401WithTheMetadata(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	r = r.WithContext(mcp.WithSite(r.Context(), siteOf(prodConfig())(r)))
	rec := httptest.NewRecorder()
	admitBearer(nil, config.Operator{}, http.NotFoundHandler(), rec, r)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") != metadataChallenge {
		t.Errorf("= %d %q, want 401 %s", rec.Code, rec.Header().Get("WWW-Authenticate"), metadataChallenge)
	}
}
