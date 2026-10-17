package store

import (
	"errors"
	"testing"
)

// Obligation 2 of ADR-00022 (the pin): a note is pinned as a tip and unpinned,
// only by the household that owns the recipe.
func TestPinAndUnpinANote(t *testing.T) {
	s, _ := newTestStore(t)
	a, b := authedCtx(t), otherCtx(t)
	id, err := s.Create(a, simpleRecipe("Soup"))
	if err != nil {
		t.Fatal(err)
	}
	one, err := s.AddNote(a, id, "salt early")
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.AddNote(a, id, "use stock")
	if err != nil {
		t.Fatal(err)
	}
	pinned := func() map[int64]bool {
		notes, err := s.ListNotes(a, id)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]bool{}
		for _, n := range notes {
			out[n.ID] = n.Pinned
		}
		return out
	}
	if p := pinned(); p[one.ID] || p[two.ID] {
		t.Fatalf("a new note is pinned: %v", p)
	}

	if err := s.SetNotePinned(a, id, two.ID, true); err != nil {
		t.Fatal(err)
	}
	if p := pinned(); p[one.ID] || !p[two.ID] {
		t.Errorf("after pinning the second note: %v", p)
	}
	if err := s.SetNotePinned(a, id, two.ID, false); err != nil {
		t.Fatal(err)
	}
	if p := pinned(); p[two.ID] {
		t.Errorf("after unpinning: %v", p)
	}

	if err := s.SetNotePinned(b, id, one.ID, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("another household pinning = %v, want ErrNotFound", err)
	}
	if err := s.SetNotePinned(a, id, 999999, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("pinning a missing note = %v, want ErrNotFound", err)
	}
	other, err := s.Create(a, simpleRecipe("Stew"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetNotePinned(a, other, one.ID, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("pinning a note through another recipe = %v, want ErrNotFound", err)
	}
	if p := pinned(); p[one.ID] {
		t.Error("a note was pinned through the wrong recipe")
	}
}
