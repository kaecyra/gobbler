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
// lines, and the revision and proposal documents together with their indexes.
// Call it at
// start-up, before the catalog is shared between goroutines. Without it a
// merge of an ingredient that recipes use fails on the foreign key.
func (s *Store) RegisterMergeHooks(c mergeRegistrar) {
	c.OnIngredientMerge(repointLines)
	c.OnIngredientMerge(repointRevisions)
	c.OnIngredientMerge(repointProposals)
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

// documentTable is a table of whole documents with an index of the catalog
// ingredient ids each names: revisions, and proposals. An ingredient merge
// re-points both with one function, repointDocuments, so the two cannot drift.
type documentTable struct {
	// what names the table in an error.
	what string
	// naming lists the documents the index says name the ingredient.
	naming func(ctx context.Context, q *storedb.Queries, ingredient int64) ([]storedDocument, error)
	// save replaces one document's body.
	save func(ctx context.Context, q *storedb.Queries, id int64, body string) error
	// moveIndex points the index rows of loser at winner, keeping the rows
	// that would duplicate one; dropIndex deletes the rows still naming loser.
	moveIndex func(ctx context.Context, q *storedb.Queries, loser, winner int64) error
	dropIndex func(ctx context.Context, q *storedb.Queries, loser int64) error
}

// storedDocument is one document found by documentTable.naming. kind says
// which format body is in; it is empty for a table with only one.
type storedDocument struct {
	id   int64
	kind string
	body string
}

var revisionTable = documentTable{
	what: "revision",
	naming: func(ctx context.Context, q *storedb.Queries, ing int64) ([]storedDocument, error) {
		rows, err := q.ListRevisionsNamingIngredient(ctx, ing)
		out := make([]storedDocument, len(rows))
		for i, r := range rows {
			out[i] = storedDocument{id: r.ID, body: r.Document}
		}
		return out, err
	},
	save: func(ctx context.Context, q *storedb.Queries, id int64, body string) error {
		return q.UpdateRevisionDocument(ctx, storedb.UpdateRevisionDocumentParams{Document: body, ID: id})
	},
	moveIndex: func(ctx context.Context, q *storedb.Queries, loser, winner int64) error {
		return q.RepointRevisionIndex(ctx, storedb.RepointRevisionIndexParams{WinnerID: winner, LoserID: loser})
	},
	dropIndex: func(ctx context.Context, q *storedb.Queries, loser int64) error {
		return q.DeleteRevisionIndex(ctx, loser)
	},
}

var proposalTable = documentTable{
	what: "proposal",
	naming: func(ctx context.Context, q *storedb.Queries, ing int64) ([]storedDocument, error) {
		rows, err := q.ListProposalsNamingIngredient(ctx, ing)
		out := make([]storedDocument, len(rows))
		for i, r := range rows {
			out[i] = storedDocument{id: r.ID, kind: r.Kind, body: r.Document}
		}
		return out, err
	},
	save: func(ctx context.Context, q *storedb.Queries, id int64, body string) error {
		return q.UpdateProposalDocument(ctx, storedb.UpdateProposalDocumentParams{Document: body, ID: id})
	},
	moveIndex: func(ctx context.Context, q *storedb.Queries, loser, winner int64) error {
		return q.RepointProposalIndex(ctx, storedb.RepointProposalIndexParams{WinnerID: winner, LoserID: loser})
	},
	dropIndex: func(ctx context.Context, q *storedb.Queries, loser int64) error {
		return q.DeleteProposalIndex(ctx, loser)
	},
}

func repointRevisions(ctx context.Context, tx *sql.Tx, loserID, winnerID int64) error {
	return repointDocuments(ctx, tx, revisionTable, loserID, winnerID)
}

func repointProposals(ctx context.Context, tx *sql.Tx, loserID, winnerID int64) error {
	return repointDocuments(ctx, tx, proposalTable, loserID, winnerID)
}

// repointDocuments rewrites the ingredient id inside every document of t that
// names the loser, then moves the index rows. A document that already names the
// winner keeps one index row.
func repointDocuments(ctx context.Context, tx *sql.Tx, t documentTable, loserID, winnerID int64) error {
	q := storedb.New(tx)
	affected, err := t.naming(ctx, q, loserID)
	if err != nil {
		return readErr(t.what+"s naming ingredient", err)
	}
	for _, r := range affected {
		body, err := repointBody(r.kind, []byte(r.body), loserID, winnerID)
		if err != nil {
			return fmt.Errorf("%s %d: %w", t.what, r.id, err)
		}
		if err := t.save(ctx, q, r.id, string(body)); err != nil {
			return fmt.Errorf("re-point %s %d: %w", t.what, r.id, err)
		}
	}
	if err := t.moveIndex(ctx, q, loserID, winnerID); err != nil {
		return fmt.Errorf("re-point %s index: %w", t.what, err)
	}
	if err := t.dropIndex(ctx, q, loserID); err != nil {
		return fmt.Errorf("drop duplicate %s index rows: %w", t.what, err)
	}
	return nil
}

// repointBody rewrites the loser's id in one document. A draft proposal's
// document is ingestion's (see proposal_doc.go); every other kind is the
// revision document format.
func repointBody(kind string, body []byte, loser, winner int64) ([]byte, error) {
	if ProposalKind(kind) == ProposalDraft {
		return repointDraft(body, loser, winner)
	}
	d, err := decodeDocument(body)
	if err != nil {
		return nil, err
	}
	d.repointIngredient(loser, winner)
	return encodeRaw(d)
}
