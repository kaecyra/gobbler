package catalog

import (
	"context"
	"database/sql"
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

// migrationDB opens a database migrated to just before the overlay migration.
func migrationDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, migrationsBelow(t, "00010")); err != nil {
		t.Fatalf("migrate to the household shopping tables: %v", err)
	}
	return ctx, d
}

func mustExec(t *testing.T, d *sql.DB, q string, args ...any) int64 {
	t.Helper()
	res, err := d.Exec(q, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func provider(t *testing.T, d *sql.DB) *goose.Provider {
	t.Helper()
	p, err := goose.NewProvider(goose.DialectSQLite3, d, db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func noViolations(t *testing.T, d *sql.DB, when string) {
	t.Helper()
	if n := count(t, d, `SELECT count(*) FROM pragma_foreign_key_check`); n != 0 {
		t.Errorf("%d foreign key violations %s", n, when)
	}
}

// The overlay migration rebuilds ingredients and ingredient_aliases while other
// tables reference them. A reviewed ingredient (the seed) stays central. An
// unreviewed one goes, with its aliases, to the overlay of the one household
// that references it, and stays central when none or several do. Every id
// survives and nothing that pointed at them is orphaned.
func TestOverlayMigrationSplitsReviewedFromUnreviewed(t *testing.T) {
	ctx, d := migrationDB(t)
	hs := household.New(d)
	first, err := hs.Create(ctx, "First", "first@example.com")
	if err != nil {
		t.Fatal(err)
	}
	second, err := hs.Create(ctx, "Second", "second@example.com")
	if err != nil {
		t.Fatal(err)
	}
	var onion int64
	if err := d.QueryRow(`SELECT id FROM ingredients WHERE name = 'onion'`).Scan(&onion); err != nil {
		t.Fatal(err)
	}
	reviewed := mustExec(t, d, `INSERT INTO ingredients (name, aisle, reviewed) VALUES ('zz reviewed', 'x', 1)`)
	proposed := mustExec(t, d, `INSERT INTO ingredients (name, aisle, reviewed) VALUES ('zz proposed', 'x', 0)`)
	mustExec(t, d, `INSERT INTO ingredient_aliases (alias, ingredient_id) VALUES ('zz proposed alias', ?)`, proposed)
	mustExec(t, d, `INSERT INTO shopping_pantry (household_id, ingredient_id) VALUES (?, ?)`, first.ID, onion)
	// Used by the second household alone, by both, and by neither.
	secondOnly := mustExec(t, d, `INSERT INTO ingredients (name, aisle, reviewed) VALUES ('zz second only', 'x', 0)`)
	mustExec(t, d, `INSERT INTO ingredient_aliases (alias, ingredient_id) VALUES ('zz second alias', ?)`, secondOnly)
	mustExec(t, d, `INSERT INTO shopping_pantry (household_id, ingredient_id) VALUES (?, ?)`, second.ID, secondOnly)
	shared := mustExec(t, d, `INSERT INTO ingredients (name, aisle, reviewed) VALUES ('zz shared', 'x', 0)`)
	mustExec(t, d, `INSERT INTO shopping_pantry (household_id, ingredient_id) VALUES (?, ?)`, first.ID, shared)
	mustExec(t, d, `INSERT INTO shopping_pantry (household_id, ingredient_id) VALUES (?, ?)`, second.ID, shared)
	orphan := mustExec(t, d, `INSERT INTO ingredients (name, aisle, reviewed) VALUES ('zz orphan', 'x', 0)`)
	recipeID := mustExec(t, d, `INSERT INTO recipes (household_id, name, created_at, created_by, updated_at, updated_by)
		VALUES (?, 'Old', '2026-01-01T00:00:00Z', 'a', '2026-01-01T00:00:00Z', 'a')`, first.ID)
	comp := mustExec(t, d, `INSERT INTO components (household_id, recipe_id, position) VALUES (?, ?, 0)`, first.ID, recipeID)
	lines := map[int64]int64{} // line id -> ingredient id
	for pos, ing := range []int64{onion, reviewed, proposed} {
		id := mustExec(t, d, `INSERT INTO ingredient_lines (household_id, component_id, position, ingredient_id, rule)
			VALUES (?, ?, ?, ?, 'linear')`, first.ID, comp, pos, ing)
		lines[id] = ing
	}
	aliasesBefore := count(t, d, `SELECT count(*) FROM ingredient_aliases`)

	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("migrate to the overlay: %v", err)
	}
	for _, c := range []struct {
		id    int64
		owner any
		what  string
	}{{onion, nil, "seed onion"}, {reviewed, nil, "reviewed entry"}, {proposed, first.ID, "unreviewed entry used by one household"},
		{secondOnly, second.ID, "unreviewed entry used by the second household alone"},
		{shared, nil, "unreviewed entry used by two households"}, {orphan, nil, "unreferenced unreviewed entry"}} {
		var got sql.NullInt64
		if err := d.QueryRow(`SELECT household_id FROM ingredients WHERE id = ?`, c.id).Scan(&got); err != nil {
			t.Fatalf("%s lost its id: %v", c.what, err)
		}
		if (c.owner == nil) == got.Valid || (c.owner != nil && got.Int64 != c.owner.(int64)) {
			t.Errorf("%s: household_id = %v, want %v", c.what, got, c.owner)
		}
	}
	if n := count(t, d, `SELECT count(*) FROM ingredient_aliases WHERE alias = 'zz proposed alias' AND household_id = ?`, first.ID); n != 1 {
		t.Error("the unreviewed entry's alias did not go with it")
	}
	if n := count(t, d, `SELECT count(*) FROM ingredient_aliases WHERE alias = 'zz second alias' AND household_id = ?`, second.ID); n != 1 {
		t.Error("the second household's entry's alias did not go with it")
	}
	if n := count(t, d, `SELECT count(*) FROM ingredient_aliases WHERE household_id IS NULL`); n != aliasesBefore-2 {
		t.Errorf("%d central aliases, want every alias but the two overlay entries' (%d)", n, aliasesBefore-2)
	}
	for line, ing := range lines {
		if n := count(t, d, `SELECT count(*) FROM ingredient_lines WHERE id = ? AND ingredient_id = ?`, line, ing); n != 1 {
			t.Errorf("line %d no longer names ingredient %d", line, ing)
		}
	}
	if n := count(t, d, `SELECT count(*) FROM shopping_pantry WHERE ingredient_id = ?`, onion); n != 1 {
		t.Errorf("pantry rows on onion = %d, want 1", n)
	}
	noViolations(t, d, "after the migration")
}

// With no household there is nobody to own a proposal, so it stays central.
func TestOverlayMigrationWithoutAHouseholdKeepsEverythingCentral(t *testing.T) {
	ctx, d := migrationDB(t)
	mustExec(t, d, `INSERT INTO ingredients (name, aisle, reviewed) VALUES ('zz proposed', 'x', 0)`)
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT count(*) FROM ingredients WHERE household_id IS NOT NULL`); n != 0 {
		t.Errorf("%d overlay entries with no household to own them", n)
	}
	if n := count(t, d, `SELECT count(*) FROM ingredients WHERE name = 'zz proposed'`); n != 1 {
		t.Error("the proposal was lost")
	}
}

// Down returns to one global catalog. Two households holding one name abort it,
// and the database stays at the overlay; otherwise overlay entries become
// ordinary ones, a household's alias on a central entry, the aisle overrides
// and the suggestions are dropped, and Up applies again.
func TestOverlayMigrationDown(t *testing.T) {
	ctx, d := migrationDB(t)
	hs := household.New(d)
	one, err := hs.Create(ctx, "One", "one@example.com")
	if err != nil {
		t.Fatal(err)
	}
	two, err := hs.Create(ctx, "Two", "two@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	p := provider(t, d)
	var onion int64
	if err := d.QueryRow(`SELECT id FROM ingredients WHERE name = 'onion'`).Scan(&onion); err != nil {
		t.Fatal(err)
	}
	ghee1 := mustExec(t, d, `INSERT INTO ingredients (household_id, name, aisle, suggested_at) VALUES (?, 'zz ghee', 'x', '2026-01-01T00:00:00Z')`, one.ID)
	ghee2 := mustExec(t, d, `INSERT INTO ingredients (household_id, name, aisle) VALUES (?, 'zz ghee', 'y')`, two.ID)
	mustExec(t, d, `INSERT INTO ingredient_aliases (household_id, alias, ingredient_id) VALUES (?, 'zz clarified', ?)`, one.ID, ghee1)
	mustExec(t, d, `INSERT INTO ingredient_aliases (household_id, alias, ingredient_id) VALUES (?, 'zz bulb', ?)`, two.ID, onion)
	mustExec(t, d, `INSERT INTO ingredient_aisle_overrides (household_id, ingredient_id, aisle) VALUES (?, ?, 'zz veg')`, two.ID, onion)
	mustExec(t, d, `INSERT INTO shopping_pantry (household_id, ingredient_id) VALUES (?, ?)`, one.ID, ghee1)

	if _, err := p.DownTo(ctx, 9); err == nil {
		t.Fatal("rolling back succeeded although two households hold \"zz ghee\"")
	}
	if v, err := p.GetDBVersion(ctx); err != nil || v != 10 {
		t.Fatalf("version after the aborted rollback = %d (err %v), want 10", v, err)
	}
	if n := count(t, d, `SELECT count(*) FROM ingredients WHERE name = 'zz ghee'`); n != 2 {
		t.Fatalf("%d ghee entries after the aborted rollback, want both", n)
	}

	mustExec(t, d, `DELETE FROM shopping_pantry`)
	mustExec(t, d, `DELETE FROM ingredients WHERE id = ?`, ghee2)
	if _, err := p.DownTo(ctx, 9); err != nil {
		t.Fatalf("rolling back with one overlay entry: %v", err)
	}
	if n := count(t, d, `SELECT count(*) FROM ingredients WHERE id = ? AND name = 'zz ghee'`, ghee1); n != 1 {
		t.Error("the overlay entry did not survive as an ordinary one")
	}
	if n := count(t, d, `SELECT count(*) FROM ingredient_aliases WHERE alias = 'zz clarified' AND ingredient_id = ?`, ghee1); n != 1 {
		t.Error("the overlay entry lost its own alias")
	}
	if n := count(t, d, `SELECT count(*) FROM ingredient_aliases WHERE alias = 'zz bulb'`); n != 0 {
		t.Error("a household's alias on a central entry became global")
	}
	if n := count(t, d, `SELECT count(*) FROM sqlite_master WHERE name = 'ingredient_aisle_overrides'`); n != 0 {
		t.Error("the aisle overrides table remains")
	}
	noViolations(t, d, "after rolling back")

	if _, err := p.UpTo(ctx, 10); err != nil {
		t.Fatalf("applying the overlay again: %v", err)
	}
	noViolations(t, d, "after the round trip")
	if n := count(t, d, `SELECT count(*) FROM ingredients WHERE id = ? AND name = 'zz ghee'`, ghee1); n != 1 {
		t.Error("the entry was lost over the round trip")
	}
}
