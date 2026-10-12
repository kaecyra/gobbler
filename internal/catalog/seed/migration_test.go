package seed_test

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
)

// The owner's real database may already hold units the old save flow created
// unreviewed, under the very names migration 00003 adds. The migration must
// adopt them, not abort start-up on the UNIQUE name.
func TestMigration00003AdoptsUnitsTheOldSaveFlowCreated(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })

	// Migrate to 00002 first, as the database was before this change.
	before := fstest.MapFS{}
	for _, name := range []string{"00001_catalog.sql", "00002_recipes.sql"} {
		b, err := fs.ReadFile(db.Migrations(), name)
		if err != nil {
			t.Fatal(err)
		}
		before[name] = &fstest.MapFile{Data: b}
	}
	if err := db.Migrate(ctx, d, before); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO units (name, system, dimension, factor, reviewed) VALUES ('bag', 'none', 'other', NULL, 0)`,
		`INSERT INTO units (name, system, dimension, factor, reviewed) VALUES ('box', 'none', 'other', NULL, 0)`,
		`INSERT INTO unit_aliases (alias, unit_id) VALUES ('bags', (SELECT id FROM units WHERE name = 'bag'))`,
	} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("migrating a database that already holds unreviewed bag and box: %v", err)
	}
	for _, name := range []string{"bag", "box"} {
		var reviewed, n int
		if err := d.QueryRowContext(ctx, `SELECT reviewed, (SELECT count(*) FROM units WHERE name = ?) FROM units WHERE name = ?`, name, name).Scan(&reviewed, &n); err != nil {
			t.Fatal(err)
		}
		if reviewed != 1 || n != 1 {
			t.Errorf("unit %q: reviewed = %d, rows = %d, want reviewed once", name, reviewed, n)
		}
	}
	for alias, unit := range map[string]string{"bags": "bag", "boxes": "box", "c": "cup", "gr": "g"} {
		var got string
		err := d.QueryRowContext(ctx, `SELECT u.name FROM unit_aliases a JOIN units u ON u.id = a.unit_id WHERE a.alias = ?`, alias).Scan(&got)
		if err != nil || got != unit {
			t.Errorf("alias %q -> %q (err %v), want %q", alias, got, err, unit)
		}
	}
}
