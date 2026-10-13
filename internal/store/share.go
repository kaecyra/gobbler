package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// A recipe share (ADR-00018, Sharing) is a copy of a recipe sent to another
// household. The recipe_shares row holds the document and the source snapshots
// as they were when it was sent, in a form of their own (shareDoc), so nothing
// in it points at a row of the sender: no recipe id, and no id of an ingredient
// in the sender's overlay. A line that used such an ingredient carries its name
// instead, and the recipient matches that against its own catalog when it
// accepts. Ids of the central catalog, units and equipment are the same in
// every household and are kept.

// shareDoc is the stored form of a shared recipe document.
type shareDoc struct {
	Recipe docRecipe `json:"recipe"`
	// Names holds the name of the ingredient of each line that used one of the
	// sender's own overlay entries, keyed by lineKey. Such a line has no
	// ingredient id in Recipe.
	Names map[string]string `json:"names,omitempty"`
}

// shareSnapshot is one source snapshot in a share, byte for byte.
type shareSnapshot struct {
	Kind    string `json:"kind"`
	Content []byte `json:"content"`
}

func lineKey(component, line int) string {
	return strconv.Itoa(component) + "." + strconv.Itoa(line)
}

// ShareOffer is a share in the recipient household's inbox. It carries what the
// household may see before it accepts: the name of the sending household, when
// it was sent and what the recipe is called.
type ShareOffer struct {
	ID         int64
	SenderName string
	SentAt     time.Time
	RecipeName string
	// Ingredients are the names, in reading order and without repeats, that the
	// recipient's catalog must resolve for AcceptShare: the ingredients that
	// were entries of the sender's overlay.
	Ingredients []string
}

