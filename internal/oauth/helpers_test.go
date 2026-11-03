package oauth_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/oauth"
)

const (
	resource = "https://gobbler.example/mcp"
	verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	redirect = "https://app.example/callback"
)

type fixture struct {
	t     *testing.T
	d     *sql.DB
	store *oauth.Store
	now   time.Time
}

// newFixture returns a migrated database and a store whose clock the test moves
// with f.now.
func newFixture(t *testing.T) *fixture {
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
	f := &fixture{t: t, d: d, store: oauth.New(d), now: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	f.store.SetClock(func() time.Time { return f.now })
	return f
}

// approve issues a code the way the consent page does.
func (f *fixture) approve(email string) string {
	f.t.Helper()
	code, err := f.store.IssueCode(context.Background(), oauth.AuthRequest{
		Owner: email, ClientID: "gobc_test", ClientName: "Test client", RedirectURI: redirect,
		CodeChallenge: oauth.Challenge(verifier), Resource: resource,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return code
}

func exchange(code string) oauth.ExchangeRequest {
	return oauth.ExchangeRequest{Code: code, ClientID: "gobc_test", RedirectURI: redirect, Verifier: verifier, Resource: resource}
}

// connect runs the whole exchange and returns the tokens.
func (f *fixture) connect(email string) oauth.Tokens {
	f.t.Helper()
	toks, err := f.store.ExchangeCode(context.Background(), exchange(f.approve(email)))
	if err != nil {
		f.t.Fatal(err)
	}
	return toks
}

func (f *fixture) count(table string) int {
	f.t.Helper()
	var n int
	if err := f.d.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// joined makes a household of first and second, with the grant hook registered
// as the router registers it.
func (f *fixture) joined(first, second string) household.Household {
	f.t.Helper()
	ctx := context.Background()
	hs := household.New(f.d)
	hs.OnMemberLeave(f.store.RevokeForMember)
	h, err := hs.Create(ctx, "House", first)
	if err != nil {
		f.t.Fatal(err)
	}
	code, err := hs.Invite(ctx, household.Invite{Kind: household.KindJoin, Email: second, HouseholdID: h.ID, IssuedBy: first})
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := hs.Accept(ctx, code, second, ""); err != nil {
		f.t.Fatal(err)
	}
	return h
}

// newHouseholds is a household store with the grant hook registered, over the
// fixture's database.
func newHouseholds(f *fixture) *household.Store {
	hs := household.New(f.d)
	hs.OnMemberLeave(f.store.RevokeForMember)
	return hs
}
