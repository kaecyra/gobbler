package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/db/dbtest"
)

// queryer is the read half of *sql.DB and *sql.Tx.
type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// snapshot reads every row of every table, each row as its column values
// named and sorted by column name (the migration moves meal_type to the end of
// recipes, which is not damage), each table's rows sorted. goose's own table is
// left out.
func snapshot(t *testing.T, q queryer) map[string][]string {
	t.Helper()
	tables, err := q.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name <> 'goose_db_version'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for tables.Next() {
		var n string
		if err := tables.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := tables.Err(); err != nil {
		t.Fatal(err)
	}
	_ = tables.Close()

	out := map[string][]string{}
	for _, name := range names {
		cols, err := q.Query(`SELECT name FROM pragma_table_info('` + name + `') ORDER BY name`)
		if err != nil {
			t.Fatal(err)
		}
		var list []string
		for cols.Next() {
			var c string
			if err := cols.Scan(&c); err != nil {
				t.Fatal(err)
			}
			list = append(list, `quote("`+c+`")`)
		}
		_ = cols.Close()
		rows, err := q.Query(`SELECT ` + strings.Join(list, ` || '|' || `) + ` FROM "` + name + `"`)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for rows.Next() {
			var row string
			if err := rows.Scan(&row); err != nil {
				t.Fatal(err)
			}
			out[name] = append(out[name], row)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		sort.Strings(out[name])
	}
	return out
}

func sameSnapshot(t *testing.T, when string, got, want map[string][]string) {
	t.Helper()
	for table, rows := range want {
		if strings.Join(got[table], "\n") != strings.Join(rows, "\n") {
			t.Errorf("%s: table %s changed\n got  %q\n want %q", when, table, got[table], rows)
		}
	}
	for table := range got {
		if _, ok := want[table]; !ok {
			t.Errorf("%s: table %s appeared", when, table)
		}
	}
}

// seedRecipes stores seven recipes, one per meal type with the first unset,
// every other column given a value of its own, and children of the kinds that
// reference recipes: a clone link between recipes, and a component, step,
// ingredient line, tag, note, author note, equipment, made-it entry, revision
// and picture on two of them.
func seedRecipes(t *testing.T, d *sql.DB) {
	t.Helper()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO households (id, name, created_at) VALUES (1, 'H', 't0')`)
	exec(`INSERT INTO equipment (id, name) VALUES (5000, 'zz dutch oven')`)
	meals := []string{"", "breakfast", "main", "side", "dessert", "snack", "drink"}
	for i, m := range meals {
		id := i + 1
		var clone any
		if id > 1 {
			clone = 1
		}
		exec(`INSERT INTO recipes (id, household_id, name, description, source_url, author, yield_qty, yield_unit, servings,
			calories_per_serving, rating, prep_ns, cook_ns, total_ns, version, cloned_from_recipe_id, cloned_from_version,
			created_at, created_by, updated_at, updated_by, video_id, spice_level, cuisine, meal_type)
			VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, 'italian', ?)`,
			id, fmt.Sprintf("name %d", id), fmt.Sprintf("desc %d", id), fmt.Sprintf("https://x.test/%d", id), fmt.Sprintf("author %d", id),
			fmt.Sprintf("%d", id*10), fmt.Sprintf("unit %d", id), fmt.Sprintf("%d", id+1), 100+id, id%6, id*1000, id*2000, id*3000, id+1,
			clone, fmt.Sprintf("c%d", id), fmt.Sprintf("cb%d", id), fmt.Sprintf("u%d", id), fmt.Sprintf("ub%d", id),
			fmt.Sprintf("vid%d", id), id%4, m)
	}
	for _, id := range []int{3, 5} {
		comp := id * 10
		exec(`INSERT INTO components (id, household_id, recipe_id, position, name) VALUES (?, 1, ?, 0, ?)`, comp, id, fmt.Sprintf("part %d", id))
		exec(`INSERT INTO steps (id, household_id, recipe_id, component_id, position, step_key, text, duration_ns, passive, stage_kind, title)
			VALUES (?, 1, ?, ?, 0, ?, ?, ?, 1, 'bake', ?)`, comp, id, comp, fmt.Sprintf("k%d", id), fmt.Sprintf("step text %d", id), id*7, fmt.Sprintf("step title %d", id))
		exec(`INSERT INTO ingredient_lines (id, household_id, component_id, position, qty, rule, raw) VALUES (?, 1, ?, 0, '2', 'linear', ?)`, comp, comp, fmt.Sprintf("2 cups thing %d", id))
		exec(`INSERT INTO recipe_tags (household_id, recipe_id, position, tag) VALUES (1, ?, 0, ?)`, id, fmt.Sprintf("tag%d", id))
		exec(`INSERT INTO notes (id, household_id, recipe_id, body, created_at, created_by, pinned) VALUES (?, 1, ?, ?, 't', 'a', 1)`, comp, id, fmt.Sprintf("note %d", id))
		exec(`INSERT INTO recipe_author_notes (household_id, recipe_id, position, title, text) VALUES (1, ?, 0, ?, ?)`, id, fmt.Sprintf("at %d", id), fmt.Sprintf("author note %d", id))
		exec(`INSERT INTO recipe_equipment (id, household_id, recipe_id, position, equipment_id, qty) VALUES (?, 1, ?, 0, 5000, '1')`, comp, id)
		exec(`INSERT INTO made_it (id, household_id, recipe_id, made_on, rating, comment, created_at, created_by) VALUES (?, 1, ?, '2026-01-01', 4, ?, 't', 'a')`, comp, id, fmt.Sprintf("made %d", id))
		exec(`INSERT INTO revisions (id, household_id, recipe_id, version, document, change_kind, created_at, created_by) VALUES (?, 1, ?, 1, ?, 'edit', 't', 'a')`, comp, id, fmt.Sprintf("{\"id\":%d}", id))
		exec(`INSERT INTO recipe_pictures (recipe_id, household_id, key, updated_at) VALUES (?, 1, ?, 't')`, id, fmt.Sprintf("pic%d", id))
	}
}

// Migration 00033 widens recipes.meal_type's CHECK by dropping and re-adding
// the column on the table that holds every recipe. Nothing else may change:
// every column of every recipe, and every row of every table, must come out of
// Up, Down and Up again as it went in, apart from Down sending baked back to
// unset. baked must be accepted after Up and refused after Down.
func TestMealBakedMigrationChangesOnlyMealType(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, dbtest.MigrationsBelow("00033")); err != nil {
		t.Fatalf("migrate to before 00033: %v", err)
	}
	seedRecipes(t, d)
	before := snapshot(t, d)
	if n := len(before["recipes"]); n != 7 {
		t.Fatalf("%d recipes seeded, want 7", n)
	}
	if n := len(before["steps"]) + len(before["revisions"]) + len(before["recipe_equipment"]); n != 6 {
		t.Fatalf("%d child rows seeded in steps, revisions and recipe_equipment, want 6", n)
	}
	if _, err := d.Exec(`UPDATE recipes SET meal_type = 'baked' WHERE id = 1`); err == nil {
		t.Fatal("baked accepted before 00033, want the old CHECK")
	}
	noViolations := func(when string) {
		t.Helper()
		if n := count(t, d, `SELECT count(*) FROM pragma_foreign_key_check`); n != 0 {
			t.Errorf("%s: %d foreign key violations", when, n)
		}
	}

	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("up: %v", err)
	}
	sameSnapshot(t, "after Up", snapshot(t, d), before)
	noViolations("after Up")
	if _, err := d.Exec(`UPDATE recipes SET meal_type = 'baked' WHERE id IN (1, 3)`); err != nil {
		t.Fatalf("baked rejected after Up: %v", err)
	}
	if _, err := d.Exec(`UPDATE recipes SET meal_type = 'lunch' WHERE id = 2`); err == nil {
		t.Error("an unknown meal type was accepted after Up, want the CHECK")
	}
	withBaked := snapshot(t, d)

	// What Down must leave: the same, with the baked rows unset.
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE recipes SET meal_type = '' WHERE meal_type = 'baked'`); err != nil {
		t.Fatal(err)
	}
	unbaked := snapshot(t, tx)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	p, err := goose.NewProvider(goose.DialectSQLite3, d, db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 32); err != nil {
		t.Fatalf("down: %v", err)
	}
	sameSnapshot(t, "after Down", snapshot(t, d), unbaked)
	noViolations("after Down")
	if _, err := d.Exec(`UPDATE recipes SET meal_type = 'baked' WHERE id = 2`); err == nil {
		t.Error("baked accepted after Down, want the old CHECK back")
	}

	if _, err := p.UpTo(ctx, 33); err != nil {
		t.Fatalf("up again: %v", err)
	}
	sameSnapshot(t, "after Up again", snapshot(t, d), unbaked)
	noViolations("after Up again")
	if reflect.DeepEqual(withBaked["recipes"], unbaked["recipes"]) {
		t.Error("the baked rows were never set, so the Down check proved nothing")
	}
}

func count(t *testing.T, d *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := d.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
