package store

import (
	"testing"
)

// REPLACE deletes the conflicting row without firing delete triggers, so the
// irreplaceable history tables must refuse the insert itself.
func TestHistoryRowsCannotBeReplacedThroughTheDatabase(t *testing.T) {
	s, d := newTestStore(t)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, simpleRecipe("v1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(ctx, id, simpleRecipe("v2")); err != nil { // writes revision 1
		t.Fatal(err)
	}
	if _, err := s.AddNote(ctx, id, "original note"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMadeIt(ctx, id, MadeIt{Comment: "original entry"}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		table, keep, check string // check selects one text column that must stay unchanged
		replace            string // INSERT body after the verb, `INTO table ...`
	}{
		{"revisions by id", "original", `SELECT change_kind FROM revisions`,
			`INTO revisions (id, recipe_id, version, document, change_kind, created_at, created_by) VALUES ((SELECT id FROM revisions), ?, 99, '{}', 'restore', 'now', 'me')`},
		{"revisions by version", "original", `SELECT change_kind FROM revisions`,
			`INTO revisions (recipe_id, version, document, change_kind, created_at, created_by) VALUES (?, 1, '{}', 'restore', 'now', 'me')`},
		{"notes", "original note", `SELECT body FROM notes`,
			`INTO notes (id, recipe_id, body, created_at, created_by) VALUES ((SELECT id FROM notes), ?, 'TAMPERED', 'now', 'me')`},
		{"made_it", "original entry", `SELECT comment FROM made_it`,
			`INTO made_it (id, recipe_id, made_on, comment, created_at, created_by) VALUES ((SELECT id FROM made_it), ?, '2000-01-01', 'TAMPERED', 'now', 'me')`},
	}
	for _, c := range cases {
		for _, verb := range []string{"INSERT OR REPLACE", "REPLACE"} {
			if _, err := d.Exec(verb+" "+c.replace, id); err == nil {
				t.Errorf("%s %s: succeeded, want it refused", verb, c.table)
			}
			var got string
			if err := d.QueryRow(c.check).Scan(&got); err != nil {
				t.Fatalf("%s %s: original row gone: %v", verb, c.table, err)
			}
			if c.table == "revisions by id" || c.table == "revisions by version" {
				if got != "edit" {
					t.Errorf("%s %s: change_kind = %q, want the original edit", verb, c.table, got)
				}
			} else if got != c.keep {
				t.Errorf("%s %s: row = %q, want %q", verb, c.table, got, c.keep)
			}
		}
	}
}
