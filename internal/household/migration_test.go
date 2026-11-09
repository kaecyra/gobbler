package household

import (
	"context"
	"errors"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/db/dbtest"
)

// Invitations issued before 00018 are given codes by the migration, keep
// their ids and every other column, and can be accepted by the new code
// (ADR-00028). Rolling back drops the column and keeps the rows.
func TestMigration00018GivesExistingInvitationsCodes(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, dbtest.MigrationsBelow("00018")); err != nil {
		t.Fatal(err)
	}
	const future = "2999-01-01T00:00:00.000000000Z"
	for _, q := range []string{
		`INSERT INTO households (id, name, created_at) VALUES (1, 'Alpha', '` + future + `')`,
		`INSERT INTO household_members (household_id, email, joined_at) VALUES (1, 'ann@example.com', 't')`,
		`INSERT INTO invitations (id, kind, email, household_id, issued_by, created_at, expires_at) VALUES (4, 'join', 'cy@example.com', 1, 'ann@example.com', 't', '` + future + `')`,
		`INSERT INTO invitations (id, kind, email, issued_by, created_at, expires_at, revoked_at) VALUES (9, 'household', 'dee@example.com', 'op@example.com', 't', '` + future + `', 'r')`,
	} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("migrate over invitation rows: %v", err)
	}

	codes := map[int64]string{}
	rows, err := d.QueryContext(ctx, `SELECT id, code FROM invitations ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		var code string
		if err := rows.Scan(&id, &code); err != nil {
			t.Fatal(err)
		}
		if !ValidCode(code) {
			t.Errorf("invitation %d got code %q, not %d base62 characters", id, code, CodeLength)
		}
		codes[id] = code
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(codes) != 2 || codes[4] == "" || codes[9] == "" || codes[4] == codes[9] {
		t.Fatalf("codes after the migration = %v, want two distinct codes for ids 4 and 9", codes)
	}
	var revoked int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM invitations WHERE id = 9 AND revoked_at = 'r' AND kind = 'household' AND household_id IS NULL`).Scan(&revoked); err != nil || revoked != 1 {
		t.Errorf("invitation 9 lost its columns: %d, %v", revoked, err)
	}

	s := New(d)
	if h, err := s.Accept(ctx, codes[4], "cy@example.com", ""); err != nil || h.ID != 1 {
		t.Fatalf("Accept(migrated code) = %+v, %v; want household 1", h, err)
	}
	if _, err := s.Invite(ctx, Invite{Kind: KindHousehold, Email: "eve@example.com", IssuedBy: "op@example.com"}); err != nil {
		t.Fatalf("Invite after the migration: %v", err)
	}

	p, err := goose.NewProvider(goose.DialectSQLite3, d, db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 17); err != nil {
		t.Fatalf("rolling back 00018: %v", err)
	}
	var n int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('invitations') WHERE name = 'code'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("invitations.code survived the rollback: %d, %v", n, err)
	}
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM invitations`).Scan(&n); err != nil || n != 3 {
		t.Errorf("invitations after the rollback = %d, %v; want 3 kept", n, err)
	}
}

// A household that exists before 00035 gets an empty house style and keeps
// its other columns (ADR-00048). Rolling back drops the column and keeps the
// household.
func TestMigration00035GivesExistingHouseholdsAnEmptyHouseStyle(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, dbtest.MigrationsBelow("00035")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO households (id, name, created_at) VALUES (1, 'Alpha', '2026-01-01T00:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("migrate over a household: %v", err)
	}
	s := New(d)
	if got, err := s.HouseStyle(ctx, 1); err != nil || got != "" {
		t.Fatalf("HouseStyle after the migration = %q, %v, want empty", got, err)
	}
	if err := s.SetHouseStyle(ctx, 1, "Metric only."); err != nil {
		t.Fatal(err)
	}
	var name, created string
	if err := d.QueryRowContext(ctx, `SELECT name, created_at FROM households WHERE id = 1`).Scan(&name, &created); err != nil || name != "Alpha" || created != "2026-01-01T00:00:00.000000000Z" {
		t.Fatalf("household after a save = %q, %q, %v", name, created, err)
	}

	p, err := goose.NewProvider(goose.DialectSQLite3, d, db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 34); err != nil {
		t.Fatalf("rolling back 00035: %v", err)
	}
	var n int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('households') WHERE name = 'house_style'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("households.house_style survived the rollback: %d, %v", n, err)
	}
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM households WHERE id = 1 AND name = 'Alpha'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("household after the rollback = %d, %v; want it kept", n, err)
	}
}

func TestTheHouseStyleOfAHouseholdThatDoesNotExistIsNotFound(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	s := New(d)
	if _, err := s.HouseStyle(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("HouseStyle(99) = %v, want ErrNotFound", err)
	}
	if err := s.SetHouseStyle(ctx, 99, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetHouseStyle(99) = %v, want ErrNotFound", err)
	}
}
