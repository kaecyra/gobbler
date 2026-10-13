package shopping

import (
	"context"
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
)

// seedBefore00009 writes one of every shopping row, in the shape migrations
// before 00008 left (no household on any of them), and two households. It
// returns the id of the first.
func seedBefore00009(t *testing.T, d *sql.DB) int64 {
	t.Helper()
	ctx := context.Background()
	if err := db.Migrate(ctx, d, migrationsBefore(t, "00008")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO ingredients (id, name, aisle) VALUES (901, 'zz migration salt', 'test')`); err != nil {
		t.Fatal(err)
	}
	rid := oldRecipe(t, d, "Old", oldLine{ingredient: 901})
	for _, q := range []string{
		`INSERT INTO shopping_lists (id, name, created_at, include_optional) VALUES (7, 'Old list', 't', 1)`,
		`INSERT INTO shopping_manual_items (list_id, text) VALUES (7, 'napkins')`,
		`INSERT INTO shopping_pantry (ingredient_id) VALUES (901)`,
		`INSERT INTO shopping_checks (list_id, item_key, checked_at) VALUES (7, 'i:901', 't')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := d.Exec(`INSERT INTO shopping_entries (list_id, recipe_id, multiplier) VALUES (7, ?, '2/1')`, rid); err != nil {
		t.Fatal(err)
	}
	hs := household.New(d)
	first, err := hs.Create(ctx, "First", auth.DevEmail)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hs.Create(ctx, "Second", auth.DevOtherEmail); err != nil {
		t.Fatal(err)
	}
	return first.ID
}

func countRows(t *testing.T, d *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// Shopping rows from before 00009 have no household of their own, so they go
// to the household with the lowest id (the one 00008 gave their recipes to);
// nothing is lost and ids are kept.
func TestMigration00009AssignsExistingShoppingRowsToTheFirstHousehold(t *testing.T) {
	d := openRaw(t)
	first := seedBefore00009(t, d)
	if err := db.Migrate(context.Background(), d, db.Migrations()); err != nil {
		t.Fatalf("migrate over shopping rows: %v", err)
	}
	for _, table := range []string{"shopping_lists", "shopping_entries", "shopping_manual_items", "shopping_pantry", "shopping_checks"} {
		if n := countRows(t, d, `SELECT COUNT(*) FROM `+table); n != 1 {
			t.Errorf("%s has %d rows after the migration, want 1", table, n)
		}
		if n := countRows(t, d, `SELECT COUNT(*) FROM `+table+` WHERE household_id <> ?`, first); n != 0 {
			t.Errorf("%s has %d rows outside the first household", table, n)
		}
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM shopping_lists WHERE id = 7 AND name = 'Old list' AND include_optional = 1`); n != 1 {
		t.Error("the list lost its id, name or include_optional")
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM shopping_entries WHERE multiplier = '2/1'`); n != 1 {
		t.Error("the entry lost its multiplier")
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM pragma_foreign_key_check`); n != 0 {
		t.Errorf("foreign_key_check reports %d violations after the migration", n)
	}
}

// With shopping rows and nobody to own them the migration stops; it never
// invents an owner (ADR-00018). 00008 stops first when there are recipes, so
// this case has a list and a pantry row but no recipe.
func TestMigration00009RefusesShoppingRowsWithNoHousehold(t *testing.T) {
	d := openRaw(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, d, migrationsBefore(t, "00008")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO shopping_lists (id, name, created_at) VALUES (7, 'Old list', 't')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, d, db.Migrations()); err == nil {
		t.Fatal("migrating shopping rows with no household succeeded")
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM shopping_lists`); n != 1 {
		t.Errorf("a refused migration left %d lists, want the original 1", n)
	}
}

// Rolling 00009 back keeps every row and merges the households' pantries into
// the one global set; migrating up again works.
func TestMigration00009DownKeepsShoppingRows(t *testing.T) {
	d := openRaw(t)
	seedBefore00009(t, d)
	ctx := context.Background()
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, d, db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	// Later migrations sit above 00009; roll them back so 00009 is rolled back
	// too, whatever is newest.
	if _, err := p.DownTo(ctx, 8); err != nil {
		t.Fatalf("rolling back 00009: %v", err)
	}
	for _, table := range []string{"shopping_lists", "shopping_entries", "shopping_manual_items", "shopping_pantry", "shopping_checks"} {
		if n := countRows(t, d, `SELECT COUNT(*) FROM pragma_table_info('`+table+`') WHERE name = 'household_id'`); n != 0 {
			t.Errorf("household_id survived the rollback on %s", table)
		}
		if n := countRows(t, d, `SELECT COUNT(*) FROM `+table); n != 1 {
			t.Errorf("%s has %d rows after the rollback, want 1", table, n)
		}
	}
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("migrating up again after the rollback: %v", err)
	}
}
