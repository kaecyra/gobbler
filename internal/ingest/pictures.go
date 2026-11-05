package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kaecyra/gobbler/internal/picture"
)

// PictureChoice is the person's choice among a draft's picture candidates
// (ADR-00039, Choosing). A positive value is the position of the chosen
// candidate; PictureNone is "No picture"; the zero value means no choice has
// been made yet, which Confirm refuses for a draft that has candidates.
type PictureChoice int

const (
	// PictureUnchosen is the zero value: the person has not chosen.
	PictureUnchosen PictureChoice = 0
	// PictureNone is "No picture", always offered.
	PictureNone PictureChoice = -1
)

// ErrBadPictureChoice is returned (wrapped) by Drafts.ChoosePicture for a
// choice that is neither "No picture" nor one of the draft's candidates.
var ErrBadPictureChoice = errors.New("that picture is not one of the draft's candidates")

// Pictures is what Drafts calls to hold a draft's candidates; *picture.Service
// satisfies it. Each call is scoped to the household on the context.
type Pictures interface {
	FetchCandidates(ctx context.Context, proposalID int64, urls []string) ([]picture.Candidate, error)
	Candidates(ctx context.Context, proposalID int64) ([]picture.Candidate, error)
	DeleteCandidates(ctx context.Context, proposalID int64) error
}

// PictureKeeper is what Saver calls to keep a confirmed draft's choice;
// *picture.Service satisfies it.
type PictureKeeper interface {
	Promote(ctx context.Context, proposalID int64, position int, id int64) (string, error)
	DeleteCandidates(ctx context.Context, proposalID int64) error
}

// checkPicture reports a draft that cannot be confirmed for want of a valid
// picture choice. A draft with no candidates needs none: its only option is
// "No picture", which is what it gets.
func (d Draft) checkPicture() error {
	switch {
	case d.Picture == PictureNone:
		return nil
	case d.Picture == PictureUnchosen && d.Candidates > 0:
		return errors.New("choose a picture, or No picture")
	case d.Picture > 0 && int(d.Picture) > d.Candidates:
		return fmt.Errorf("picture %d is not one of the draft's %d candidates", d.Picture, d.Candidates)
	case d.Picture < PictureNone:
		return fmt.Errorf("picture choice %d is not valid", d.Picture)
	}
	return nil
}

// WithPictures returns d fetching picture candidates for the URL drafts it
// holds and deleting them when a draft is discarded (ADR-00039).
func (d *Drafts) WithPictures(p Pictures) *Drafts {
	c := *d
	c.pictures = p
	return &c
}

// fetchCandidates downloads dr's candidates for the draft just held under
// token. A failure is logged and the draft is held without candidates: the
// person can still confirm it with No picture, and the failure is a fact about
// the page or the media store, not a broken ingest (ADR-00039).
func (d *Drafts) fetchCandidates(ctx context.Context, token string, dr Draft) {
	if d.pictures == nil || len(dr.ImageURLs) == 0 {
		return
	}
	p, err := d.store.GetProposal(ctx, token)
	if err != nil {
		slog.WarnContext(ctx, "draft held without picture candidates", "reason", "read the held draft", "error", err.Error())
		return
	}
	if _, err := d.pictures.FetchCandidates(ctx, p.ID, dr.ImageURLs); err != nil {
		slog.WarnContext(ctx, "draft held without picture candidates", "proposal_id", p.ID, "error", err.Error())
	}
}

// ChoosePicture records the person's choice on the pending draft for token:
// PictureNone, or the position of one of its candidates. It changes nothing
// else, and the choice survives later edits of the draft. A choice the draft
// has no candidate for is ErrBadPictureChoice.
func (d *Drafts) ChoosePicture(ctx context.Context, token string, c PictureChoice) error {
	dr, err := d.Get(ctx, token)
	if err != nil {
		return err
	}
	if c == PictureUnchosen || c < PictureNone || int(c) > dr.Candidates {
		return fmt.Errorf("%w: %d of %d", ErrBadPictureChoice, c, dr.Candidates)
	}
	dr.Picture = c
	return d.Replace(ctx, token, dr)
}

// keepPicture promotes the chosen candidate of a confirmed draft to recipe
// id's picture and deletes the rest, or deletes them all for No picture. The
// recipe is already stored, so a failure is returned for the caller to report,
// not rolled back; what is left is deleted when the proposal is pruned.
func (s *Saver) keepPicture(ctx context.Context, d Draft, id int64) error {
	if s.pictures == nil || d.ProposalID == 0 || d.Candidates == 0 {
		return nil
	}
	if d.Picture > 0 {
		if _, err := s.pictures.Promote(ctx, d.ProposalID, int(d.Picture), id); err != nil {
			slog.ErrorContext(ctx, "keep chosen picture", "recipe_id", id, "proposal_id", d.ProposalID, "error", err.Error())
			return fmt.Errorf("keep the chosen picture: %w", err)
		}
		return nil
	}
	if err := s.pictures.DeleteCandidates(ctx, d.ProposalID); err != nil {
		slog.ErrorContext(ctx, "delete unchosen picture candidates", "recipe_id", id, "proposal_id", d.ProposalID, "error", err.Error())
		return fmt.Errorf("delete the draft's picture candidates: %w", err)
	}
	return nil
}

// WithPictures returns s keeping the chosen picture of the drafts it saves.
func (s *Saver) WithPictures(p PictureKeeper) *Saver {
	c := *s
	c.pictures = p
	return &c
}
