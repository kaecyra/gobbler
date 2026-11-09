package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kaecyra/gobbler/internal/store"
)

// A draft of a new recipe, or of a re-import of a saved recipe (ADR-00042),
// waits for review as a pending proposal in the
// proposals table (ADR-00035), under a random token, until the person confirms
// or discards it or a year passes. That replaces the in-memory table drafts
// were held in, which expired after an hour and was emptied by a restart.
//
// Every source of drafts shares the table, so the review screen finds a draft
// whoever made it: a person on the ingest form, or an agent over MCP
// (ADR-00034). A draft belongs to the household that made it (ADR-00018): every
// call names the household on its context, and a token held by another
// household finds nothing, as if it were unknown.

// ErrDraftGone is returned (wrapped) for a token that names no pending draft of
// the household: unknown, another household's, already confirmed or discarded,
// expired, or a proposal that is not an ingestion draft.
var ErrDraftGone = errors.New("that draft is no longer available")

// DraftStore is what Drafts calls on the proposals table; *store.Store
// satisfies it.
type DraftStore interface {
	ProposeDraft(ctx context.Context, in store.DraftInput) (string, error)
	GetProposal(ctx context.Context, token string) (store.Proposal, error)
	ReplaceDraft(ctx context.Context, token string, doc []byte) error
	ReviseDraft(ctx context.Context, token string, doc []byte, revision, reason string) error
	PendingDraftList(ctx context.Context) ([]store.DraftSummary, error)
	RejectProposal(ctx context.Context, token string) error
	PendingDrafts(ctx context.Context) (int, error)
}

// Drafts holds the drafts awaiting review.
type Drafts struct {
	store DraftStore
	// pictures fetches and holds URL drafts' picture candidates; nil holds none.
	pictures Pictures
}

// NewDrafts returns the drafts held in s.
func NewDrafts(s DraftStore) *Drafts { return &Drafts{store: s} }

// Add holds d for the household on ctx and returns the token that retrieves it.
// The household holds at most store.MaxPendingDrafts drafts of new recipes:
// past that Add returns store.ErrDraftLimit and holds nothing, and no older
// draft is dropped. A re-import (d.Reimport set) is not counted or refused for
// the limit; it supersedes the recipe's pending edit or re-import. d's
// source is stored with it as a source snapshot that confirming attaches to the
// recipe; the photo behind an OCR source is not stored.
func (d *Drafts) Add(ctx context.Context, dr Draft, by store.Provenance) (string, error) {
	doc, err := EncodeDraft(dr)
	if err != nil {
		return "", fmt.Errorf("encode draft: %w", err)
	}
	token, err := d.store.ProposeDraft(ctx, store.DraftInput{
		Document:   doc,
		Source:     store.SnapshotInput{Kind: dr.Source.Kind, Content: dr.Source.Content, ParserVersion: dr.ParserVersion},
		Provenance: by,
	})
	if err != nil {
		return "", fmt.Errorf("hold draft: %w", err)
	}
	d.fetchCandidates(ctx, token, dr)
	return token, nil
}

// Full reports whether the household on ctx already holds the most drafts it
// may, so a caller can refuse before spending a fetch or an LLM call on a draft
// Add would turn away. Add's own check is the authority; this one is early and
// may be overtaken by a racing request.
func (d *Drafts) Full(ctx context.Context) (bool, error) {
	n, err := d.store.PendingDrafts(ctx)
	if err != nil {
		return false, fmt.Errorf("count pending drafts: %w", err)
	}
	return n >= store.MaxPendingDrafts, nil
}

// Get returns the pending draft for token, or ErrDraftGone.
func (d *Drafts) Get(ctx context.Context, token string) (Draft, error) {
	p, err := d.store.GetProposal(ctx, token)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return Draft{}, fmt.Errorf("%w: %w", ErrDraftGone, err)
	case err != nil:
		return Draft{}, fmt.Errorf("read draft: %w", err)
	case p.Kind != store.ProposalDraft || p.State != store.ProposalPending || p.Snapshot == nil:
		return Draft{}, fmt.Errorf("%w: it is %s", ErrDraftGone, p.State)
	}
	dr, err := DecodeDraft(p.Document, *p.Snapshot)
	if err != nil {
		return Draft{}, fmt.Errorf("read draft: %w", err)
	}
	dr.ProposalID = p.ID
	dr.Revision, dr.Reason = store.DraftRevision(p.Document), p.Reason
	if d.pictures != nil {
		cs, err := d.pictures.Candidates(ctx, p.ID)
		if err != nil {
			return Draft{}, fmt.Errorf("read draft's picture candidates: %w", err)
		}
		dr.Candidates = len(cs)
	}
	return dr, nil
}

