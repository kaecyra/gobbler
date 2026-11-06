package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/credential"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// ProposalRef is what history shows of the proposal behind a version: who
// proposed it, through which credential, and why (ADR-00035). It is the
// proposal's provenance without its document.
type ProposalRef struct {
	Token string
	// Reimport says the version was written by saving a re-import of the
	// recipe's source (ADR-00042); Reason then reads "Re-imported from <host>".
	Reimport bool
	Provenance
	CreatedAt time.Time
}

func optionalID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

func proposalRef(token, reason, tool, person, credKind string, credID int64, credRef, credOwner, credCreated, created string) (ProposalRef, error) {
	ref := ProposalRef{Token: token, Provenance: Provenance{Person: person, Tool: tool, Reason: reason}}
	var err error
	if ref.CreatedAt, err = parseTime(created); err != nil {
		return ProposalRef{}, err
	}
	if credKind != "" {
		c := &Credential{Kind: credKind, ID: credID, Ref: credRef, Owner: credOwner}
		// A credential recorded without its creation time has none to read.
		if credCreated != "" {
			if c.CreatedAt, err = parseTime(credCreated); err != nil {
				return ProposalRef{}, err
			}
		}
		ref.Credential = c
	}
	return ref, nil
}

// StaleAt reports whether the proposal was made against a version other than
// version, the recipe's current one. A stale proposal cannot be accepted: it
// holds the content of an older version, and accepting it would undo whatever
// was saved since (ADR-00035). Staleness is computed, never stored. A draft of
// a new recipe has no base and is never stale; a re-import is stale once the
// recipe is at a version other than the one it started from (ADR-00042).
func (p Proposal) StaleAt(version int64) bool {
	if p.Reimport != nil {
		return p.Reimport.BaseVersion != version
	}
	return p.Kind == ProposalEdit && p.BaseVersion != version
}

// ErrStaleProposal is returned by AcceptEdit when the recipe has moved on from
// the version the proposal was made against. It is also an ErrStaleVersion.
var ErrStaleProposal = fmt.Errorf("the proposal is out of date: %w", ErrStaleVersion)

// AcceptEdit saves r as the new version of the recipe that the pending proposal
// with token proposes to change, as an ordinary save under the version check,
// and marks the proposal accepted, all in one transaction. r is what the
// reviewer saved: the proposal as it stood, or as they changed it. The revision
// it writes holds the version it replaced, has change kind "proposal" and
// refers to the proposal; its author is the person on ctx (ADR-00035).
//
// A proposal that is not pending is ErrNotPending, and one made against a
// version the recipe has since left is ErrStaleProposal. expectedVersion is the
// version the reviewer's form was loaded at; one that is not the recipe's
// current version is ErrStaleVersion. Nothing is written in any of those
// cases, so a review that races an editor save leaves the recipe as it was.
func (s *Store) AcceptEdit(ctx context.Context, token string, expectedVersion int64, r recipe.Recipe) error {
	who, err := identity(ctx)
	if err != nil {
		return err
	}
	hid, err := scope(ctx)
	if err != nil {
		return err
	}
	if err := r.Validate(); err != nil {
		return fmt.Errorf("validate recipe: %w", err)
	}
	return s.withTx(ctx, func(q *storedb.Queries) error {
		row, err := q.GetProposal(ctx, storedb.GetProposalParams{Token: token, HouseholdID: hid})
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: proposal", ErrNotFound)
		}
		if err != nil {
			return readErr("proposal", err)
		}
		if ProposalKind(row.Kind) != ProposalEdit {
			return fmt.Errorf("%w: proposal is not an edit of a recipe", ErrInvalid)
		}
		if ProposalState(row.State) != ProposalPending {
			return ErrNotPending
		}
		id := row.RecipeID.Int64
		if err := lockRecipe(ctx, q, hid, id); err != nil {
			return err
		}
		cur, err := loadInfo(ctx, q, hid, id)
		if err != nil {
			return err
		}
		if cur.Version != row.BaseVersion.Int64 {
			return fmt.Errorf("%w: recipe %d is at version %d, the proposal was made against %d", ErrStaleProposal, id, cur.Version, row.BaseVersion.Int64)
		}
		if cur.Version != expectedVersion {
			return fmt.Errorf("%w: recipe %d is at version %d, not %d", ErrStaleVersion, id, cur.Version, expectedVersion)
		}
		n, err := q.SetProposalState(ctx, storedb.SetProposalStateParams{State: string(ProposalAccepted), StateAt: s.stamp(), ID: row.ID, HouseholdID: hid})
		if err != nil {
			return fmt.Errorf("accept proposal: %w", err)
		}
		if n == 0 {
			return ErrNotPending
		}
		return s.replace(ctx, q, hid, id, r, who, changeProposal, nil, row.ID)
	})
}

// Describe says who made a proposal for a person to read: the credential an
// agent used and whom it acts for, or the person alone when no credential was
// involved. A household key acts for no one person, so it is named without an
// owner.
func (p Provenance) Describe() string {
	c := p.Credential
	if c == nil {
		return p.Person
	}
	kind := c.Kind
	switch kind {
	case credential.KindAPIKey:
		kind = "API key"
	case credential.KindOAuth:
		kind = "OAuth client"
	}
	out := kind
	if c.Ref != "" {
		out += " " + c.Ref
	}
	if p.Person != "" && !apikey.IsHouseholdOwner(p.Person) {
		out += " (" + p.Person + ")"
	}
	return out
}
