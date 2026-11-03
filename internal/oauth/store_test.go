package oauth_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/oauth"
)

func wantCode(t *testing.T, err error, want *oauth.Error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want code %s", err, want.Code)
	}
}

func TestFullFlowIssuesTokensThatVerifyAndRefreshRotates(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	toks := f.connect("Ann@Example.com")
	if !strings.HasPrefix(toks.Access, oauth.AccessTokenPrefix) || toks.ExpiresIn != oauth.AccessLifetime {
		t.Fatalf("tokens = %+v", toks)
	}
	p, err := f.store.Verify(ctx, toks.Access, resource)
	if err != nil || p.Email != "ann@example.com" || p.ClientID != "gobc_test" {
		t.Fatalf("Verify = %+v, %v", p, err)
	}

	next, err := f.store.Refresh(ctx, oauth.RefreshRequest{Token: toks.Refresh, ClientID: "gobc_test", Resource: resource})
	if err != nil {
		t.Fatal(err)
	}
	if next.Access == toks.Access || next.Refresh == toks.Refresh {
		t.Fatal("refresh did not rotate the tokens")
	}
	if _, err := f.store.Verify(ctx, next.Access, resource); err != nil {
		t.Fatalf("new access token: %v", err)
	}
	// The old access token is still good until it expires: only the refresh
	// token rotates.
	if _, err := f.store.Verify(ctx, toks.Access, resource); err != nil {
		t.Fatalf("old access token: %v", err)
	}
}

func TestSecretsAreStoredOnlyAsHashes(t *testing.T) {
	f := newFixture(t)
	code := f.approve("ann@example.com")
	toks, err := f.store.ExchangeCode(context.Background(), exchange(code))
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"oauth_codes", "oauth_tokens", "oauth_grants", "oauth_clients"} {
		rows, err := f.d.Query(`SELECT * FROM ` + table)
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			for _, v := range vals {
				var s string
				switch x := v.(type) {
				case string:
					s = x
				case []byte:
					s = string(x)
				}
				for _, secret := range []string{code, toks.Access, toks.Refresh, verifier} {
					if s != "" && strings.Contains(s, secret) {
						t.Fatalf("%s holds a secret in plain text", table)
					}
				}
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
	}
}

func TestExchangeRefusesAWrongOrMissingPKCEVerifier(t *testing.T) {
	for name, v := range map[string]string{
		"wrong":     "xBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
		"too short": "short",
		"missing":   "",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			req := exchange(f.approve("ann@example.com"))
			req.Verifier = v
			_, err := f.store.ExchangeCode(context.Background(), req)
			if v == "" {
				wantCode(t, err, oauth.ErrInvalidRequest)
			} else {
				wantCode(t, err, oauth.ErrInvalidGrant)
			}
			if f.count("oauth_grants") != 0 {
				t.Fatal("a grant was made")
			}
		})
	}
}

func TestWrongVerifierSpendsTheCode(t *testing.T) {
	f := newFixture(t)
	req := exchange(f.approve("ann@example.com"))
	good := req.Verifier
	req.Verifier = "xBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	if _, err := f.store.ExchangeCode(context.Background(), req); err == nil {
		t.Fatal("wrong verifier accepted")
	}
	req.Verifier = good
	_, err := f.store.ExchangeCode(context.Background(), req)
	wantCode(t, err, oauth.ErrInvalidGrant)
}

func TestExchangeRefusesAMissingResource(t *testing.T) {
	f := newFixture(t)
	req := exchange(f.approve("ann@example.com"))
	req.Resource = ""
	_, err := f.store.ExchangeCode(context.Background(), req)
	wantCode(t, err, oauth.ErrInvalidRequest)
}

func TestExchangeRefusesAWrongResource(t *testing.T) {
	f := newFixture(t)
	req := exchange(f.approve("ann@example.com"))
	req.Resource = "https://other.example/mcp"
	_, err := f.store.ExchangeCode(context.Background(), req)
	wantCode(t, err, oauth.ErrInvalidTarget)
	if f.count("oauth_grants") != 0 {
		t.Fatal("a grant was made")
	}
}

func TestExchangeRefusesAnotherClientOrRedirectURI(t *testing.T) {
	f := newFixture(t)
	req := exchange(f.approve("ann@example.com"))
	req.ClientID = "gobc_other"
	_, err := f.store.ExchangeCode(context.Background(), req)
	wantCode(t, err, oauth.ErrInvalidGrant)

	req = exchange(f.approve("ann@example.com"))
	req.RedirectURI = "https://evil.example/callback"
	_, err = f.store.ExchangeCode(context.Background(), req)
	wantCode(t, err, oauth.ErrInvalidGrant)
}

