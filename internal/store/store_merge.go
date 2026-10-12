package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// mergeRegistrar is what the store needs of the catalog: somewhere to register
// its ingredient merge hooks.
type mergeRegistrar interface {
	OnIngredientMerge(h catalog.MergeHook)
}

// RegisterMergeHooks registers, with the catalog, the hooks that re-point
// every table of this package that references ingredients(id): the recipe
// lines, and the revision documents together with their index. Call it at
// start-up, before the catalog is shared between goroutines. Without it a
// merge of an ingredient that recipes use fails on the foreign key.
func (s *Store) RegisterMergeHooks(c mergeRegistrar) {
	c.OnIngredientMerge(repointLines)
	c.OnIngredientMerge(repointRevisions)
}

func repointLines(ctx context.Context, tx *sql.Tx, loserID, winnerID int64) error {
	// ingredient_lines.ingredient_id is nullable (unresolved lines), so sqlc
	// types the parameters as nullable too.
	err := storedb.New(tx).RepointIngredientLines(ctx, storedb.RepointIngredientLinesParams{
		WinnerID: sql.NullInt64{Int64: winnerID, Valid: true},
		LoserID:  sql.NullInt64{Int64: loserID, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("re-point recipe lines: %w", err)
	}
	return nil
}

// repointRevisions rewrites the ingredient id inside every revision document
// that names the loser, then moves the index rows. A revision that already
// names the winner keeps one index row.
func repointRevisions(ctx context.Context, tx *sql.Tx, loserID, winnerID int64) error {
	q := storedb.New(tx)
	affected, err := q.ListRevisionsNamingIngredient(ctx, loserID)
	if err != nil {
		return readErr("revisions naming ingredient", err)
	}
	for _, r := range affected {
		d, err := decodeDocument([]byte(r.Document))
		if err != nil {
			return fmt.Errorf("revision %d: %w", r.ID, err)
		}
		d.repointIngredient(loserID, winnerID)
		body, err := encodeRaw(d)
		if err != nil {
			return fmt.Errorf("revision %d: %w", r.ID, err)
		}
		if err := q.UpdateRevisionDocument(ctx, storedb.UpdateRevisionDocumentParams{Document: string(body), ID: r.ID}); err != nil {
			return fmt.Errorf("re-point revision %d: %w", r.ID, err)
		}
	}
	if err := q.RepointRevisionIndex(ctx, storedb.RepointRevisionIndexParams{WinnerID: winnerID, LoserID: loserID}); err != nil {
		return fmt.Errorf("re-point revision index: %w", err)
	}
	if err := q.DeleteRevisionIndex(ctx, loserID); err != nil {
		return fmt.Errorf("drop duplicate revision index rows: %w", err)
	}
	return nil
}
