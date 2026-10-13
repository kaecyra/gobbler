package catalog

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
)

func migrationsBelow(t *testing.T, first string) fs.FS {
	t.Helper()
	older := fstest.MapFS{}
	all := db.Migrations()
	entries, err := fs.ReadDir(all, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() >= first {
			continue
		}
		body, err := fs.ReadFile(all, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		older[e.Name()] = &fstest.MapFile{Data: body}
	}
	return older
}

// The overlay migration rebuilds ingredients and ingredient_aliases while other
// tables reference them. Existing entries keep their ids and stay central,
// their aliases survive, and nothing that pointed at them is orphaned.
func TestOverlayMigrationKeepsIdsAliasesAndReferences(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, migrationsBelow(t, "00010")); err != nil {
		t.Fatalf("migrate to the household shopping tables: %v", err)
	}
	h, err := household.New(d).Create(ctx, "Old", "old@example.com")
	if err != nil {
		t.Fatal(err)
	}
	var onion int64
	if err := d.QueryRow(`SELECT id FROM ingredients WHERE name = 'onion'`).Scan(&onion); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO shopping_pantry (household_id, ingredient_id) VALUES (?, ?)`, h.ID, onion); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO ingredients (name, aisle) VALUES ('zz pre', 'x')`); err != nil {
		t.Fatal(err)
	}
	before := count(t, d, `SELECT count(*) FROM ingredient_aliases`)

	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("migrate to the overlay: %v", err)
	}
	if n := count(t, d, `SELECT count(*) FROM ingredients WHERE id = ? AND name = 'onion' AND household_id IS NULL`, onion); n != 1 {
		t.Errorf("onion kept its id and stayed central: %d rows", n)
	}
	if n := count(t, d, `SELECT count(*) FROM ingredients WHERE name = 'zz pre' AND household_id IS NULL`); n != 1 {
		t.Error("an entry added before the migration is not central afterwards")
	}
	if n := count(t, d, `SELECT count(*) FROM ingredient_aliases WHERE household_id IS NULL`); n != before {
		t.Errorf("%d central aliases after the migration, want the %d before", n, before)
	}
	if n := count(t, d, `SELECT count(*) FROM shopping_pantry WHERE ingredient_id = ?`, onion); n != 1 {
		t.Errorf("pantry rows on onion = %d, want 1", n)
	}
	if n := count(t, d, `SELECT count(*) FROM pragma_foreign_key_check`); n != 0 {
		t.Errorf("%d foreign key violations after the migration", n)
	}

	// Rolling back returns to one global catalog, with everything still there.
	p, err := goose.NewProvider(goose.DialectSQLite3, d, db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 9); err != nil {
		t.Fatalf("roll back the overlay: %v", err)
	}
	if n := count(t, d, `SELECT count(*) FROM ingredient_aliases`); n != before {
		t.Errorf("%d aliases after rolling back, want %d", n, before)
	}
	if n := count(t, d, `SELECT count(*) FROM shopping_pantry WHERE ingredient_id = ?`, onion); n != 1 {
		t.Errorf("pantry rows on onion after rolling back = %d, want 1", n)
	}
	if n := count(t, d, `SELECT count(*) FROM pragma_foreign_key_check`); n != 0 {
		t.Errorf("%d foreign key violations after rolling back", n)
	}
}
