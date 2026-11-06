package store

import (
	"errors"
	"testing"

	"github.com/kaecyra/gobbler/internal/recipe"
)

func TestAuthorNotesSurviveEditRestoreCloneAndAShare(t *testing.T) {
	s, d := newTestStore(t)
	a, b := authedCtx(t), otherCtx(t)
	notes := func(c recipe.Recipe, n ...recipe.AuthorNote) recipe.Recipe { c.AuthorNotes = n; return c }

	first := []recipe.AuthorNote{{Title: "Honey", Text: "Not for infants."}, {Text: "Keeps three days."}}
	id, err := s.Create(a, notes(simpleRecipe("Katsu"), first...))
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(a, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AuthorNotes) != 2 || got.AuthorNotes[0] != first[0] || got.AuthorNotes[1] != first[1] {
		t.Fatalf("after create = %+v, want %+v in order", got.AuthorNotes, first)
	}

	// An edit replaces the list: a reorder, an edit and a removal all land.
	second := []recipe.AuthorNote{{Text: "Keeps three days."}}
	if err := s.Update(a, id, notes(simpleRecipe("Katsu"), second...)); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Get(a, id); len(got.AuthorNotes) != 1 || got.AuthorNotes[0] != second[0] {
		t.Errorf("after an edit = %+v, want %+v", got.AuthorNotes, second)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM recipe_author_notes`); n != 1 {
		t.Errorf("recipe_author_notes has %d rows after the edit, want 1 (old rows must be replaced)", n)
	}
	if old, err := s.GetVersion(a, id, 1); err != nil || len(old.AuthorNotes) != 2 || old.AuthorNotes[0] != first[0] {
		t.Errorf("version 1 notes = %+v (%v), want the first list", old.AuthorNotes, err)
	}

	if err := s.Restore(a, id, 1); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Get(a, id); len(got.AuthorNotes) != 2 || got.AuthorNotes[0] != first[0] {
		t.Errorf("after restoring version 1 = %+v", got.AuthorNotes)
	}

	cloneID, err := s.Clone(a, id)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Get(a, cloneID); len(got.AuthorNotes) != 2 || got.AuthorNotes[1] != first[1] {
		t.Errorf("clone = %+v", got.AuthorNotes)
	}
	shareID, err := s.SendShare(a, id, otherHouseholdID)
	if err != nil {
		t.Fatal(err)
	}
	copyID, err := s.AcceptShare(b, shareID, fixed(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Get(b, copyID); len(got.AuthorNotes) != 2 || got.AuthorNotes[0] != first[0] {
		t.Errorf("shared copy = %+v", got.AuthorNotes)
	}

	if err := s.Delete(a, id); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM recipe_author_notes WHERE recipe_id = ?`, id); n != 0 {
		t.Errorf("%d author note rows left after delete", n)
	}
}

func TestInvalidAuthorNoteWritesNothing(t *testing.T) {
	s, d := newTestStore(t)
	a := authedCtx(t)
	bad := simpleRecipe("Katsu")
	bad.AuthorNotes = []recipe.AuthorNote{{Text: "One.\n\nTwo."}}
	if _, err := s.Create(a, bad); !errors.Is(err, recipe.ErrInvalidAuthorNote) {
		t.Errorf("Create = %v, want ErrInvalidAuthorNote", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM recipes`); n != 0 {
		t.Errorf("%d recipes after a refused create", n)
	}
}
