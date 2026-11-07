package db_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/db/dbtest"
)

func mealTypes(t *testing.T, d *sql.DB) map[int]string {
	t.Helper()
	rows, err := d.Query(`SELECT id, meal_type FROM recipes ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int]string{}
	for rows.Next() {
		var id int
		var m string
		if err := rows.Scan(&id, &m); err != nil {
			t.Fatal(err)
		}
		out[id] = m
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Migration 00033 widens recipes.meal_type's CHECK by dropping and re-adding
// the column. Every stored meal type must survive Up, baked must be accepted
// after it, Down must send baked back to unset and keep the rest, and Up again
// must be clean.
func TestMealBakedMigrationKeepsEveryMealType(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, dbtest.MigrationsBelow("00033")); err != nil {
		t.Fatalf("migrate to before 00033: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO households (id, name, created_at) VALUES (1, 'H', 't')`); err != nil {
		t.Fatal(err)
	}
	want := map[int]string{1: "", 2: "breakfast", 3: "main", 4: "side", 5: "dessert", 6: "snack", 7: "drink"}
	for id, m := range want {
		if _, err := d.Exec(`INSERT INTO recipes (id, household_id, name, meal_type, created_at, created_by, updated_at, updated_by) VALUES (?, 1, 'r', ?, 't', 'a', 't', 'a')`, id, m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Exec(`UPDATE recipes SET meal_type = 'baked' WHERE id = 1`); err == nil {
		t.Fatal("baked accepted before 00033, want the old CHECK")
	}

	same := func(when string, want map[int]string) {
		t.Helper()
		got := mealTypes(t, d)
		if len(got) != len(want) {
			t.Fatalf("%s: %d recipes, want %d", when, len(got), len(want))
		}
		for id, m := range want {
			if got[id] != m {
				t.Errorf("%s: recipe %d meal_type = %q, want %q", when, id, got[id], m)
			}
		}
		if n := count(t, d, `SELECT count(*) FROM pragma_foreign_key_check`); n != 0 {
			t.Errorf("%s: %d foreign key violations", when, n)
		}
	}

	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("up: %v", err)
	}
	same("after Up", want)
	if _, err := d.Exec(`UPDATE recipes SET meal_type = 'baked' WHERE id IN (1, 3)`); err != nil {
		t.Fatalf("baked rejected after Up: %v", err)
	}
	if _, err := d.Exec(`UPDATE recipes SET meal_type = 'lunch' WHERE id = 2`); err == nil {
		t.Error("an unknown meal type was accepted after Up, want the CHECK")
	}

	p, err := goose.NewProvider(goose.DialectSQLite3, d, db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 32); err != nil {
		t.Fatalf("down: %v", err)
	}
	down := map[int]string{}
	for id, m := range want {
		down[id] = m
	}
	down[3] = "" // was baked
	same("after Down", down)
	if _, err := d.Exec(`UPDATE recipes SET meal_type = 'baked' WHERE id = 2`); err == nil {
		t.Error("baked accepted after Down, want the old CHECK back")
	}

	if _, err := p.UpTo(ctx, 33); err != nil {
		t.Fatalf("up again: %v", err)
	}
	same("after Up again", down)
}

func count(t *testing.T, d *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := d.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
