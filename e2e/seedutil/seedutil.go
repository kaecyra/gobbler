// Package seedutil is the setup every e2e seed program shares: open and
// migrate the data directory, seed the dev household, and return a context
// carrying the dev identity and that household. It is test tooling (ADR-00013).
// The seeds write through the real store, which requires a verified identity
// and scopes every call to the household on the context (ADR-00018); the
// server resolves the same household for the dev identity, so what a seed
// writes is what the spec's browser sees.
package seedutil

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
)

// householdName is the dev household's name. It must equal the name the dev
// bypass seeds (devSeeded in internal/web); Seed does nothing for an email
// that already belongs to a household, so a mismatch would not fail, only
// drift.
const householdName = "Dev household"

// Open opens and migrates the database under dir, seeds the dev household if
// it is not there, and returns a context carrying the dev identity and that
// household. The returned func closes the database, reporting a failure on
// stderr; call it once, when the seed is done.
func Open(dir string) (context.Context, *sql.DB, func(), error) {
	cfg := config.Config{Server: config.Server{ListenAddr: "127.0.0.1:0", DataDir: dir, DevAuthBypass: true}}
	ctx, err := identityContext(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	d, err := db.Open(ctx, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	closeDB := func() {
		if err := d.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "seed: close database:", err)
		}
	}
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		closeDB()
		return nil, nil, nil, err
	}
	ctx, err = withHousehold(ctx, d)
	if err != nil {
		closeDB()
		return nil, nil, nil, err
	}
	return ctx, d, closeDB, nil
}

// withHousehold seeds the dev household in d, if it is not there, and returns
// ctx carrying it.
func withHousehold(ctx context.Context, d *sql.DB) (context.Context, error) {
	hs := household.New(d)
	if err := hs.Seed(ctx, householdName, auth.DevEmail); err != nil {
		return nil, fmt.Errorf("seed dev household: %w", err)
	}
	h, err := hs.ForEmail(ctx, auth.DevEmail)
	if err != nil {
		return nil, fmt.Errorf("find dev household: %w", err)
	}
	return household.WithContext(ctx, h), nil
}

// identityContext returns a context carrying the verified identity the store
// requires on writes. Only the auth middleware can mint one, so this runs a
// request through it in dev-bypass mode.
func identityContext(cfg config.Config) (context.Context, error) {
	a, err := auth.New(cfg)
	if err != nil {
		return nil, err
	}
	var got context.Context
	a.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Context() })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if got == nil {
		return nil, fmt.Errorf("auth middleware did not pass the request on")
	}
	return got, nil
}
