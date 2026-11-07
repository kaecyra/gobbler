package db_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/db/dbtest"
)

// schemaObjects lists every table, index and trigger by type, name and table,
// so a rebuild that drops one is caught. The SQL text is left out: the rename
// rewrites it.
func schemaObjects(t *testing.T, d queryer) string {
	t.Helper()
	rows, err := d.Query(`SELECT type || ' ' || name || ' on ' || tbl_name FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type, name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(out, "\n")
}

// Migration 00034 widens steps.stage_kind's CHECK to take mix by rebuilding
// steps and the two tables that reference it, which a cascade from the dropped
// table would otherwise empty. Every row of every table, and every table,
// index and trigger, must come out of Up, Down and Up again as it went in,
// apart from Down sending mix back to unset. mix must be accepted after Up and
// refused after Down.
func TestStageMixMigrationChangesOnlyStageKind(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, dbtest.MigrationsBelow("00034")); err != nil {
		t.Fatalf("migrate to before 00034: %v", err)
	}
	seedRecipes(t, d)
	// seedRecipes gives recipes 3 and 5 one step each (ids 30 and 50); a second
	// step on recipe 3 lets the step tables' own references be seeded too.
	for _, q := range []string{
		`INSERT INTO steps (id, household_id, recipe_id, component_id, position, step_key, text, stage_kind) VALUES (31, 1, 3, 30, 1, 'k3b', 'Knead.', 'prep')`,
		`INSERT INTO step_dependencies (household_id, step_id, position, depends_on_step_id) VALUES (1, 31, 0, 30)`,
		`INSERT INTO recipe_equipment_steps (household_id, recipe_equipment_id, position, step_id) VALUES (1, 30, 0, 31)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	before := snapshot(t, d)
	objects := schemaObjects(t, d)
	for table, want := range map[string]int{"steps": 3, "step_dependencies": 1, "recipe_equipment_steps": 1} {
		if n := len(before[table]); n != want {
			t.Fatalf("%d rows seeded in %s, want %d", n, table, want)
		}
	}
	if _, err := d.Exec(`UPDATE steps SET stage_kind = 'mix' WHERE id = 31`); err == nil {
		t.Fatal("mix accepted before 00034, want the old CHECK")
	}
	unchanged := func(when string, want map[string][]string) {
		t.Helper()
		sameSnapshot(t, when, snapshot(t, d), want)
		if got := schemaObjects(t, d); got != objects {
			t.Errorf("%s: schema objects changed\n got  %s\n want %s", when, got, objects)
		}
		if n := count(t, d, `SELECT count(*) FROM pragma_foreign_key_check`); n != 0 {
			t.Errorf("%s: %d foreign key violations", when, n)
		}
	}

	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("up: %v", err)
	}
	unchanged("after Up", before)
	if _, err := d.Exec(`UPDATE steps SET stage_kind = 'mix' WHERE id IN (30, 31)`); err != nil {
		t.Fatalf("mix rejected after Up: %v", err)
	}
	if _, err := d.Exec(`UPDATE steps SET stage_kind = 'whisk' WHERE id = 50`); err == nil {
		t.Error("an unknown stage kind was accepted after Up, want the CHECK")
	}
	withMix := snapshot(t, d)

	// What Down must leave: the same, with the mix rows unset.
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE steps SET stage_kind = '' WHERE stage_kind = 'mix'`); err != nil {
		t.Fatal(err)
	}
	unmixed := snapshot(t, tx)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	p, err := goose.NewProvider(goose.DialectSQLite3, d, db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 33); err != nil {
		t.Fatalf("down: %v", err)
	}
	unchanged("after Down", unmixed)
	if _, err := d.Exec(`UPDATE steps SET stage_kind = 'mix' WHERE id = 50`); err == nil {
		t.Error("mix accepted after Down, want the old CHECK back")
	}

	if _, err := p.UpTo(ctx, 34); err != nil {
		t.Fatalf("up again: %v", err)
	}
	unchanged("after Up again", unmixed)
	if reflect.DeepEqual(withMix["steps"], unmixed["steps"]) {
		t.Error("the mix rows were never set, so the Down check proved nothing")
	}
}
