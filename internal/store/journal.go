package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
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
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if err := lockRecipe(ctx, tx, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO notes (recipe_id, body, created_at, created_by) VALUES (?, ?, ?, ?)`,
			id, body, formatTime(now), who)
		if err != nil {
			return fmt.Errorf("write note on recipe %d: %w", id, err)
		}
		n.ID, err = res.LastInsertId()
		return err
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
	if _, err := loadInfo(ctx, s.db, id); err != nil {
		return nil, err
	}
	var out []Note
	err := collect(ctx, s.db, "notes", `SELECT id, body, created_at, created_by FROM notes WHERE recipe_id = ? ORDER BY id`,
		[]any{id}, func(rows *sql.Rows) error {
			n := Note{RecipeID: id}
			var at string
			if err := rows.Scan(&n.ID, &n.Body, &at, &n.CreatedBy); err != nil {
				return err
			}
			var err error
			if n.CreatedAt, err = parseTime(at); err != nil {
				return err
			}
			out = append(out, n)
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
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if err := lockRecipe(ctx, tx, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO made_it (recipe_id, made_on, rating, comment, created_at, created_by) VALUES (?, ?, ?, ?, ?, ?)`,
			id, out.On.Format(dateLayout), nullInt(e.Rating), e.Comment, formatTime(now), who)
		if err != nil {
			return fmt.Errorf("write made-it entry on recipe %d: %w", id, err)
		}
		out.ID, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return MadeIt{}, err
	}
	return out, nil
}

// ListMadeIt returns the made-it log of recipe id, oldest date first. A
// missing recipe is ErrNotFound.
func (s *Store) ListMadeIt(ctx context.Context, id int64) ([]MadeIt, error) {
	if _, err := loadInfo(ctx, s.db, id); err != nil {
		return nil, err
	}
	var out []MadeIt
	err := collect(ctx, s.db, "made-it log",
		`SELECT id, made_on, rating, comment, created_at, created_by FROM made_it WHERE recipe_id = ? ORDER BY made_on, id`,
		[]any{id}, func(rows *sql.Rows) error {
			e := MadeIt{RecipeID: id}
			var (
				on, at string
				rating sql.NullInt64
			)
			if err := rows.Scan(&e.ID, &on, &rating, &e.Comment, &at, &e.CreatedBy); err != nil {
				return err
			}
			var err error
			if e.On, err = time.Parse(dateLayout, on); err != nil {
				return fmt.Errorf("parse stored date %q: %w", on, err)
			}
			if e.CreatedAt, err = parseTime(at); err != nil {
				return err
			}
			e.Rating = intPtr(rating)
			out = append(out, e)
			return nil
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}
