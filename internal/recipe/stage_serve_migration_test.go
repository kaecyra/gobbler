package recipe_test

import (
	"context"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/db/dbtest"
)

// The stage_kind CHECK is widened by rebuilding steps. The rebuild must keep
// every step and the rows that reference it, which a cascade from the dropped
// table would otherwise delete.
func TestServeStageMigrationKeepsPopulatedSteps(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	// The schema as it stood before the serve migration.
	if err := db.Migrate(ctx, d, dbtest.MigrationsBelow("99999")); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO households (id, name, created_at) VALUES (1, 'Home', 't')`,
		`INSERT INTO recipes (id, household_id, name, created_at, created_by, updated_at, updated_by) VALUES (7, 1, 'Bread', 't', 'a', 't', 'a')`,
		`INSERT INTO components (id, household_id, recipe_id, position) VALUES (3, 1, 7, 0)`,
		`INSERT INTO steps (id, household_id, recipe_id, component_id, position, step_key, text, duration_ns, passive, stage_kind, title) VALUES (5, 1, 7, 3, 0, 'a', 'Bake.', 60, 1, 'bake', 'Bake')`,
		`INSERT INTO steps (id, household_id, recipe_id, component_id, position, step_key, text) VALUES (6, 1, 7, 3, 1, 'b', 'Serve.')`,
		`INSERT INTO step_dependencies (household_id, step_id, position, depends_on_step_id) VALUES (1, 6, 0, 5)`,
		`INSERT INTO equipment (id, name) VALUES (900, 'zz serve pan')`,
		`INSERT INTO recipe_equipment (id, household_id, recipe_id, position, equipment_id) VALUES (11, 1, 7, 0, 900)`,
		`INSERT INTO recipe_equipment_steps (household_id, recipe_equipment_id, position, step_id) VALUES (1, 11, 0, 5)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	if _, err := d.Exec(`UPDATE steps SET stage_kind = 'serve' WHERE id = 6`); err == nil {
		t.Fatal("serve was accepted before the migration widened the CHECK")
	}

	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("migrate over a populated database: %v", err)
	}

	var kind, title string
	var passive int
	if err := d.QueryRow(`SELECT stage_kind, title, passive FROM steps WHERE id = 5`).Scan(&kind, &title, &passive); err != nil {
		t.Fatal(err)
	}
	if kind != "bake" || title != "Bake" || passive != 1 {
		t.Errorf("step 5 = kind %q title %q passive %d, want bake, Bake, 1", kind, title, passive)
	}
	for table, want := range map[string]int{"steps": 2, "step_dependencies": 1, "recipe_equipment_steps": 1} {
		var n int
		if err := d.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Errorf("%s has %d rows after the migration, want %d", table, n, want)
		}
	}
	if _, err := d.Exec(`UPDATE steps SET stage_kind = 'serve' WHERE id = 6`); err != nil {
		t.Errorf("serve rejected after the migration: %v", err)
	}
	if _, err := d.Exec(`UPDATE steps SET stage_kind = 'plate' WHERE id = 6`); err == nil {
		t.Error("an unknown kind was accepted: the CHECK is gone")
	}
	var fkViolations int
	if err := d.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&fkViolations); err != nil || fkViolations != 0 {
		t.Errorf("foreign_key_check = %d violations, err %v", fkViolations, err)
	}
}