// Replace stores dr as the draft for token: the draft as the reviewer left it,
// or as the LLM read it again. Its source does not change.
func (d *Drafts) Replace(ctx context.Context, token string, dr Draft) error {
	doc, err := EncodeDraft(dr)
	if err != nil {
		return fmt.Errorf("encode draft: %w", err)
	}
	err = d.store.ReplaceDraft(ctx, token, doc)
	return draftGone(err, "replace draft")
}

// Revise replaces the draft for token with r, an agent's revision of it made
// from the draft at revision, and stores reason with it (ADR-00048). It is the
// replace Replace makes, with two differences: values that differ from the held
// draft are marked FromLLM (Revise), and a draft whose revision is no longer
// revision is ErrDraftChanged and left as it is. It returns the draft as now
// held, with its new Revision. It never saves the draft.
func (d *Drafts) Revise(ctx context.Context, cat CatalogReader, token, revision string, r Revision, reason string) (Draft, error) {
	held, err := d.Get(ctx, token)
	if err != nil {
		return Draft{}, err
	}
	if held.Revision != revision {
		return Draft{}, fmt.Errorf("%w: it is at revision %s, not %s", ErrDraftChanged, held.Revision, revision)
	}
	next, err := Revise(ctx, cat, held, r)
	if err != nil {
		return Draft{}, err
	}
	was, err := EncodeDraft(held)
	if err != nil {
		return Draft{}, fmt.Errorf("encode held draft: %w", err)
	}
	doc, err := EncodeDraft(next)
	if err != nil {
		return Draft{}, fmt.Errorf("encode draft: %w", err)
	}
	if bytes.Equal(was, doc) {
		return Draft{}, fmt.Errorf("%w: it is the same as the draft held", ErrInvalidRevision)
	}
	err = d.store.ReviseDraft(ctx, token, doc, revision, reason)
	if errors.Is(err, store.ErrStaleVersion) {
		return Draft{}, fmt.Errorf("%w: %w", ErrDraftChanged, err)
	}
	if err := draftGone(err, "revise draft"); err != nil {
		return Draft{}, err
	}
	next.Revision, next.Reason = store.DraftRevision(doc), reason
	return next, nil
}

// Pending lists the household's pending drafts, re-imports included, oldest
// first.
func (d *Drafts) Pending(ctx context.Context) ([]store.DraftSummary, error) {
	out, err := d.store.PendingDraftList(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending drafts: %w", err)
	}
	return out, nil
}

// Discard rejects the pending draft for token. The draft is kept, as rejected,
// for store.FinishedLifetime and then deleted with its source; its picture
// candidates, if Drafts holds pictures, are deleted now.
func (d *Drafts) Discard(ctx context.Context, token string) error {
	var id int64
	if d.pictures != nil {
		p, err := d.store.GetProposal(ctx, token)
		if err != nil {
			return draftGone(err, "discard draft")
		}
		id = p.ID
	}
	if err := draftGone(d.store.RejectProposal(ctx, token), "discard draft"); err != nil {
		return err
	}
	if id != 0 {
		// The draft is discarded; a candidate that outlives this call goes
		// when the proposal is pruned, so a failure is logged, not returned.
		if err := d.pictures.DeleteCandidates(ctx, id); err != nil {
			slog.WarnContext(ctx, "delete discarded draft's picture candidates", "proposal_id", id, "error", err.Error())
		}
	}
	return nil
}

// draftGone reports a draft that is not there to change as ErrDraftGone.
func draftGone(err error, what string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrNotPending):
		return fmt.Errorf("%s: %w: %w", what, ErrDraftGone, err)
	}
	return fmt.Errorf("%s: %w", what, err)
}
