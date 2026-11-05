package store

import (
	"context"
	"testing"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/db/dbtest"
)

// The revisions CHECK is widened by rebuilding the table. The rebuild must keep
// every revision and the ingredient index that references it, which a cascade
// from the dropped table would otherwise delete.
func TestProposalRevisionMigrationKeepsPopulatedRevisions(t *testing.T) {
	ctx := context.Background()
	d := openRaw(t)
	if err := db.Migrate(ctx, d, dbtest.MigrationsBelow("00029")); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO households (id, name, created_at) VALUES (1, 'Home', 't')`,
		`INSERT INTO recipes (id, household_id, name, created_at, created_by, updated_at, updated_by) VALUES (7, 1, 'Bread', 't', 'a', 't', 'a')`,
		`INSERT INTO ingredients (id, name, aisle) VALUES (901, 'zz revision salt', 'test')`,
		`INSERT INTO revisions (id, household_id, recipe_id, version, document, change_kind, created_at, created_by) VALUES (9, 1, 7, 1, '{}', 'edit', 't', 'a')`,
		`INSERT INTO revisions (id, household_id, recipe_id, version, document, change_kind, restored_version, created_at, created_by) VALUES (10, 1, 7, 2, '{}', 'restore', 1, 't', 'a')`,
		`INSERT INTO revision_ingredients (household_id, revision_id, ingredient_id) VALUES (1, 9, 901)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := d.Exec(`UPDATE revisions SET change_kind = 'proposal' WHERE id = 9`); err == nil {
		t.Fatal("kind proposal was accepted before the migration widened the CHECK")
	}

	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("migrate over a populated database: %v", err)
	}

	for table, want := range map[string]int{"revisions": 2, "revision_ingredients": 1} {
		if n := count(t, d, `SELECT count(*) FROM `+table); n != want {
			t.Errorf("%s has %d rows after the migration, want %d", table, n, want)
		}
	}
	var restored int
	if err := d.QueryRow(`SELECT restored_version FROM revisions WHERE id = 10`).Scan(&restored); err != nil || restored != 1 {
		t.Errorf("restored_version = %d (%v), want 1", restored, err)
	}
	// The kind and the reference travel together.
	if _, err := d.Exec(`UPDATE revisions SET change_kind = 'proposal' WHERE id = 9`); err == nil {
		t.Error("a proposal revision with no proposal was accepted")
	}
}
