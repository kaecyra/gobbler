package session

import (
	"context"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/db/dbtest"
)

// 00019 adds the sessions table over an existing database, and rolling it
// back drops the table and nothing else.
func TestMigration00019AddsAndDropsSessions(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, dbtest.MigrationsBelow("00019")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO households (id, name, created_at) VALUES (1, 'Alpha', 't')`); err != nil {
		t.Fatal(err)
	}
	tableCount := func() int {
		t.Helper()
		var n int
		if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = 'sessions'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if tableCount() != 0 {
		t.Fatal("sessions exists before 00019")
	}

	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("migrate to 00019: %v", err)
	}
	s, err := New(d, lifetime)
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.Create(ctx, "ann@example.com")
	if err != nil {
		t.Fatalf("Create after the migration: %v", err)
	}
	if email, err := s.Lookup(ctx, token); err != nil || email != "ann@example.com" {
		t.Fatalf("Lookup after the migration = %q, %v", email, err)
	}

	for _, bad := range []string{
		`INSERT INTO sessions (token_hash, email, created_at, last_used_at, expires_at) VALUES (randomblob(32), 'Ann@example.com', 't', 't', 't')`,
		`INSERT INTO sessions (token_hash, email, created_at, last_used_at, expires_at) VALUES (randomblob(32), '', 't', 't', 't')`,
		`INSERT INTO sessions (token_hash, email, created_at, last_used_at, expires_at) VALUES (randomblob(16), 'ann@example.com', 't', 't', 't')`,
		`INSERT INTO sessions (token_hash, email, created_at, last_used_at, expires_at) VALUES (NULL, 'ann@example.com', 't', 't', 't')`,
	} {
		if _, err := d.ExecContext(ctx, bad); err == nil {
			t.Errorf("schema accepted %s", bad)
		}
	}

	p, err := goose.NewProvider(goose.DialectSQLite3, d, db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 18); err != nil {
		t.Fatalf("rolling back 00019: %v", err)
	}
	if tableCount() != 0 {
		t.Error("sessions survived the rollback")
	}
	var n int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM households`).Scan(&n); err != nil || n != 1 {
		t.Errorf("households after the rollback = %d, %v; want 1 kept", n, err)
	}
}
