package db_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/db/dbtest"
)

// Migration 00039 widens recipes.cuisine's CHECK to admit asian and bbq by
// dropping and re-adding the column. Nothing else may change: every row of
// every table must come out of Up, Down and Up again as it went in, apart from
// Down sending asian and bbq back to unset. An unknown cuisine stays refused.
func TestCuisineAsianBBQMigrationChangesOnlyCuisine(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, dbtest.MigrationsBelow("00039")); err != nil {
		t.Fatalf("migrate to before 00039: %v", err)
	}
	seedRecipes(t, d)
	before := snapshot(t, d)
	if n := len(before["recipes"]); n != 7 {
		t.Fatalf("%d recipes seeded, want 7", n)
	}
	for _, c := range []string{"asian", "bbq"} {
		if _, err := d.Exec(`UPDATE recipes SET cuisine = ? WHERE id = 1`, c); err == nil {
			t.Fatalf("%s accepted before 00039, want the old CHECK", c)
		}
	}
	noViolations := func(when string) {
		t.Helper()
		if n := count(t, d, `SELECT count(*) FROM pragma_foreign_key_check`); n != 0 {
			t.Errorf("%s: %d foreign key violations", when, n)
		}
	}

	if err := db.Migrate(ctx, d, dbtest.MigrationsBelow("00040")); err != nil {
		t.Fatalf("up: %v", err)
	}
	sameSnapshot(t, "after Up", snapshot(t, d), before)
	noViolations("after Up")
	if _, err := d.Exec(`UPDATE recipes SET cuisine = 'asian' WHERE id IN (1, 3)`); err != nil {
		t.Fatalf("asian rejected after Up: %v", err)
	}
	if _, err := d.Exec(`UPDATE recipes SET cuisine = 'bbq' WHERE id = 5`); err != nil {
		t.Fatalf("bbq rejected after Up: %v", err)
	}
	if _, err := d.Exec(`UPDATE recipes SET cuisine = 'martian' WHERE id = 2`); err == nil {
		t.Error("an unknown cuisine was accepted after Up, want the CHECK")
	}
	withNew := snapshot(t, d)

	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE recipes SET cuisine = '' WHERE cuisine IN ('asian', 'bbq')`); err != nil {
		t.Fatal(err)
	}
	unset := snapshot(t, tx)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	p, err := goose.NewProvider(goose.DialectSQLite3, d, db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 38); err != nil {
		t.Fatalf("down: %v", err)
	}
	sameSnapshot(t, "after Down", snapshot(t, d), unset)
	noViolations("after Down")
	if _, err := d.Exec(`UPDATE recipes SET cuisine = 'bbq' WHERE id = 2`); err == nil {
		t.Error("bbq accepted after Down, want the old CHECK back")
	}

	if _, err := p.UpTo(ctx, 39); err != nil {
		t.Fatalf("up again: %v", err)
	}
	sameSnapshot(t, "after Up again", snapshot(t, d), unset)
	noViolations("after Up again")
	if reflect.DeepEqual(withNew["recipes"], unset["recipes"]) {
		t.Error("the asian and bbq rows were never set, so the Down check proved nothing")
	}
}