func TestExchangeRefusesAnExpiredAndAnUnknownCode(t *testing.T) {
	f := newFixture(t)
	code := f.approve("ann@example.com")
	f.now = f.now.Add(oauth.CodeLifetime + time.Second)
	_, err := f.store.ExchangeCode(context.Background(), exchange(code))
	wantCode(t, err, oauth.ErrInvalidGrant)
	_, err = f.store.ExchangeCode(context.Background(), exchange("goc_nonsense"))
	wantCode(t, err, oauth.ErrInvalidGrant)
}

func TestReusedCodeRevokesTheGrantItMade(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	code := f.approve("ann@example.com")
	toks, err := f.store.ExchangeCode(ctx, exchange(code))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.store.ExchangeCode(ctx, exchange(code))
	wantCode(t, err, oauth.ErrInvalidGrant)
	if _, err := f.store.Verify(ctx, toks.Access, resource); !errors.Is(err, oauth.ErrInvalidToken) {
		t.Fatalf("access token after code reuse: %v", err)
	}
	if f.count("oauth_grants") != 0 || f.count("oauth_tokens") != 0 {
		t.Fatal("the grant or its tokens survived")
	}
}

func TestVerifyRefusesATokenForAnotherAudience(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	toks := f.connect("ann@example.com")
	_, err := f.store.Verify(ctx, toks.Access, "https://other.example/mcp")
	if !errors.Is(err, oauth.ErrInvalidToken) {
		t.Fatalf("token checked for another server: %v", err)
	}
	if _, err := f.store.Verify(ctx, toks.Access, ""); !errors.Is(err, oauth.ErrInvalidToken) {
		t.Fatalf("token checked for no audience: %v", err)
	}
	// A grant that was bound to another server's resource is not good here.
	code, err := f.store.IssueCode(ctx, oauth.AuthRequest{
		Owner: "ann@example.com", ClientID: "gobc_test", RedirectURI: redirect,
		CodeChallenge: oauth.Challenge(verifier), Resource: "https://other.example/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := exchange(code)
	req.Resource = "https://other.example/mcp"
	foreign, err := f.store.ExchangeCode(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Verify(ctx, foreign.Access, resource); !errors.Is(err, oauth.ErrInvalidToken) {
		t.Fatalf("token bound to another server accepted here: %v", err)
	}
}

func TestVerifyRefusesUnknownExpiredAndNonAccessTokens(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	toks := f.connect("ann@example.com")
	for name, tok := range map[string]string{
		"unknown": oauth.AccessTokenPrefix + "nonsense", "api key": "gob_abc", "refresh token": toks.Refresh, "empty": "",
	} {
		if _, err := f.store.Verify(ctx, tok, resource); !errors.Is(err, oauth.ErrInvalidToken) {
			t.Errorf("%s: %v", name, err)
		}
	}
	f.now = f.now.Add(oauth.AccessLifetime + time.Second)
	if _, err := f.store.Verify(ctx, toks.Access, resource); !errors.Is(err, oauth.ErrInvalidToken) {
		t.Fatalf("expired access token: %v", err)
	}
}

func TestReusedRefreshTokenRevokesTheGrant(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	toks := f.connect("ann@example.com")
	next, err := f.store.Refresh(ctx, oauth.RefreshRequest{Token: toks.Refresh, ClientID: "gobc_test"})
	if err != nil {
		t.Fatal(err)
	}
	// The retired token comes back: two parties hold it.
	_, err = f.store.Refresh(ctx, oauth.RefreshRequest{Token: toks.Refresh, ClientID: "gobc_test"})
	wantCode(t, err, oauth.ErrInvalidGrant)
	for _, a := range []string{toks.Access, next.Access} {
		if _, err := f.store.Verify(ctx, a, resource); !errors.Is(err, oauth.ErrInvalidToken) {
			t.Fatalf("access token after refresh reuse: %v", err)
		}
	}
	// The current refresh token died with the grant.
	_, err = f.store.Refresh(ctx, oauth.RefreshRequest{Token: next.Refresh, ClientID: "gobc_test"})
	wantCode(t, err, oauth.ErrInvalidGrant)
	if f.count("oauth_grants") != 0 {
		t.Fatal("the grant survived")
	}
}

func TestRefreshRefusals(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	toks := f.connect("ann@example.com")

	_, err := f.store.Refresh(ctx, oauth.RefreshRequest{Token: toks.Refresh, ClientID: "gobc_other"})
	wantCode(t, err, oauth.ErrInvalidGrant)
	_, err = f.store.Refresh(ctx, oauth.RefreshRequest{Token: toks.Refresh, ClientID: "gobc_test", Resource: "https://other.example/mcp"})
	wantCode(t, err, oauth.ErrInvalidTarget)
	_, err = f.store.Refresh(ctx, oauth.RefreshRequest{Token: "gor_nonsense", ClientID: "gobc_test"})
	wantCode(t, err, oauth.ErrInvalidGrant)
	_, err = f.store.Refresh(ctx, oauth.RefreshRequest{Token: toks.Access, ClientID: "gobc_test"})
	wantCode(t, err, oauth.ErrInvalidGrant)
	_, err = f.store.Refresh(ctx, oauth.RefreshRequest{ClientID: "gobc_test"})
	wantCode(t, err, oauth.ErrInvalidRequest)

	f.now = f.now.Add(oauth.RefreshLifetime + time.Second)
	_, err = f.store.Refresh(ctx, oauth.RefreshRequest{Token: toks.Refresh, ClientID: "gobc_test"})
	wantCode(t, err, oauth.ErrInvalidGrant)
}

func TestIssueCodeRefusesABadChallenge(t *testing.T) {
	f := newFixture(t)
	_, err := f.store.IssueCode(context.Background(), oauth.AuthRequest{
		Owner: "ann@example.com", ClientID: "c", RedirectURI: redirect, CodeChallenge: "plain-text-challenge", Resource: resource,
	})
	wantCode(t, err, oauth.ErrInvalidRequest)
}

func TestGrantsAreListedAndRevokedByTheirOwnerOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	toks := f.connect("ann@example.com")
	f.connect("bob@example.com")

	grants, err := f.store.ListGrants(ctx, "Ann@Example.com")
	if err != nil || len(grants) != 1 {
		t.Fatalf("ListGrants = %+v, %v", grants, err)
	}
	g := grants[0]
	if g.ClientName != "Test client" || g.ReturnHost != "app.example" || g.ClientID != "gobc_test" {
		t.Fatalf("grant = %+v", g)
	}
	if err := f.store.RevokeGrant(ctx, "bob@example.com", g.ID); !errors.Is(err, oauth.ErrNotFound) {
		t.Fatalf("revoking another person's grant: %v", err)
	}
	if _, err := f.store.Verify(ctx, toks.Access, resource); err != nil {
		t.Fatalf("grant was revoked by someone else: %v", err)
	}
	if err := f.store.RevokeGrant(ctx, "ann@example.com", g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Verify(ctx, toks.Access, resource); !errors.Is(err, oauth.ErrInvalidToken) {
		t.Fatalf("access token after revoke: %v", err)
	}
	if _, err := f.store.Refresh(ctx, oauth.RefreshRequest{Token: toks.Refresh, ClientID: "gobc_test"}); !errors.Is(err, oauth.ErrInvalidGrant) {
		t.Fatalf("refresh after revoke: %v", err)
	}
	if err := f.store.RevokeGrant(ctx, "ann@example.com", g.ID); !errors.Is(err, oauth.ErrNotFound) {
		t.Fatalf("revoking twice: %v", err)
	}
}

func TestRemovingAMemberRevokesTheirGrantsAndAnUnexchangedCode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	h := f.joined("ann@example.com", "bob@example.com")
	bobToks := f.connect("bob@example.com")
	annToks := f.connect("ann@example.com")
	pending := f.approve("bob@example.com")

	hs := newHouseholds(f)
	if err := hs.Remove(ctx, h.ID, "ann@example.com", "bob@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Verify(ctx, bobToks.Access, resource); !errors.Is(err, oauth.ErrInvalidToken) {
		t.Fatalf("removed member's token: %v", err)
	}
	if _, err := f.store.ExchangeCode(ctx, exchange(pending)); !errors.Is(err, oauth.ErrInvalidGrant) {
		t.Fatalf("removed member's pending code: %v", err)
	}
	if _, err := f.store.Verify(ctx, annToks.Access, resource); err != nil {
		t.Fatalf("someone else's token was revoked: %v", err)
	}
}

func TestLeavingRevokesTheirGrants(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.joined("ann@example.com", "bob@example.com")
	toks := f.connect("bob@example.com")
	if err := newHouseholds(f).Leave(ctx, "bob@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Verify(ctx, toks.Access, resource); !errors.Is(err, oauth.ErrInvalidToken) {
		t.Fatalf("token after leaving: %v", err)
	}
}

func TestGrantsAreRevokedInTheTransactionThatRemovesTheMember(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	h := f.joined("ann@example.com", "bob@example.com")
	toks := f.connect("bob@example.com")

	hs := newHouseholds(f)
	// A hook that fails after ours rolls the whole removal back, grant included.
	hs.OnMemberLeave(func(context.Context, *sql.Tx, string) error { return errors.New("later hook failed") })
	if err := hs.Remove(ctx, h.ID, "ann@example.com", "bob@example.com"); err == nil {
		t.Fatal("removal succeeded despite the failing hook")
	}
	if _, err := f.store.Verify(ctx, toks.Access, resource); err != nil {
		t.Fatalf("grant was revoked outside the removal's transaction: %v", err)
	}
}

func TestRevokeForMemberRefusesAnEmptyEmail(t *testing.T) {
	f := newFixture(t)
	tx, err := f.d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := f.store.RevokeForMember(context.Background(), tx, " "); err == nil {
		t.Fatal("empty email accepted")
	}
}
