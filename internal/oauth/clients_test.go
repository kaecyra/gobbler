package oauth_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/kaecyra/gobbler/internal/oauth"
)

func TestRegisterClientStoresAndReturnsItByID(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	c, err := f.store.RegisterClient(ctx, oauth.ClientMetadata{
		Name: "  My\x00 agent ", RedirectURIs: []string{"https://app.example/cb", "http://127.0.0.1:7777/cb"},
		AuthMethod: "none", GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(c.ID, "gobc_") || c.Name != "My agent" {
		t.Fatalf("client = %+v", c)
	}
	if c.CreatedAt.IsZero() {
		t.Fatal("a registered client has no registration time")
	}
	got, err := f.store.Client(ctx, c.ID)
	if err != nil || got.Name != "My agent" || len(got.RedirectURIs) != 2 || !got.CreatedAt.Equal(c.CreatedAt) {
		t.Fatalf("Client = %+v, %v", got, err)
	}
	if !got.AllowsRedirect("http://127.0.0.1:51234/cb") || got.AllowsRedirect("http://127.0.0.1:51234/other") || got.AllowsRedirect("https://app.example/cb2") {
		t.Fatal("redirect matching is wrong")
	}
	if _, err := f.store.Client(ctx, "gobc_nope"); !errors.Is(err, oauth.ErrUnknownClient) {
		t.Fatalf("unknown client: %v", err)
	}
}

func TestRegisterClientRefusesBadMetadata(t *testing.T) {
	f := newFixture(t)
	good := []string{"https://app.example/cb"}
	for name, tc := range map[string]struct {
		m    oauth.ClientMetadata
		want *oauth.Error
	}{
		"no redirect uris":    {oauth.ClientMetadata{}, oauth.ErrInvalidRedirectURI},
		"too many":            {oauth.ClientMetadata{RedirectURIs: make([]string, oauth.MaxRedirectURIs+1)}, oauth.ErrInvalidRedirectURI},
		"plain http":          {oauth.ClientMetadata{RedirectURIs: []string{"http://app.example/cb"}}, oauth.ErrInvalidRedirectURI},
		"fragment":            {oauth.ClientMetadata{RedirectURIs: []string{"https://app.example/cb#x"}}, oauth.ErrInvalidRedirectURI},
		"javascript":          {oauth.ClientMetadata{RedirectURIs: []string{"javascript://app/%0aalert(1)"}}, oauth.ErrInvalidRedirectURI},
		"userinfo":            {oauth.ClientMetadata{RedirectURIs: []string{"https://u:p@app.example/cb"}}, oauth.ErrInvalidRedirectURI},
		"relative":            {oauth.ClientMetadata{RedirectURIs: []string{"/cb"}}, oauth.ErrInvalidRedirectURI},
		"a secret method":     {oauth.ClientMetadata{RedirectURIs: good, AuthMethod: "client_secret_basic"}, oauth.ErrInvalidClientMetadata},
		"implicit grant":      {oauth.ClientMetadata{RedirectURIs: good, GrantTypes: []string{"implicit"}}, oauth.ErrInvalidClientMetadata},
		"token response type": {oauth.ClientMetadata{RedirectURIs: good, ResponseTypes: []string{"token"}}, oauth.ErrInvalidClientMetadata},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.store.RegisterClient(context.Background(), tc.m)
			wantCode(t, err, tc.want)
		})
	}
	if f.count("oauth_clients") != 0 {
		t.Fatal("a refused client was stored")
	}
}

func TestRegistrationIsCappedAndPrunesClientsThatNeverGotAGrant(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := oauth.ClientMetadata{RedirectURIs: []string{"https://app.example/cb"}}
	f.fillClients(t, oauth.MaxClients)
	_, err := f.store.RegisterClient(ctx, m)
	wantCode(t, err, oauth.ErrTemporarilyBusy)

	// A day later the unused ones are pruned to make room, but one with a grant stays.
	f.grant(t, "gobc_0")
	f.now = f.now.Add(oauth.ClientRetention + 2)
	if _, err := f.store.RegisterClient(ctx, m); err != nil {
		t.Fatal(err)
	}
	if n := f.count("oauth_clients"); n != 2 {
		t.Fatalf("clients after prune = %d, want the granted one and the new one", n)
	}
}

// ADR-00036: the ceiling is 10,000 clients without a grant. Clients with a
// grant neither count towards it nor are pruned.
func TestCeilingCountsOnlyClientsWithoutAGrant(t *testing.T) {
	if oauth.MaxClients != 10000 {
		t.Fatalf("MaxClients = %d, want 10000", oauth.MaxClients)
	}
	f := newFixture(t)
	ctx := context.Background()
	m := oauth.ClientMetadata{RedirectURIs: []string{"https://app.example/cb"}}
	f.fillClients(t, oauth.MaxClients)
	// Granting 50 of the full table frees 50 places: they are in use, not counted.
	for i := 0; i < 50; i++ {
		f.grant(t, fmt.Sprintf("gobc_%d", i))
	}
	for i := 0; i < 50; i++ {
		if _, err := f.store.RegisterClient(ctx, m); err != nil {
			t.Fatalf("registration %d refused though clients with a grant do not count: %v", i+1, err)
		}
	}
	_, err := f.store.RegisterClient(ctx, m)
	wantCode(t, err, oauth.ErrTemporarilyBusy)

	// Well past retention every unused client is pruned; the granted ones stay.
	f.now = f.now.Add(oauth.ClientRetention + time.Hour)
	if _, err := f.store.RegisterClient(ctx, m); err != nil {
		t.Fatal(err)
	}
	if n := f.count("oauth_clients"); n != 51 {
		t.Fatalf("clients after prune = %d, want the 50 granted and the new one", n)
	}
}

