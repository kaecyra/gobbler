package store

import (
	"errors"
	"testing"
)

// ADR-00037 obligations 1 to 3: a note's body is edited in place and the note
// can be deleted, only through its own recipe and household, and neither
// writes a revision.
func TestANoteIsEditedInPlace(t *testing.T) {
	s, _ := newTestStore(t)
	a, b := authedCtx(t), otherCtx(t)
	id, err := s.Create(a, simpleRecipe("Toast"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.AddNote(a, id, "burns under the grill", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateNoteBody(a, id, n.ID, "watch it under the grill"); err != nil {
		t.Fatal(err)
	}
	notes, err := s.ListNotes(a, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Fatalf("notes = %+v, want one", notes)
	}
	got := notes[0]
	if got.Body != "watch it under the grill" || got.ID != n.ID || got.CreatedBy != n.CreatedBy ||
		!got.CreatedAt.Equal(n.CreatedAt) || !got.Pinned {
		t.Errorf("after the edit = %+v, want the new body and everything else of %+v", got, n)
	}
	// Search follows the edit.
	wantNames(t, mustSearch(t, s, "watch"), "Toast")
	wantNames(t, mustSearch(t, s, "burns"))

	for name, body := range map[string]string{"empty": "", "whitespace": " \n\t"} {
		if err := s.UpdateNoteBody(a, id, n.ID, body); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s body = %v, want ErrInvalid", name, err)
		}
	}
	if err := s.UpdateNoteBody(b, id, n.ID, "hijacked"); !errors.Is(err, ErrNotFound) {
		t.Errorf("another household editing = %v, want ErrNotFound", err)
	}
	other, err := s.Create(a, simpleRecipe("Stew"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateNoteBody(a, other, n.ID, "wrong recipe"); !errors.Is(err, ErrNotFound) {
		t.Errorf("editing through another recipe = %v, want ErrNotFound", err)
	}
	if err := s.UpdateNoteBody(a, id, 999999, "nowhere"); !errors.Is(err, ErrNotFound) {
		t.Errorf("editing a missing note = %v, want ErrNotFound", err)
	}
	if notes, err = s.ListNotes(a, id); err != nil || notes[0].Body != "watch it under the grill" {
		t.Errorf("a refused edit changed the note: %+v, %v", notes, err)
	}
	revs, err := s.ListRevisions(a, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 0 {
		t.Errorf("editing a note wrote revisions: %+v", revs)
	}
}

func TestANoteIsDeleted(t *testing.T) {
	s, _ := newTestStore(t)
	a, b := authedCtx(t), otherCtx(t)
	id, err := s.Create(a, simpleRecipe("Toast"))
	if err != nil {
		t.Fatal(err)
	}
	gone, err := s.AddNote(a, id, "burns under the grill", true)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := s.AddNote(a, id, "use rye", false)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Create(a, simpleRecipe("Stew"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteNote(b, id, gone.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another household deleting = %v, want ErrNotFound", err)
	}
	if err := s.DeleteNote(a, other, gone.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting through another recipe = %v, want ErrNotFound", err)
	}
	if err := s.DeleteNote(a, id, gone.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteNote(a, id, gone.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting it again = %v, want ErrNotFound", err)
	}
	notes, err := s.ListNotes(a, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].ID != kept.ID {
		t.Errorf("notes = %+v, want only %d", notes, kept.ID)
	}
	wantNames(t, mustSearch(t, s, "grill"))
	revs, err := s.ListRevisions(a, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 0 {
		t.Errorf("deleting a note wrote revisions: %+v", revs)
	}
}
