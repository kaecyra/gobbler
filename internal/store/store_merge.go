package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kaecyra/gobbler/internal/catalog"
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
	if _, err := tx.ExecContext(ctx, `UPDATE ingredient_lines SET ingredient_id = ? WHERE ingredient_id = ?`, winnerID, loserID); err != nil {
		return fmt.Errorf("re-point recipe lines: %w", err)
	}
	return nil
}

// repointRevisions rewrites the ingredient id inside every revision document
// that names the loser, then moves the index rows. A revision that already
// names the winner keeps one index row.
func repointRevisions(ctx context.Context, tx *sql.Tx, loserID, winnerID int64) error {
	type row struct {
		id   int64
		body string
	}
	var affected []row
	err := collect(ctx, tx, "revisions naming ingredient",
		`SELECT r.id, r.document FROM revisions r JOIN revision_ingredients ri ON ri.revision_id = r.id WHERE ri.ingredient_id = ?`,
		[]any{loserID}, func(rows *sql.Rows) error {
			var r row
			if err := rows.Scan(&r.id, &r.body); err != nil {
				return err
			}
			affected = append(affected, r)
			return nil
		})
	if err != nil {
		return err
	}
	for _, r := range affected {
		d, err := decodeDocument([]byte(r.body))
		if err != nil {
			return fmt.Errorf("revision %d: %w", r.id, err)
		}
		d.repointIngredient(loserID, winnerID)
		body, err := encodeRaw(d)
		if err != nil {
			return fmt.Errorf("revision %d: %w", r.id, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE revisions SET document = ? WHERE id = ?`, string(body), r.id); err != nil {
			return fmt.Errorf("re-point revision %d: %w", r.id, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE OR IGNORE revision_ingredients SET ingredient_id = ? WHERE ingredient_id = ?`, winnerID, loserID); err != nil {
		return fmt.Errorf("re-point revision index: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM revision_ingredients WHERE ingredient_id = ?`, loserID); err != nil {
		return fmt.Errorf("drop duplicate revision index rows: %w", err)
	}
	return nil
}
