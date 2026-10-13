package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
)

// seedPre00008 writes a recipe graph in the shape migration 00007 left, with a
// clone, a revision, a snapshot, a note and a shopping entry that references
// the recipe.
func seedPre00008(t *testing.T, d *sql.DB) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO recipes (id, name, created_at, created_by, updated_at, updated_by) VALUES (7, 'Stew', 't', 'a', 't', 'a')`,
		`INSERT INTO recipes (id, name, cloned_from_recipe_id, cloned_from_version, created_at, created_by, updated_at, updated_by) VALUES (8, 'Stew copy', 7, 1, 't', 'a', 't', 'a')`,
		`INSERT INTO recipe_tags (recipe_id, position, tag) VALUES (7, 0, 'winter')`,
		`INSERT INTO components (id, recipe_id, position, name) VALUES (3, 7, 0, '')`,
		`INSERT INTO ingredient_lines (id, component_id, position, rule, raw) VALUES (4, 3, 0, 'linear', 'water')`,
		`INSERT INTO line_qualifiers (line_id, position, kind, text) VALUES (4, 0, 'note', 'cold')`,
		`INSERT INTO steps (id, recipe_id, component_id, position, step_key, text) VALUES (5, 7, 3, 0, 'a', 'Boil.')`,
		`INSERT INTO steps (id, recipe_id, component_id, position, step_key, text) VALUES (6, 7, 3, 1, 'b', 'Serve.')`,
		`INSERT INTO step_dependencies (step_id, position, depends_on_step_id) VALUES (6, 0, 5)`,
		`INSERT INTO notes (recipe_id, body, created_at, created_by) VALUES (7, 'needs thyme', 't', 'a')`,
		`INSERT INTO made_it (recipe_id, made_on, created_at, created_by) VALUES (7, '2026-01-01', 't', 'a')`,
		`INSERT INTO revisions (id, recipe_id, version, document, change_kind, created_at, created_by) VALUES (9, 7, 1, '{}', 'edit', 't', 'a')`,
		`INSERT INTO source_snapshots (recipe_id, kind, content, sha256, created_at, created_by) VALUES (7, 'text', x'6162', 'h', 't', 'a')`,
		`INSERT INTO shopping_lists (id, name, created_at) VALUES (1, 'Week', 't')`,
		`INSERT INTO shopping_entries (list_id, recipe_id, multiplier) VALUES (1, 7, '1/1')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func migratedTo7(t *testing.T) *sql.DB {
	t.Helper()
	d := openRaw(t)
	if err := db.Migrate(context.Background(), d, migrationsBelow(t, "00008")); err != nil {
		t.Fatal(err)
	}
	return d
}

