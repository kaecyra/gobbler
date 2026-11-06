package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// A re-import is an ingestion draft that replaces a saved recipe instead of
// creating one (ADR-00042). It is stored as a draft proposal like any other,
// because the proposals table gives only an edit a recipe column and the
// picture candidates of ADR-00039 are kept only for drafts. The recipe and the
// version it started from are in the draft's document, under "reimport", which
// is the store's second read of that document (see proposal_doc.go). A recipe
// has at most one pending proposal of either kind: this file keeps that rule
// for re-imports, and the table's unique index keeps it for edits.

// Reimport names the recipe a re-import draft replaces and the version of it
// the draft started from.
type Reimport struct {
	RecipeID, BaseVersion int64
}

// draftReimport reads the re-import marker of a draft document: nil when it is
// a draft of a new recipe.
func draftReimport(doc []byte) (*Reimport, error) {
	var top struct {
		Reimport *struct {
			Recipe      int64 `json:"recipe"`
			BaseVersion int64 `json:"base_version"`
		} `json:"reimport"`
	}
	if err := json.Unmarshal(doc, &top); err != nil {
		return nil, fmt.Errorf("decode draft document: %w", err)
	}
	if top.Reimport == nil {
		return nil, nil //nolint:nilnil // nil is "not a re-import"
	}
	if top.Reimport.Recipe < 1 || top.Reimport.BaseVersion < 1 {
		return nil, fmt.Errorf("draft document reimport names recipe %d at version %d", top.Reimport.Recipe, top.Reimport.BaseVersion)
	}
	return &Reimport{RecipeID: top.Reimport.Recipe, BaseVersion: top.Reimport.BaseVersion}, nil
}

// WithObjects returns s deleting the picture candidates of the re-import
// drafts that a new proposal, or the recipe's deletion, ends (ADR-00042). Without
// it their objects stay until the sweep prunes the proposals, which is the
// remover SweepWithPictures is given. Call it before sharing s.
func (s *Store) WithObjects(r ObjectRemover) *Store {
	c := *s
	c.objects = r
	return &c
}

// endPendingProposals ends what is waiting to change recipe id, so a new
// proposal can take its place: the pending edit and the pending re-import
// drafts become superseded. It returns the object keys of the re-imports'
// picture candidates, and deletes their rows, when s has a remover to delete the
// objects with; the caller removes the objects after the transaction commits.
func (s *Store) endPendingProposals(ctx context.Context, q *storedb.Queries, hid, id int64, now string) ([]string, error) {
	if _, err := q.SetPendingEditSuperseded(ctx, storedb.SetPendingEditSupersededParams{StateAt: now, RecipeID: nullID(id), HouseholdID: hid}); err != nil {
		return nil, fmt.Errorf("supersede the pending proposal for recipe %d: %w", id, err)
	}
	return s.endPendingReimports(ctx, q, hid, id, now)
}

func (s *Store) endPendingReimports(ctx context.Context, q *storedb.Queries, hid, id int64, now string) ([]string, error) {
	pending, err := q.ListPendingReimports(ctx, storedb.ListPendingReimportsParams{HouseholdID: hid, RecipeID: id})
	if err != nil {
		return nil, readErr(fmt.Sprintf("pending re-imports of recipe %d", id), err)
	}
	var keys []string
	for _, p := range pending {
		if _, err := q.SetProposalState(ctx, storedb.SetProposalStateParams{State: string(ProposalSuperseded), StateAt: now, ID: p.ID, HouseholdID: hid}); err != nil {
			return nil, fmt.Errorf("supersede the pending re-import of recipe %d: %w", id, err)
		}
		if s.objects == nil {
			continue
		}
		ks, err := q.ListProposalCandidateKeys(ctx, storedb.ListProposalCandidateKeysParams{HouseholdID: hid, ProposalID: p.ID})
		if err != nil {
			return nil, readErr(fmt.Sprintf("candidates of proposal %d", p.ID), err)
		}
		if err := q.DeleteProposalCandidates(ctx, storedb.DeleteProposalCandidatesParams{HouseholdID: hid, ProposalID: p.ID}); err != nil {
			return nil, fmt.Errorf("delete candidates of proposal %d: %w", p.ID, err)
		}
		keys = append(keys, ks...)
	}
	return keys, nil
}

