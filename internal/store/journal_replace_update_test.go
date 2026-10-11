package store

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// dumpTables renders every row of the guarded tables, so a test can assert
// that a refused statement changed nothing anywhere, cascades included.
func dumpTables(t *testing.T, d *sql.DB) string {
	t.Helper()
	var b strings.Builder
	for _, table := range []string{"recipes", "source_snapshots", "revisions", "notes", "made_it", "ingredient_lines", "components"} {
		rows, err := d.Query(`SELECT * FROM ` + table + ` ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&b, "%s%v\n", table, vals)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
	}
	return b.String()
}

// guardedFixture builds two recipes (ids 1 and 2), each with revisions, two
// notes, two made-it entries and a snapshot, so every collision has a victim.
func guardedFixture(t *testing.T) *sql.DB {
	t.Helper()
	s, d := newTestStore(t)
	ctx := authedCtx(t)
	for _, name := range []string{"one", "two"} {
		id, err := s.Create(ctx, simpleRecipe(name))
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range []string{"a", "b"} { // revisions 1 and 2
			if err := s.Update(ctx, id, simpleRecipe(name+v)); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 2; i++ {
			if _, err := s.AddNote(ctx, id, fmt.Sprintf("%s note %d", name, i)); err != nil {
				t.Fatal(err)
			}
			if _, err := s.AddMadeIt(ctx, id, MadeIt{Comment: fmt.Sprintf("%s made %d", name, i)}); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.AddSnapshot(ctx, id, SnapshotText, []byte(name+" source")); err != nil {
			t.Fatal(err)
		}
	}
	// Recipe 3 has no children, so an id change is not stopped by a foreign key.
	bare, err := s.Create(ctx, simpleRecipe("bare"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DELETE FROM components WHERE recipe_id = ?`, bare); err != nil {
		t.Fatal(err)
	}
	return d
}

// Every statement here would delete or overwrite a row of an irreplaceable
// table. Each must be refused and leave all tables exactly as they were.
func TestGuardedRowsCannotBeOverwrittenByUpdateOrUpsert(t *testing.T) {
	stmts := map[string]string{
		"update or replace recipes id":      `UPDATE OR REPLACE recipes SET id = 1 WHERE id = 3`,
		"update or replace revisions id":    `UPDATE OR REPLACE revisions SET id = 1 WHERE id = 2`,
		"update or replace revisions ver":   `UPDATE OR REPLACE revisions SET version = 1 WHERE version = 2 AND recipe_id = 1`,
		"update or replace revisions recip": `UPDATE OR REPLACE revisions SET recipe_id = 1 WHERE recipe_id = 2 AND version = 1`,
		"update or replace notes id":        `UPDATE OR REPLACE notes SET id = 1 WHERE id = 2`,
		"update or replace made_it id":      `UPDATE OR REPLACE made_it SET id = 1 WHERE id = 2`,
		"update or replace snapshots":       `UPDATE OR REPLACE source_snapshots SET id = 1 WHERE id = 2`,
		"plain update recipes id":           `UPDATE recipes SET id = 99 WHERE id = 3`,
		"upsert recipes": `INSERT INTO recipes (id, name, created_at, created_by, updated_at, updated_by) VALUES (1, 'T', 'n', 'me', 'n', 'me')
			ON CONFLICT (id) DO UPDATE SET name = 'TAMPERED'`,
		"upsert notes": `INSERT INTO notes (id, recipe_id, body, created_at, created_by) VALUES (1, 1, 'T', 'n', 'me')
			ON CONFLICT (id) DO UPDATE SET body = 'TAMPERED'`,
		"upsert made_it": `INSERT INTO made_it (id, recipe_id, made_on, created_at, created_by) VALUES (1, 1, '2000-01-01', 'n', 'me')
			ON CONFLICT (id) DO UPDATE SET comment = 'TAMPERED'`,
		"upsert revisions": `INSERT INTO revisions (recipe_id, version, document, change_kind, created_at, created_by) VALUES (1, 1, '{}', 'edit', 'n', 'me')
			ON CONFLICT (recipe_id, version) DO UPDATE SET document = 'TAMPERED'`,
		"upsert snapshots": `INSERT INTO source_snapshots (recipe_id, kind, content, sha256, created_at, created_by) VALUES (1, 'text', 'T', 'x', 'n', 'me')
			ON CONFLICT (recipe_id, kind) DO UPDATE SET content = 'TAMPERED'`,
	}
	for name, stmt := range stmts {
		t.Run(name, func(t *testing.T) {
			d := guardedFixture(t)
			before := dumpTables(t, d)
			if _, err := d.Exec(stmt); err == nil {
				t.Error("statement succeeded, want it refused")
			}
			if after := dumpTables(t, d); after != before {
				t.Errorf("tables changed:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

// The guards must not block the writes the store itself makes.
func TestGuardsAllowTheStoresOwnUpdates(t *testing.T) {
	d := guardedFixture(t)
	for name, stmt := range map[string]string{
		"revision document (merge hook)": `UPDATE revisions SET document = document WHERE id = 1`,
		"recipe columns (edit)":          `UPDATE recipes SET name = 'renamed', version = version + 1 WHERE id = 1`,
		"note body":                      `UPDATE notes SET body = 'edited' WHERE id = 1`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Revisions are history: a direct DELETE is refused, while deleting the recipe
// still removes them through the cascade.
func TestRevisionsCannotBeDeletedDirectly(t *testing.T) {
	d := guardedFixture(t)
	before := dumpTables(t, d)
	for name, stmt := range map[string]string{
		"all":       `DELETE FROM revisions`,
		"one":       `DELETE FROM revisions WHERE id = 1`,
		"by recipe": `DELETE FROM revisions WHERE recipe_id = 1`,
	} {
		if _, err := d.Exec(stmt); err == nil {
			t.Errorf("%s: DELETE succeeded, want it refused", name)
		}
	}
	if after := dumpTables(t, d); after != before {
		t.Errorf("revisions changed after refused deletes:\nbefore:\n%s\nafter:\n%s", before, after)
	}

	if _, err := d.Exec(`DELETE FROM recipes WHERE id = 1`); err != nil {
		t.Fatalf("deleting the recipe: %v", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM revisions WHERE recipe_id = 1`); n != 0 {
		t.Errorf("recipe delete left %d revisions", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM revisions WHERE recipe_id = 2`); n == 0 {
		t.Error("recipe delete removed another recipe's revisions")
	}
	if n := count(t, d, `SELECT COUNT(*) FROM revision_ingredients ri WHERE NOT EXISTS (SELECT 1 FROM revisions r WHERE r.id = ri.revision_id)`); n != 0 {
		t.Errorf("%d orphan revision_ingredients rows", n)
	}
}

// Notes and made-it entries stay deletable for the future journal UI.
func TestNotesAndMadeItStayDeletable(t *testing.T) {
	d := guardedFixture(t)
	for _, stmt := range []string{`DELETE FROM notes WHERE id = 1`, `DELETE FROM made_it WHERE id = 1`} {
		if _, err := d.Exec(stmt); err != nil {
			t.Errorf("%s: %v", stmt, err)
		}
	}
}
