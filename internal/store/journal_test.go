package store

import (
	"errors"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/auth"
)

func TestNotesAreTimestampedAttributedAndListed(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, simpleRecipe("x"))
	if err != nil {
		t.Fatal(err)
	}

	first, err := s.AddNote(ctx, id, "needs more salt")
	if err != nil {
		t.Fatalf("AddNote: %v", err)
	}
	if _, err := s.AddNote(ctx, id, "baked 5 minutes longer"); err != nil {
		t.Fatal(err)
	}

	notes, err := s.ListNotes(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 || notes[0].Body != "needs more salt" || notes[1].Body != "baked 5 minutes longer" {
		t.Fatalf("notes = %+v", notes)
	}
	if notes[0].CreatedBy != auth.DevEmail || notes[0].ID != first.ID || !notes[0].CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("note metadata = %+v, added %+v", notes[0], first)
	}
	if !notes[1].CreatedAt.After(notes[0].CreatedAt) {
		t.Error("notes not timestamped in order")
	}
}

func TestNoteValidationAndMissingRecipe(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, simpleRecipe("x"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNote(ctx, id, "  \n"); !errors.Is(err, ErrInvalid) {
		t.Errorf("blank note = %v, want ErrInvalid", err)
	}
	if _, err := s.AddNote(ctx, id+9, "hi"); !errors.Is(err, ErrNotFound) {
		t.Errorf("note on missing recipe = %v, want ErrNotFound", err)
	}
	if _, err := s.ListNotes(ctx, id+9); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListNotes(missing) = %v, want ErrNotFound", err)
	}
	if _, err := s.AddMadeIt(ctx, id+9, MadeIt{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("made-it on missing recipe = %v, want ErrNotFound", err)
	}
	if _, err := s.ListMadeIt(ctx, id+9); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListMadeIt(missing) = %v, want ErrNotFound", err)
	}
}

func TestMadeItEntries(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, simpleRecipe("x"))
	if err != nil {
		t.Fatal(err)
	}

	late := time.Date(2026, 2, 20, 23, 30, 0, 0, time.FixedZone("x", -8*3600))
	if _, err := s.AddMadeIt(ctx, id, MadeIt{On: late, Rating: ptr(5), Comment: "kids loved it"}); err != nil {
		t.Fatalf("AddMadeIt: %v", err)
	}
	if _, err := s.AddMadeIt(ctx, id, MadeIt{On: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	today, err := s.AddMadeIt(ctx, id, MadeIt{})
	if err != nil {
		t.Fatal(err)
	}
	if today.On.Format("2006-01-02") != "2026-03-01" {
		t.Errorf("default date = %v, want the clock's day", today.On)
	}

	log, err := s.ListMadeIt(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 3 {
		t.Fatalf("log = %+v", log)
	}
	// Oldest date first; the date is the one written, whatever the zone.
	if log[0].On.Format("2006-01-02") != "2026-01-05" || log[1].On.Format("2006-01-02") != "2026-02-20" {
		t.Errorf("dates = %v, %v", log[0].On, log[1].On)
	}
	if log[0].Rating != nil || log[0].Comment != "" {
		t.Errorf("optional fields should be empty: %+v", log[0])
	}
	if log[1].Rating == nil || *log[1].Rating != 5 || log[1].Comment != "kids loved it" || log[1].CreatedBy != auth.DevEmail {
		t.Errorf("entry = %+v", log[1])
	}
}
