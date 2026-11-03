package oauth_test

import (
	"context"
	"io/fs"
	"strconv"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
)

// oauthMigration is the version of the migration that adds the OAuth tables,
// found by name so the test survives the renumbering at merge.
func oauthMigration(t *testing.T) int64 {
	t.Helper()
	entries, err := fs.ReadDir(db.Migrations(), ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), "_a-oauth-server.sql"); ok {
			v, err := strconv.ParseInt(n, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			return v
		}
	}
	t.Fatal("no a-oauth-server migration")
	return 0
}

// Rolling the migration back drops the OAuth tables and keeps what was there.
func TestMigrationAddsAndDropsTheOAuthTables(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	tables := func() int {
		var n int
		err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name LIKE 'oauth_%'`).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if tables() != 4 {
		t.Fatalf("oauth tables = %d, want 4", tables())
	}
	for _, bad := range []string{
		`INSERT INTO oauth_grants (owner_email, client_id, client_name, return_host, resource, created_at) VALUES ('Ann@Example.com', 'c', 'n', 'h', 'r', 't')`,
		`INSERT INTO oauth_grants (owner_email, client_id, client_name, return_host, resource, created_at) VALUES ('', 'c', 'n', 'h', 'r', 't')`,
		`INSERT INTO oauth_tokens (grant_id, kind, token_hash, expires_at) VALUES (999, 'access', randomblob(32), 't')`,
		`INSERT INTO oauth_codes (code_hash, owner_email, client_id, client_name, redirect_uri, code_challenge, resource, created_at, expires_at) VALUES (randomblob(8), 'a@b.c', 'c', 'n', 'u', 'x', 'r', 't', 't')`,
	} {
		if _, err := d.ExecContext(ctx, bad); err == nil {
			t.Errorf("schema accepted %s", bad)
		}
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, d, db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, oauthMigration(t)-1); err != nil {
		t.Fatalf("rolling back: %v", err)
	}
	if tables() != 0 {
		t.Error("oauth tables survived the rollback")
	}
	var n int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys`).Scan(&n); err != nil {
		t.Errorf("api_keys after the rollback: %v", err)
	}
}
