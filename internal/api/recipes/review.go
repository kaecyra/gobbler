package recipes

import (
	"context"
	"errors"

	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// This file is what waits for a person to review: a proposed edit to a saved
// recipe (ADR-00035), and the draft of a new recipe or a re-import
// (ADR-00006, ADR-00042). None of it changes a saved recipe except Accept* and
// SaveProposed, which the review screen alone calls.

// errNoPictures is Candidates on a service built without a picture service.
var errNoPictures = errors.New("recipes service: no picture service")

// GetProposal returns the proposal under token, in whatever state it is in.
func (s *Service) GetProposal(ctx context.Context, c Caller, token string) (Proposal, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Proposal{}, err
	}
	return s.recipes.GetProposal(ctx, token)
}

// PendingEdit returns the edit waiting on recipe id, if any.
func (s *Service) PendingEdit(ctx context.Context, c Caller, id int64) (Proposal, bool, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Proposal{}, false, err
	}
	return s.recipes.PendingEdit(ctx, id)
}

// PendingReimport returns the re-import waiting on recipe id, if any.
func (s *Service) PendingReimport(ctx context.Context, c Caller, id int64) (Proposal, bool, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Proposal{}, false, err
	}
	return s.recipes.PendingReimport(ctx, id)
}

// ProposeEdit holds r as a proposed new version of recipe id, made from
// baseVersion, for review; it saves nothing (ADR-00035). It is
// ErrStaleVersion if the recipe has moved on from baseVersion.
func (s *Service) ProposeEdit(ctx context.Context, c Caller, id, baseVersion int64, r recipe.Recipe, by Provenance) (string, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return "", err
	}
	return s.recipes.ProposeEdit(ctx, id, baseVersion, r, by)
}

// RejectProposal turns the proposal under token down.
func (s *Service) RejectProposal(ctx context.Context, c Caller, token string) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	return s.recipes.RejectProposal(ctx, token)
}

// AcceptEdit saves r as the proposal under token, if recipe is still at
// expectedVersion (ErrStaleVersion if not) and the proposal still pending.
func (s *Service) AcceptEdit(ctx context.Context, c Caller, token string, expectedVersion int64, r recipe.Recipe) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	return s.recipes.AcceptEdit(ctx, token, expectedVersion, r)
}

// DraftsFull says the household already holds the most drafts it may, so a
// caller can refuse before it fetches or reads anything. AddDraft stays the
// authority.
func (s *Service) DraftsFull(ctx context.Context, c Caller) (bool, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return false, err
	}
	return s.drafts.Full(ctx)
}

// AddDraft holds d for review and returns the token that retrieves it. It is
// ErrDraftLimit past the household's limit of drafts.
func (s *Service) AddDraft(ctx context.Context, c Caller, d ingest.Draft, by Provenance) (string, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return "", err
	}
	return s.drafts.Add(ctx, d, by)
}

// GetDraft returns the draft held under token, or ingest.ErrDraftGone.
func (s *Service) GetDraft(ctx context.Context, c Caller, token string) (ingest.Draft, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return ingest.Draft{}, err
	}
	return s.drafts.Get(ctx, token)
}

// ReplaceDraft replaces the draft held under token, after an edit or a retry.
func (s *Service) ReplaceDraft(ctx context.Context, c Caller, token string, d ingest.Draft) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	return s.drafts.Replace(ctx, token, d)
}

// ReviseDraft replaces the draft held under token with an agent's revision of
// it, made from the draft at revision, and stores reason on it (ADR-00048). It
// is ReplaceDraft's replace, refused with ErrDraftChanged when the draft has
// changed since revision. It returns the draft as now held; it never saves it.
func (s *Service) ReviseDraft(ctx context.Context, c Caller, token, revision string, r DraftRevision, reason string) (ingest.Draft, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return ingest.Draft{}, err
	}
	return s.drafts.Revise(ctx, s.catalog, token, revision, r, reason)
}

// PendingDrafts lists the household's pending drafts, re-imports included,
// oldest first.
func (s *Service) PendingDrafts(ctx context.Context, c Caller) ([]DraftSummary, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.drafts.Pending(ctx)
}

// SameSource lists the household's saved recipes and pending drafts whose
// source address is sourceURL's, fragments ignored, for an import or preview
// to report (ADR-00048). It reports and refuses nothing. The drafts are read
// from PendingDrafts' list, the one read of drafts' source addresses.
func (s *Service) SameSource(ctx context.Context, c Caller, sourceURL string) (SameSource, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return SameSource{}, err
	}
	var out SameSource
	if out.Recipes, err = s.recipes.RecipesFromSource(ctx, sourceURL); err != nil {
		return SameSource{}, err
	}
	addr := store.SourceAddress(sourceURL)
	if addr == "" {
		return out, nil
	}
	pending, err := s.drafts.Pending(ctx)
	if err != nil {
		return SameSource{}, err
	}
	for _, d := range pending {
		if store.SourceAddress(d.SourceURL) == addr {
			out.Drafts = append(out.Drafts, d)
		}
	}
	return out, nil
}

// DiscardDraft drops the draft held under token with its picture candidates.
func (s *Service) DiscardDraft(ctx context.Context, c Caller, token string) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	return s.drafts.Discard(ctx, token)
}

// ChoosePicture records which candidate, or none, the draft is saved with.
func (s *Service) ChoosePicture(ctx context.Context, c Caller, token string, choice ingest.PictureChoice) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	return s.drafts.ChoosePicture(ctx, token, choice)
}

// SaveProposed saves the confirmed draft held under token as a new recipe, or
// as the new version of the recipe a re-import replaces, and marks the draft
// accepted, in one transaction. It is the only way a draft becomes a recipe
// and takes ingest.Confirmed, which only the review screen's confirm step can
// make.
func (s *Service) SaveProposed(ctx context.Context, c Caller, token string, conf ingest.Confirmed) (ingest.Saved, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return ingest.Saved{}, err
	}
	return s.saver.SaveProposed(ctx, token, conf)
}

// StartReimport drafts recipe id again from its source page with drafter and
// holds the draft for review, returning its token (ADR-00042). drafter is the
// ingestion pipeline of the adapter, which differs between the web (it may
// fall back to an LLM) and MCP (it never does).
func (s *Service) StartReimport(ctx context.Context, c Caller, id int64, drafter ingest.URLDrafter, by Provenance) (string, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return "", err
	}
	return ingest.NewReimporter(s.recipes, drafter, s.drafts).Start(ctx, id, by)
}

// PictureKey returns the key of recipe id's picture, and whether it has one.
// A recipe that is not the household's is ErrNotFound. With no picture service
// no recipe has one.
func (s *Service) PictureKey(ctx context.Context, c Caller, id int64) (string, bool, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return "", false, err
	}
	if s.pictures == nil {
		return "", false, nil
	}
	return s.pictures.Key(ctx, id)
}

// Candidates returns the picture candidates held for draft proposalID. With no
// picture service it is an error: a draft that has candidates cannot be
// reviewed without one.
func (s *Service) Candidates(ctx context.Context, c Caller, proposalID int64) ([]Candidate, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	if s.pictures == nil {
		return nil, errNoPictures
	}
	return s.pictures.Candidates(ctx, proposalID)
}