// Rows from before 00008 have no household of their own, so they go to the
// household with the lowest id; nothing is lost and ids are kept.
func TestMigration00008AssignsExistingRecipesToTheFirstHousehold(t *testing.T) {
	ctx := context.Background()
	d := migratedTo7(t)
	seedPre00008(t, d)
	hs := household.New(d)
	first, err := hs.Create(ctx, "First", auth.DevEmail)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hs.Create(ctx, "Second", auth.DevOtherEmail); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("migrate over recipe rows: %v", err)
	}
	for table, want := range map[string]int{
		"recipes": 2, "recipe_tags": 1, "components": 1, "ingredient_lines": 1, "line_qualifiers": 1,
		"steps": 2, "step_dependencies": 1, "notes": 1, "made_it": 1, "revisions": 1, "source_snapshots": 1,
		"recipe_search": 2, "shopping_entries": 1,
	} {
		if n := count(t, d, `SELECT COUNT(*) FROM `+table); n != want {
			t.Errorf("%s has %d rows after the migration, want %d", table, n, want)
		}
		if table == "shopping_entries" {
			continue // shopping rows carry no household yet
		}
		if n := count(t, d, `SELECT COUNT(*) FROM `+table+` WHERE household_id <> ?`, first.ID); n != 0 {
			t.Errorf("%s has %d rows outside the first household", table, n)
		}
	}
	var clonedFrom int64
	if err := d.QueryRow(`SELECT cloned_from_recipe_id FROM recipes WHERE id = 8`).Scan(&clonedFrom); err != nil || clonedFrom != 7 {
		t.Errorf("clone points at %d (err %v), want recipe 7 kept", clonedFrom, err)
	}
	var recipeID int64
	if err := d.QueryRow(`SELECT recipe_id FROM shopping_entries`).Scan(&recipeID); err != nil || recipeID != 7 {
		t.Errorf("shopping entry names recipe %d (err %v), want 7 kept", recipeID, err)
	}
	// The foreign keys are live on the rebuilt tables: shopping still guards
	// the recipe it uses, and the recipe still cascades to its children.
	if _, err := d.Exec(`DELETE FROM recipes WHERE id = 7`); err == nil {
		t.Error("deleting a recipe a shopping list uses succeeded")
	}
	if rows, err := d.Query(`PRAGMA foreign_key_check`); err != nil {
		t.Fatal(err)
	} else {
		defer func() { _ = rows.Close() }()
		if rows.Next() {
			t.Error("foreign_key_check reports violations after the migration")
		}
	}
	// The store reads what the migration carried over.
	s := New(d)
	hctx := household.WithContext(authedCtx(t), first)
	got, err := s.Search(hctx, SearchQuery{Text: "thyme"})
	if err != nil || len(got) != 1 || got[0].Name != "Stew" {
		t.Errorf("search after the migration = %v (err %v), want Stew", names(got), err)
	}
	if _, err := s.Get(otherCtx(t), 7); err == nil {
		t.Error("the second household reads the first household's migrated recipe")
	}
}

// With recipe rows and nobody to own them the migration stops; it never
// invents an owner (ADR-00018: no deployment holds data).
func TestMigration00008RefusesRecipesWithNoHousehold(t *testing.T) {
	d := migratedTo7(t)
	seedPre00008(t, d)
	err := db.Migrate(context.Background(), d, db.Migrations())
	if err == nil || !strings.Contains(err.Error(), "00008") || !strings.Contains(err.Error(), "household_id") {
		t.Fatalf("migrating recipes with no household = %v, want an error naming 00008 and household_id", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM recipes`); n != 2 {
		t.Errorf("a refused migration left %d recipes, want the original 2", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM pragma_table_info('recipes') WHERE name = 'household_id'`); n != 0 {
		t.Error("a refused migration changed the schema")
	}
}

func TestMigration00008DownKeepsRecipes(t *testing.T) {
	ctx := context.Background()
	d := migratedTo7(t)
	seedPre00008(t, d)
	if _, err := household.New(d).Create(ctx, "First", auth.DevEmail); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, d, db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Down(ctx); err != nil {
		t.Fatalf("rolling back 00008: %v", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM pragma_table_info('recipes') WHERE name = 'household_id'`); n != 0 {
		t.Error("household_id survived the rollback")
	}
	for table, want := range map[string]int{"recipes": 2, "steps": 2, "notes": 1, "revisions": 1, "recipe_search": 2, "shopping_entries": 1} {
		if n := count(t, d, `SELECT COUNT(*) FROM `+table); n != want {
			t.Errorf("%s has %d rows after the rollback, want %d", table, n, want)
		}
	}
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("migrating up again after the rollback: %v", err)
	}
}

// The rebuild leaves no staging table behind, and no foreign key still names
// one: a rename that failed to follow a reference would surface here.
func TestMigration00008LeavesNoStagingNames(t *testing.T) {
	d := newTestDB(t)
	if n := count(t, d, `SELECT COUNT(*) FROM sqlite_master WHERE sql LIKE '%\_new%' ESCAPE '\' OR name LIKE '%\_new' ESCAPE '\'`); n != 0 {
		t.Errorf("%d schema objects still mention a _new staging name", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM sqlite_master WHERE sql LIKE '%\_old%' ESCAPE '\' OR name LIKE '%\_old' ESCAPE '\'`); n != 0 {
		t.Errorf("%d schema objects mention an _old staging name", n)
	}
}
