package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/kaecyra/gobbler/internal/picture"
)

// PictureChoice is the person's choice among a draft's picture candidates
// (ADR-00039, Choosing). Any value but the constants below is the offer ID of
// the chosen candidate, never its position (ADR-00052); PictureNone is "No
// picture"; the zero value means no choice has been made yet, which Confirm
// refuses for a draft that has candidates.
type PictureChoice string

const (
	// PictureUnchosen is the zero value: the person has not chosen.
	PictureUnchosen PictureChoice = ""
	// PictureNone is "No picture", always offered.
	PictureNone PictureChoice = "none"
	// PictureKeep is "Keep current picture", offered on a re-import only
	// (ADR-00042): the recipe's picture is left as it is. It is the default
	// of a re-import, and the zero value reads the same there.
	PictureKeep PictureChoice = "keep"
)

// candidate reports whether c names a candidate by its offer ID rather than
// being one of the constants. No offer ID has the shape of a constant.
func (c PictureChoice) candidate() bool {
	return c != PictureUnchosen && c != PictureNone && c != PictureKeep
}

// ErrStalePicture is a draft whose chosen candidate is no longer one of its
// candidates: they were fetched again after the choice was made. Confirm
// refuses it, and the person chooses again.
var ErrStalePicture = errors.New("the chosen picture is no longer offered; choose a picture, or No picture")

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
	Promote(ctx context.Context, proposalID int64, offer string, id int64) (string, error)
	DeleteCandidates(ctx context.Context, proposalID int64) error
	// Remove clears a recipe's picture, for a re-import saved with No picture.
	Remove(ctx context.Context, id int64) error
}

// checkPicture reports a draft that cannot be confirmed for want of a valid
// picture choice. A draft with no candidates needs none: its only option is
// "No picture", which is what it gets.
func (d Draft) checkPicture() error {
	if d.Reimport != nil && (d.Picture == PictureKeep || d.Picture == PictureUnchosen) {
		return nil
	}
	switch {
	case d.Picture == PictureNone:
		return nil
	case d.Picture == PictureUnchosen:
		if len(d.Candidates) > 0 {
			return errors.New("choose a picture, or No picture")
		}
		return nil
	case d.Picture == PictureKeep:
		return fmt.Errorf("picture choice %q is not valid", d.Picture)
	case !slices.Contains(d.Candidates, string(d.Picture)):
		return ErrStalePicture
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
// PictureNone, PictureKeep on a re-import, or the offer ID of one of its
// candidates. It changes nothing else, and the choice survives later edits of
// the draft. A choice the draft has no candidate for, including the ID of a
// candidate a later fetch replaced, is ErrBadPictureChoice.
func (d *Drafts) ChoosePicture(ctx context.Context, token string, c PictureChoice) error {
	dr, err := d.Get(ctx, token)
	if err != nil {
		return err
	}
	keep := c == PictureKeep && dr.Reimport != nil
	if !keep && c != PictureNone && !slices.Contains(dr.Candidates, string(c)) {
		return fmt.Errorf("%w: %q", ErrBadPictureChoice, c)
	}
	dr.Picture = c
	return d.Replace(ctx, token, dr)
}

// keepPicture promotes the chosen candidate of a confirmed draft to recipe
// id's picture and deletes the rest, or deletes them all for No picture. The
// recipe is already stored, so a failure is returned for the caller to report,
// not rolled back; what is left is deleted when the proposal is pruned.
func (s *Saver) keepPicture(ctx context.Context, d Draft, id int64) error {
	if d.Reimport != nil {
		return s.reimportPicture(ctx, d, id)
	}
	if s.pictures == nil || d.ProposalID == 0 || len(d.Candidates) == 0 {
		return nil
	}
	if d.Picture.candidate() {
		if _, err := s.pictures.Promote(ctx, d.ProposalID, string(d.Picture), id); err != nil {
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

// reimportPicture applies a re-import's picture choice to the recipe it
// replaced (ADR-00042). Keeping the current picture, the default, deletes the
// candidates and leaves the picture; a candidate replaces it and the previous
// object is deleted by Promote; No picture removes it. The recipe is already
// saved, so a failure is returned for the caller to report.
func (s *Saver) reimportPicture(ctx context.Context, d Draft, id int64) error {
	if s.pictures == nil || d.ProposalID == 0 {
		return nil
	}
	switch {
	case d.Picture.candidate() && len(d.Candidates) > 0:
		if _, err := s.pictures.Promote(ctx, d.ProposalID, string(d.Picture), id); err != nil {
			slog.ErrorContext(ctx, "keep chosen picture of re-import", "recipe_id", id, "proposal_id", d.ProposalID, "error", err.Error())
			return fmt.Errorf("keep the chosen picture: %w", err)
		}
		return nil
	case d.Picture == PictureNone:
		if err := s.pictures.Remove(ctx, id); err != nil {
			slog.ErrorContext(ctx, "remove picture on re-import", "recipe_id", id, "error", err.Error())
			return fmt.Errorf("remove the recipe's picture: %w", err)
		}
	}
	if err := s.pictures.DeleteCandidates(ctx, d.ProposalID); err != nil {
		slog.ErrorContext(ctx, "delete picture candidates of re-import", "recipe_id", id, "proposal_id", d.ProposalID, "error", err.Error())
		return fmt.Errorf("delete the re-import's picture candidates: %w", err)
	}
	return nil
}

// WithPictures returns s keeping the chosen picture of the drafts it saves.
func (s *Saver) WithPictures(p PictureKeeper) *Saver {
	c := *s
	c.pictures = p
	return &c
}