// SendShare sends a copy of recipe id of the request's household to the
// household recipientID and returns the share's id. The copy is the recipe as
// it is now, with its source snapshots; notes, revisions, the made-it log and
// the rating stay behind. Sending again sends another copy. A recipe of another
// household is ErrNotFound. That the recipient exists is the caller's check; a
// recipient that is the sender is ErrInvalid.
func (s *Store) SendShare(ctx context.Context, id, recipientID int64) (int64, error) {
	who, err := identity(ctx)
	if err != nil {
		return 0, err
	}
	hid, err := scope(ctx)
	if err != nil {
		return 0, err
	}
	if recipientID == hid {
		return 0, fmt.Errorf("%w: a recipe cannot be shared with its own household", ErrInvalid)
	}
	var shareID int64
	err = s.withTx(ctx, func(q *storedb.Queries) error {
		if err := lockRecipe(ctx, q, hid, id); err != nil {
			return err
		}
		units, err := loadUnits(ctx, q)
		if err != nil {
			return err
		}
		r, err := loadDocument(ctx, q, hid, id, units)
		if err != nil {
			return err
		}
		doc, err := portableDocument(ctx, q, hid, r, units)
		if err != nil {
			return fmt.Errorf("share recipe %d: %w", id, err)
		}
		body, err := json.Marshal(doc)
		if err != nil {
			return fmt.Errorf("encode share of recipe %d: %w", id, err)
		}
		snaps, err := shareSnapshots(ctx, q, hid, id)
		if err != nil {
			return err
		}
		shareID, err = q.InsertRecipeShare(ctx, storedb.InsertRecipeShareParams{
			SenderHouseholdID: hid, RecipientHouseholdID: recipientID, SentBy: who,
			RecipeDocument: string(body), SourceSnapshot: snaps, CreatedAt: s.stamp(),
		})
		if err != nil {
			return wrapWrite(fmt.Sprintf("send recipe %d", id), err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return shareID, nil
}

// portableDocument turns r into its shared form: no rating (the sender's
// opinion), and no id of an ingredient the sender's household owns.
func portableDocument(ctx context.Context, q *storedb.Queries, hid int64, r recipe.Recipe, units *unitTable) (shareDoc, error) {
	body, err := encodeDocument(r, units)
	if err != nil {
		return shareDoc{}, err
	}
	d, err := decodeDocument(body)
	if err != nil {
		return shareDoc{}, err
	}
	d.Rating = nil
	doc := shareDoc{Recipe: d}
	for ci := range d.Components {
		for li := range d.Components[ci].Lines {
			l := &d.Components[ci].Lines[li]
			if l.Ingredient == 0 {
				continue
			}
			row, err := q.ShareIngredient(ctx, storedb.ShareIngredientParams{ID: l.Ingredient, Scope: sql.NullInt64{Int64: hid, Valid: true}})
			if errors.Is(err, sql.ErrNoRows) {
				return shareDoc{}, fmt.Errorf("%w: ingredient %d of the recipe is not in the catalog", ErrCorrupt, l.Ingredient)
			}
			if err != nil {
				return shareDoc{}, readErr(fmt.Sprintf("ingredient %d", l.Ingredient), err)
			}
			if !row.HouseholdID.Valid {
				continue
			}
			if doc.Names == nil {
				doc.Names = map[string]string{}
			}
			doc.Names[lineKey(ci, li)] = row.Name
			l.Ingredient = 0
		}
	}
	return doc, nil
}

// shareSnapshots returns the source snapshots of recipe id in the column form,
// or NULL when it has none.
func shareSnapshots(ctx context.Context, q *storedb.Queries, hid, id int64) (sql.NullString, error) {
	kinds, err := q.ListSnapshotKinds(ctx, storedb.ListSnapshotKindsParams{RecipeID: id, HouseholdID: hid})
	if err != nil {
		return sql.NullString{}, readErr("snapshot kinds", err)
	}
	if len(kinds) == 0 {
		return sql.NullString{}, nil
	}
	snaps := make([]shareSnapshot, 0, len(kinds))
	for _, k := range kinds {
		row, err := q.GetSnapshot(ctx, storedb.GetSnapshotParams{RecipeID: id, Kind: k, HouseholdID: hid})
		if err != nil {
			return sql.NullString{}, readErr(fmt.Sprintf("%s snapshot of recipe %d", k, id), err)
		}
		snaps = append(snaps, shareSnapshot{Kind: row.Kind, Content: row.Content})
	}
	body, err := json.Marshal(snaps)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("encode snapshots of recipe %d: %w", id, err)
	}
	return sql.NullString{String: string(body), Valid: true}, nil
}

// PendingShares returns the shares waiting for the request's household, oldest
// first. Another household's shares are never in it.
func (s *Store) PendingShares(ctx context.Context) ([]ShareOffer, error) {
	hid, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListPendingShares(ctx, hid)
	if err != nil {
		return nil, readErr("pending shares", err)
	}
	out := make([]ShareOffer, 0, len(rows))
	for _, r := range rows {
		o, err := offerOf(r.ID, r.RecipeDocument, r.SenderName, r.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

// PendingShare returns share id if it is addressed to the request's household
// and still pending, or ErrNotFound. A share of another household, one already
// resolved and one that does not exist are the same answer.
func (s *Store) PendingShare(ctx context.Context, id int64) (ShareOffer, error) {
	hid, err := scope(ctx)
	if err != nil {
		return ShareOffer{}, err
	}
	row, err := s.q.GetShare(ctx, storedb.GetShareParams{ID: id, HouseholdID: hid})
	if errors.Is(err, sql.ErrNoRows) || (err == nil && row.Status != shareStatusPending) {
		return ShareOffer{}, fmt.Errorf("%w: share %d", ErrNotFound, id)
	}
	if err != nil {
		return ShareOffer{}, readErr(fmt.Sprintf("share %d", id), err)
	}
	return offerOf(row.ID, row.RecipeDocument, row.SenderName, row.CreatedAt)
}

const (
	shareStatusPending  = "pending"
	shareStatusAccepted = "accepted"
	shareStatusDeclined = "declined"
)

func offerOf(id int64, document, sender, created string) (ShareOffer, error) {
	var doc shareDoc
	if err := json.Unmarshal([]byte(document), &doc); err != nil {
		return ShareOffer{}, fmt.Errorf("%w: share %d document: %w", ErrCorrupt, id, err)
	}
	at, err := parseTime(created)
	if err != nil {
		return ShareOffer{}, err
	}
	o := ShareOffer{ID: id, SenderName: sender, SentAt: at, RecipeName: doc.Recipe.Name}
	seen := map[string]bool{}
	for ci, c := range doc.Recipe.Components {
		for li := range c.Lines {
			if name, ok := doc.Names[lineKey(ci, li)]; ok && !seen[name] {
				seen[name] = true
				o.Ingredients = append(o.Ingredients, name)
			}
		}
	}
	return o, nil
}

// AcceptShare turns share id into a recipe of the request's household and
// returns its id. The recipe is independent of the sender's: the document and
// the source snapshots as sent, a new version 1, no notes, revisions, made-it
// log or rating, and one note recording the sending household's name and the
// date. ingredients maps each name in ShareOffer.Ingredients to the id of the
// ingredient in this household's catalog (central or its own overlay); a name
// missing from it, or an id the household cannot see, fails the call and
// changes nothing. A share that is not pending for this household is
// ErrNotFound, so it can be accepted only once.
func (s *Store) AcceptShare(ctx context.Context, id int64, ingredients map[string]int64) (int64, error) {
	who, err := identity(ctx)
	if err != nil {
		return 0, err
	}
	hid, err := scope(ctx)
	if err != nil {
		return 0, err
	}
	var recipeID int64
	err = s.withTx(ctx, func(q *storedb.Queries) error {
		// The claim is the first statement: it takes the write lock, and only one
		// of two concurrent accepts affects the row.
		now := s.stamp()
		if err := resolveShare(ctx, q, hid, id, shareStatusAccepted, now); err != nil {
			return err
		}
		row, err := q.GetShare(ctx, storedb.GetShareParams{ID: id, HouseholdID: hid})
		if err != nil {
			return readErr(fmt.Sprintf("share %d", id), err)
		}
		var doc shareDoc
		if err := json.Unmarshal([]byte(row.RecipeDocument), &doc); err != nil {
			return fmt.Errorf("%w: share %d document: %w", ErrCorrupt, id, err)
		}
		for ci := range doc.Recipe.Components {
			for li := range doc.Recipe.Components[ci].Lines {
				name, ok := doc.Names[lineKey(ci, li)]
				if !ok {
					continue
				}
				ing, ok := ingredients[name]
				if !ok || ing == 0 {
					return fmt.Errorf("accept share %d: no ingredient given for %q: %w", id, name, ErrUnknownReference)
				}
				doc.Recipe.Components[ci].Lines[li].Ingredient = ing
			}
		}
		units, err := loadUnits(ctx, q)
		if err != nil {
			return err
		}
		r, err := doc.Recipe.toRecipe(units)
		if err != nil {
			return fmt.Errorf("share %d: %w", id, err)
		}
		if err := r.Validate(); err != nil {
			return fmt.Errorf("validate shared recipe %d: %w", id, err)
		}
		if recipeID, err = s.insertRecipe(ctx, q, hid, r, nil, who); err != nil {
			return err
		}
		if err := s.insertSharedSnapshots(ctx, q, hid, recipeID, row.SourceSnapshot, who); err != nil {
			return err
		}
		at, err := parseTime(now)
		if err != nil {
			return err
		}
		note := fmt.Sprintf("Shared by %s on %s.", row.SenderName, at.UTC().Format("2006-01-02"))
		if _, err := q.InsertNote(ctx, storedb.InsertNoteParams{HouseholdID: hid, RecipeID: recipeID, Body: note, CreatedAt: now, CreatedBy: who}); err != nil {
			return wrapWrite("record where the shared recipe came from", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return recipeID, nil
}

func (s *Store) insertSharedSnapshots(ctx context.Context, q *storedb.Queries, hid, recipeID int64, column sql.NullString, who string) error {
	if !column.Valid {
		return nil
	}
	var snaps []shareSnapshot
	if err := json.Unmarshal([]byte(column.String), &snaps); err != nil {
		return fmt.Errorf("%w: share snapshots: %w", ErrCorrupt, err)
	}
	for _, sn := range snaps {
		if err := s.insertSnapshot(ctx, q, hid, recipeID, SnapshotKind(sn.Kind), sn.Content, who); err != nil {
			return err
		}
	}
	return nil
}

// DeclineShare declines share id of the request's household. Nothing enters the
// household's recipes. A share that is not pending for it is ErrNotFound.
func (s *Store) DeclineShare(ctx context.Context, id int64) error {
	if _, err := identity(ctx); err != nil {
		return err
	}
	hid, err := scope(ctx)
	if err != nil {
		return err
	}
	return s.withTx(ctx, func(q *storedb.Queries) error {
		return resolveShare(ctx, q, hid, id, shareStatusDeclined, s.stamp())
	})
}

func resolveShare(ctx context.Context, q *storedb.Queries, hid, id int64, status, at string) error {
	n, err := q.ResolveShare(ctx, storedb.ResolveShareParams{
		Status: status, ResolvedAt: sql.NullString{String: at, Valid: true}, ID: id, HouseholdID: hid,
	})
	if err != nil {
		return fmt.Errorf("resolve share %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: share %d", ErrNotFound, id)
	}
	return nil
}