// removeObjects deletes keys once the transaction that dropped their rows has
// committed.
func (s *Store) removeObjects(ctx context.Context, keys []string) {
	if s.objects != nil && len(keys) > 0 {
		s.objects.Remove(ctx, keys)
	}
}

// PendingReimport returns the pending re-import draft of recipe id, if it has
// one.
func (s *Store) PendingReimport(ctx context.Context, id int64) (Proposal, bool, error) {
	hid, err := scope(ctx)
	if err != nil {
		return Proposal{}, false, err
	}
	var p Proposal
	var found bool
	err = s.read(ctx, func(q *storedb.Queries) error {
		pending, err := q.ListPendingReimports(ctx, storedb.ListPendingReimportsParams{HouseholdID: hid, RecipeID: id})
		if err != nil {
			return readErr("pending re-import", err)
		}
		if len(pending) == 0 {
			return nil
		}
		found = true
		p, err = loadProposal(ctx, q, hid, pending[0].Token)
		return err
	})
	return p, found, err
}

// AcceptReimport saves r over the recipe that the pending re-import draft with
// token replaces, as an ordinary save, and marks the draft accepted, attaching
// the draft, all in one transaction. The revision it writes holds the version it replaced, has change
// kind "proposal" and refers to the draft; its author is the person on ctx
// (ADR-00042). The journal, made-it entries, shares and history stay with the
// recipe. The recipe's tags and rating are the household's, not the source's,
// and are kept: r's are replaced by the recipe's own.
//
// A draft that is not pending is ErrNotPending. A recipe that has moved on
// from the version the re-import started from is ErrStaleProposal (an
// ErrStaleVersion), and nothing is written. It returns the recipe's id.
func (s *Store) AcceptReimport(ctx context.Context, token string, r recipe.Recipe) (int64, error) {
	who, err := identity(ctx)
	if err != nil {
		return 0, err
	}
	hid, err := scope(ctx)
	if err != nil {
		return 0, err
	}
	if err := r.Validate(); err != nil {
		return 0, fmt.Errorf("validate recipe: %w", err)
	}
	var id int64
	err = s.withTx(ctx, func(q *storedb.Queries) error {
		row, err := q.GetProposal(ctx, storedb.GetProposalParams{Token: token, HouseholdID: hid})
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: proposal", ErrNotFound)
		}
		if err != nil {
			return readErr("proposal", err)
		}
		re, err := draftReimport([]byte(row.Document))
		if err != nil {
			return fmt.Errorf("%w: %w", ErrCorrupt, err)
		}
		if ProposalKind(row.Kind) != ProposalDraft || re == nil {
			return fmt.Errorf("%w: proposal is not a re-import of a recipe", ErrInvalid)
		}
		if ProposalState(row.State) != ProposalPending {
			return ErrNotPending
		}
		if !row.SnapshotID.Valid {
			return fmt.Errorf("%w: re-import has no source snapshot to attach", ErrCorrupt)
		}
		id = re.RecipeID
		if err := lockRecipe(ctx, q, hid, id); err != nil {
			return err
		}
		cur, err := loadInfo(ctx, q, hid, id)
		if err != nil {
			return err
		}
		if cur.Version != re.BaseVersion {
			return fmt.Errorf("%w: recipe %d is at version %d, the re-import started from %d", ErrStaleProposal, id, cur.Version, re.BaseVersion)
		}
		units, err := loadUnits(ctx, q)
		if err != nil {
			return err
		}
		live, err := loadDocument(ctx, q, hid, id, units)
		if err != nil {
			return err
		}
		r.Tags, r.Rating = live.Tags, live.Rating
		n, err := q.AcceptDraft(ctx, storedb.AcceptDraftParams{ResultRecipeID: nullID(id), StateAt: s.stamp(), ID: row.ID, HouseholdID: hid})
		if err != nil {
			return fmt.Errorf("accept re-import: %w", err)
		}
		if n != 1 {
			return ErrNotPending
		}
		if err := s.replace(ctx, q, hid, id, r, who, changeProposal, nil, row.ID); err != nil {
			return err
		}
		// The new snapshot stays on the accepted draft (proposals.snapshot_id).
		// Attaching it to the recipe beside the first waits on the ruling on
		// source_snapshots' UNIQUE (recipe_id, kind) (ADR-00042 obligation 2).
		return nil
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}