// fillClients inserts n clients registered a moment before the fixture's
// clock, named gobc_0 to gobc_<n-1>, in one transaction.
func (f *fixture) fillClients(t *testing.T, n int) {
	t.Helper()
	_, err := f.d.ExecContext(context.Background(), `WITH RECURSIVE s(i) AS (SELECT 0 UNION ALL SELECT i + 1 FROM s WHERE i + 1 < ?)
		INSERT INTO oauth_clients (client_id, client_name, redirect_uris, created_at)
		SELECT 'gobc_' || i, 'x', '[]', '2026-11-03T12:00:00.000000000Z' FROM s`, n)
	if err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) grant(t *testing.T, clientID string) {
	t.Helper()
	if _, err := f.d.ExecContext(context.Background(), `INSERT INTO oauth_grants (owner_email, client_id, client_name, return_host, resource, created_at)
		VALUES ('ann@example.com', ?, 'x', 'h', 'r', 't')`, clientID); err != nil {
		t.Fatal(err)
	}
}

func TestRedirectURIRules(t *testing.T) {
	ok := []string{
		"https://app.example/cb", "http://127.0.0.1:8080/cb", "http://localhost/cb", "http://[::1]:9/cb",
		"com.example.app:/oauth2redirect", "io.cursor.app://oauth/callback",
	}
	for _, u := range ok {
		if err := oauth.ValidateRedirectURI(u); err != nil {
			t.Errorf("%s refused: %v", u, err)
		}
	}
	bad := []string{"", "http://app.example/cb", "https:///cb", "https://app.example/cb#f", "data:text/html,x", "file:///etc/passwd", "intent://scan/x", "ms-settings://network", "tel://5555550100", "cursor://anysphere/cb", "javascript://x", "foo", "https://app.example/c b", "app://"}
	for _, u := range bad {
		if err := oauth.ValidateRedirectURI(u); !errors.Is(err, oauth.ErrInvalidRedirectURI) {
			t.Errorf("%q accepted (%v)", u, err)
		}
	}
	if !oauth.MatchRedirectURI([]string{"https://app.example/cb"}, "https://app.example/cb") ||
		oauth.MatchRedirectURI([]string{"https://app.example/cb"}, "https://app.example/cb/") ||
		oauth.MatchRedirectURI([]string{"https://app.example/cb"}, "https://app.example:444/cb") ||
		oauth.MatchRedirectURI([]string{"http://127.0.0.1/cb"}, "http://localhost:80/cb") {
		t.Fatal("MatchRedirectURI is wrong")
	}
	for uri, want := range map[string]string{
		"https://app.example:8443/cb": "app.example:8443", "io.cursor.app://oauth/cb": "io.cursor.app://oauth", "com.example.app:/redirect": "com.example.app:/redirect",
	} {
		if got := oauth.ReturnHost(uri); got != want {
			t.Errorf("ReturnHost(%q) = %q, want %q", uri, got, want)
		}
	}
}

func TestPKCE(t *testing.T) {
	if !oauth.VerifyPKCE(oauth.Challenge(verifier), verifier) {
		t.Fatal("the right verifier was refused")
	}
	if oauth.VerifyPKCE(oauth.Challenge(verifier), verifier+"x") || oauth.VerifyPKCE("", verifier) || oauth.VerifyPKCE(oauth.Challenge(verifier), "") {
		t.Fatal("a wrong verifier was accepted")
	}
	if oauth.ValidVerifier(strings.Repeat("a", 129)) || oauth.ValidVerifier(strings.Repeat("a", 42)) || oauth.ValidVerifier(strings.Repeat("a", 42)+"!") {
		t.Fatal("ValidVerifier accepts a bad verifier")
	}
	if oauth.ValidChallenge(strings.Repeat("a", 43)+"=") || oauth.ValidChallenge("short") {
		t.Fatal("ValidChallenge accepts a bad challenge")
	}
}

func TestTruncateCutsOnACharacterBoundary(t *testing.T) {
	for _, tc := range []struct {
		in   string
		n    int
		want string
	}{
		{"abcdef", 3, "abc"},
		{"abc", 3, "abc"},
		{"abc", 10, "abc"},
		{"", 5, ""},
		{"héllo", 2, "h"}, // é is two bytes, so a cut at 2 would split it
		{"héllo", 3, "hé"},
		{"日本語", 4, "日"}, // three bytes each
		{"日本語", 2, ""},
		{"abc", 0, ""},
	} {
		if got := oauth.Truncate(tc.in, tc.n); got != tc.want || !utf8.ValidString(got) {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}
