package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// dateLayout is the stored form of a made-it date.
const dateLayout = "2006-01-02"

// Note is a timestamped free-text note on a recipe, many per recipe.
type Note struct {
	ID        int64
	RecipeID  int64
	Body      string
	CreatedAt time.Time
	// CreatedBy is the verified identity that wrote it.
	CreatedBy string
}

// MadeIt is one entry in a recipe's made-it log. On is a calendar date; the
// time of day and zone of the value passed to AddMadeIt are dropped.
type MadeIt struct {
	ID        int64
	RecipeID  int64
	On        time.Time
	Rating    *int
	Comment   string
	CreatedAt time.Time
	CreatedBy string
}

// AddNote adds a note to recipe id. An empty body is ErrInvalid; a missing
// recipe is ErrNotFound.
func (s *Store) AddNote(ctx context.Context, id int64, body string) (Note, error) {
	who, err := identity(ctx)
	if err != nil {
		return Note{}, err
	}
	if strings.TrimSpace(body) == "" {
		return Note{}, fmt.Errorf("%w: note is empty", ErrInvalid)
	}
	n := Note{RecipeID: id, Body: body, CreatedBy: who}
	now := s.now().UTC()
	err = s.withTx(ctx, func(q *storedb.Queries) (err error) {
		if err := lockRecipe(ctx, q, id); err != nil {
			return err
		}
		n.ID, err = q.InsertNote(ctx, storedb.InsertNoteParams{RecipeID: id, Body: body, CreatedAt: formatTime(now), CreatedBy: who})
		if err != nil {
			return fmt.Errorf("write note on recipe %d: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return Note{}, err
	}
	n.CreatedAt = now
	return n, nil
}

// ListNotes returns the notes on recipe id, oldest first. A missing recipe is
// ErrNotFound.
func (s *Store) ListNotes(ctx context.Context, id int64) ([]Note, error) {
	var out []Note
	err := s.read(ctx, func(q *storedb.Queries) error {
		if _, err := loadInfo(ctx, q, id); err != nil {
			return err
		}
		rows, err := q.ListNotes(ctx, id)
		if err != nil {
			return readErr("notes", err)
		}
		for _, r := range rows {
			n := Note{ID: r.ID, RecipeID: id, Body: r.Body, CreatedBy: r.CreatedBy}
			if n.CreatedAt, err = parseTime(r.CreatedAt); err != nil {
				return readErr("notes", err)
			}
			out = append(out, n)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AddMadeIt logs that recipe id was made. A zero On means today; the other
// fields are optional. A missing recipe is ErrNotFound.
func (s *Store) AddMadeIt(ctx context.Context, id int64, e MadeIt) (MadeIt, error) {
	who, err := identity(ctx)
	if err != nil {
		return MadeIt{}, err
	}
	now := s.now().UTC()
	if e.On.IsZero() {
		e.On = now
	}
	out := MadeIt{RecipeID: id, Rating: e.Rating, Comment: e.Comment, CreatedAt: now, CreatedBy: who}
	out.On, err = time.Parse(dateLayout, e.On.Format(dateLayout))
	if err != nil {
		return MadeIt{}, fmt.Errorf("made-it date: %w", err)
	}
	err = s.withTx(ctx, func(q *storedb.Queries) (err error) {
		if err := lockRecipe(ctx, q, id); err != nil {
			return err
		}
		out.ID, err = q.InsertMadeIt(ctx, storedb.InsertMadeItParams{
			RecipeID: id, MadeOn: out.On.Format(dateLayout), Rating: nullInt(e.Rating),
			Comment: e.Comment, CreatedAt: formatTime(now), CreatedBy: who,
		})
		if err != nil {
			return fmt.Errorf("write made-it entry on recipe %d: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return MadeIt{}, err
	}
	return out, nil
}

// ListMadeIt returns the made-it log of recipe id, oldest date first. A
// missing recipe is ErrNotFound.
func (s *Store) ListMadeIt(ctx context.Context, id int64) ([]MadeIt, error) {
	var out []MadeIt
	err := s.read(ctx, func(q *storedb.Queries) error {
		if _, err := loadInfo(ctx, q, id); err != nil {
			return err
		}
		rows, err := q.ListMadeIt(ctx, id)
		if err != nil {
			return readErr("made-it log", err)
		}
		for _, r := range rows {
			e := MadeIt{ID: r.ID, RecipeID: id, Comment: r.Comment, CreatedBy: r.CreatedBy, Rating: intPtr(r.Rating)}
			if e.On, err = time.Parse(dateLayout, r.MadeOn); err != nil {
				return readErr("made-it log", fmt.Errorf("parse stored date %q: %w", r.MadeOn, err))
			}
			if e.CreatedAt, err = parseTime(r.CreatedAt); err != nil {
				return readErr("made-it log", err)
			}
			out = append(out, e)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
