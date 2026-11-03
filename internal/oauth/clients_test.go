package oauth_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

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
	for i := 0; i < oauth.MaxClients; i++ {
		if _, err := f.d.ExecContext(ctx, `INSERT INTO oauth_clients (client_id, client_name, redirect_uris, created_at) VALUES (?, 'x', '[]', ?)`,
			fmt.Sprintf("gobc_%d", i), "2026-11-03T12:00:00.000000000Z"); err != nil {
			t.Fatal(err)
		}
	}
	_, err := f.store.RegisterClient(ctx, m)
	wantCode(t, err, oauth.ErrTemporarilyBusy)

	// A day later the unused ones are pruned to make room, but one with a grant stays.
	if _, err := f.d.ExecContext(ctx, `INSERT INTO oauth_grants (owner_email, client_id, client_name, return_host, resource, created_at)
		VALUES ('ann@example.com', 'gobc_0', 'x', 'h', 'r', 't')`); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(oauth.ClientRetention + 2)
	if _, err := f.store.RegisterClient(ctx, m); err != nil {
		t.Fatal(err)
	}
	if n := f.count("oauth_clients"); n != 2 {
		t.Fatalf("clients after prune = %d, want the granted one and the new one", n)
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
