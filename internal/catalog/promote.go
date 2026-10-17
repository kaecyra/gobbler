package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kaecyra/gobbler/internal/catalog/catalogdb"
)

// Promotion describes what promoting a suggested overlay entry would do, or
// did. Duplicates counts the other households' overlay entries that merge into
// it; which households they belong to is deliberately not reported
// (ADR-00018).
type Promotion struct {
	Ingredient Ingredient
	Duplicates int
}

// ListSuggestedIngredients returns the overlay entries households flagged for
// promotion, oldest first, for the operator's review. They carry no
// household. It sees every household's flags whatever the context holds; the
// caller is responsible for being the operator.
func (s *Store) ListSuggestedIngredients(ctx context.Context) ([]Ingredient, error) {
	rows, err := s.q.ListSuggestedIngredients(ctx)
	if err != nil {
		return nil, fmt.Errorf("list suggested ingredients: %w", err)
	}
	aliasRows, err := s.q.ListSuggestedIngredientAliases(ctx)
	if err != nil {
		return nil, fmt.Errorf("list aliases of suggested ingredients: %w", err)
	}
	aliases := map[int64][]string{}
	for _, r := range aliasRows {
		aliases[r.IngredientID] = append(aliases[r.IngredientID], r.Alias)
	}
	out := make([]Ingredient, 0, len(rows))
	for _, r := range rows {
		i := ingredientFrom(r)
		i.Aliases = aliases[i.ID]
		out = append(out, i)
	}
	return out, nil
}

// PreviewPromotion reports what promoting the suggested entry id would merge,
// without changing anything. It returns ErrNotFound if id is not an overlay
// entry a household suggested.
func (s *Store) PreviewPromotion(ctx context.Context, id int64) (Promotion, error) {
	e, err := s.overlayEntry(ctx, s.q, id)
	if err != nil {
		return Promotion{}, err
	}
	dups, err := s.q.ListPromotionDuplicates(ctx, id)
	if err != nil {
		return Promotion{}, fmt.Errorf("list duplicates of ingredient %d: %w", id, err)
	}
	return Promotion{Ingredient: e, Duplicates: len(dups)}, nil
}

// PromoteIngredient moves the suggested overlay entry id into the central layer in one
// transaction (ADR-00018). The entry keeps its id, so the rows of the household
// it came from need no rewrite. Every other household's overlay entry that
// duplicates it, by exact name or alias, is merged into it: that household's
// rows are re-pointed through the merge hooks, and its aliases and reviewed
// aisle stay as its own aliases and aisle override on the promoted entry. It
// returns ErrConflict if the entry's name or an alias is already central, and
// ErrNotFound if id is not an overlay entry. Callers must be the operator.
func (s *Store) PromoteIngredient(ctx context.Context, id int64) (Promotion, error) {
	var p Promotion
	err := s.withTx(ctx, func(q *catalogdb.Queries, tx *sql.Tx) error {
		// Existence check; the write lock is already held from begin (db.WithTx).
		n, err := q.LockIngredient(ctx, id)
		if err != nil {
			return fmt.Errorf("lock ingredient %d: %w", id, err)
		}
		if err := requireAffected(n, "ingredient", id); err != nil {
			return err
		}
		e, err := s.overlayEntry(ctx, q, id)
		if err != nil {
			return err
		}
		for _, term := range append([]string{e.Name}, e.Aliases...) {
			if _, ok, err := findIngredient(ctx, q, 0, term); err != nil {
				return err
			} else if ok {
				return fmt.Errorf("%w: %q is already in the central catalog", ErrConflict, term)
			}
		}
		dups, err := q.ListPromotionDuplicates(ctx, id)
		if err != nil {
			return fmt.Errorf("list duplicates of ingredient %d: %w", id, err)
		}
		if _, err := q.PromoteIngredient(ctx, id); err != nil {
			return wrapWrite(fmt.Sprintf("promote ingredient %d", id), err)
		}
		if err := q.PromoteIngredientAliases(ctx, id); err != nil {
			return wrapWrite(fmt.Sprintf("promote aliases of ingredient %d", id), err)
		}
		for _, d := range dups {
			loser, err := q.GetOverlayIngredient(ctx, d.ID)
			if err != nil {
				return fmt.Errorf("read duplicate ingredient %d: %w", d.ID, err)
			}
			if err := s.fold(ctx, q, tx, id, loser); err != nil {
				return fmt.Errorf("merge duplicate of ingredient %d: %w", id, err)
			}
		}
		p.Duplicates = len(dups)
		p.Ingredient, err = getIngredient(ctx, q, 0, id)
		return err
	})
	if err != nil {
		return Promotion{}, err
	}
	return p, nil
}

// overlayEntry reads the suggested overlay entry id whichever household holds
// it, with its aliases. An entry nobody suggested is ErrNotFound: the operator
// reviews what households chose to offer, and an id is not a way to browse
// what they did not.
func (s *Store) overlayEntry(ctx context.Context, q *catalogdb.Queries, id int64) (Ingredient, error) {
	row, err := q.GetOverlayIngredient(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Ingredient{}, fmt.Errorf("%w: ingredient %d", ErrNotFound, id)
	}
	if err != nil {
		return Ingredient{}, fmt.Errorf("read ingredient %d: %w", id, err)
	}
	e := ingredientFrom(row)
	if !e.Suggested {
		return Ingredient{}, fmt.Errorf("%w: ingredient %d", ErrNotFound, id)
	}
	if e.Aliases, err = q.ListOverlayIngredientAliases(ctx, id); err != nil {
		return Ingredient{}, fmt.Errorf("read aliases of ingredient %d: %w", id, err)
	}
	return e, nil
}
