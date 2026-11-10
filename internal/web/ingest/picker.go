package ingest

import (
	"context"
	"errors"
	"fmt"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apiingest "github.com/kaecyra/gobbler/internal/api/ingest"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/web/layout"
	webpicture "github.com/kaecyra/gobbler/internal/web/picture"
)

const (
	// fieldPicture is the picker's radio group. Its radios belong to the
	// editor's form (editor.FormID), so the choice is posted with the edits.
	fieldPicture = "picture"
	// pictureNone is the value of the "No picture" radio.
	pictureNone = "none"
	// pictureKeep is the value of the "Keep current picture" radio, offered on a
	// re-import (ADR-00042).
	pictureKeep = "keep"
)

// pickOption is one candidate in the picker.
type pickOption struct {
	// Value is the radio's value: the candidate's offer ID (ADR-00052).
	Value         string
	Src           string
	Width, Height int
	Checked       bool
}

// pickerView is the picture picker of the review screen (ADR-00039,
// Choosing): the draft's candidates and "No picture", which is always offered.
type pickerView struct {
	Options []pickOption
	// NoneChecked is whether "No picture" is the choice. A draft with no
	// candidates has nothing else to choose, so it starts that way.
	NoneChecked bool
	// Chosen is whether any option is the choice.
	Chosen bool
	// Keep is "Keep current picture", offered only on a re-import and its
	// default. KeepSrc is the address of the picture it keeps, empty when the
	// recipe has none.
	Keep        bool
	KeepChecked bool
	KeepSrc     string
}

// picker builds the picker for d. A draft with candidates starts with no
// choice, and confirm refuses until one is made.
func (h *handler) picker(ctx context.Context, d apiingest.Draft) (pickerView, error) {
	v := pickerView{NoneChecked: d.Picture == apiingest.PictureNone || (len(d.Candidates) == 0 && d.Reimport == nil)}
	if d.Reimport != nil {
		// Keeping the picture is the default, so no choice is ever missing.
		v.Keep = true
		v.KeepChecked = d.Picture == apiingest.PictureKeep || d.Picture == apiingest.PictureUnchosen
		key, ok, err := h.recipes.PictureKey(ctx, caller.FromContext(ctx), d.Reimport.RecipeID)
		if err != nil {
			return pickerView{}, fmt.Errorf("read the recipe's picture: %w", err)
		}
		if ok {
			v.KeepSrc = webpicture.Src(d.Reimport.RecipeID, key)
		}
	}
	v.Chosen = v.NoneChecked || v.KeepChecked
	if len(d.Candidates) == 0 {
		return v, nil
	}
	cs, err := h.recipes.Candidates(ctx, caller.FromContext(ctx), d.ProposalID)
	if err != nil {
		return pickerView{}, fmt.Errorf("read picture candidates: %w", err)
	}
	for _, c := range cs {
		checked := d.Picture == apiingest.PictureChoice(c.Offer)
		v.Chosen = v.Chosen || checked
		v.Options = append(v.Options, pickOption{
			Value: c.Offer, Src: webpicture.CandidateSrc(d.ProposalID, c.Offer),
			Width: c.Width, Height: c.Height, Checked: checked,
		})
	}
	return v, nil
}

// choosePicture keeps the choice posted with the review form on the draft, and
// on base. No value leaves the draft as it is. A value that is not "none",
// "keep" or the shape of an offer ID, or an ID the draft has no candidate for,
// is a bad request.
func (h *handler) choosePicture(ctx context.Context, token, value string, base *apiingest.Draft) error {
	if value == "" {
		return nil
	}
	choice := apiingest.PictureNone
	if value == pictureKeep {
		choice = apiingest.PictureKeep
	} else if value != pictureNone {
		if !apirecipes.ValidOfferID(value) {
			return fmt.Errorf("picture %q: %w", value, layout.ErrInvalid)
		}
		choice = apiingest.PictureChoice(value)
	}
	if choice == base.Picture || (choice == apiingest.PictureKeep && base.Picture == apiingest.PictureUnchosen && base.Reimport != nil) {
		return nil
	}
	err := h.recipes.ChoosePicture(ctx, caller.FromContext(ctx), token, choice)
	if errors.Is(err, apiingest.ErrBadPictureChoice) {
		return fmt.Errorf("%w: %w", layout.ErrInvalid, err)
	}
	if errors.Is(err, apiingest.ErrDraftGone) {
		return errGone
	}
	if err != nil {
		return fmt.Errorf("choose picture: %w", err)
	}
	base.Picture = choice
	return nil
}
