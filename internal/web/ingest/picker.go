package ingest

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/web/layout"
	webpicture "github.com/kaecyra/gobbler/internal/web/picture"
)

const (
	// fieldPicture is the picker's radio group. Its radios belong to the
	// editor's form (see editorFormID), so the choice is posted with the edits.
	fieldPicture = "picture"
	// pictureNone is the value of the "No picture" radio.
	pictureNone = "none"
	// editorFormID is the id of the editor's form (editor.templ), which the
	// picker sits outside of.
	editorFormID = "editor"
)

// pickOption is one candidate in the picker.
type pickOption struct {
	// Value is the radio's value: the candidate's position.
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
}

// picker builds the picker for d. A draft with candidates starts with no
// choice, and confirm refuses until one is made.
func (h *handler) picker(ctx context.Context, d ingestcore.Draft) (pickerView, error) {
	v := pickerView{NoneChecked: d.Picture == ingestcore.PictureNone || d.Candidates == 0}
	v.Chosen = v.NoneChecked
	if d.Candidates == 0 {
		return v, nil
	}
	if h.pictures == nil {
		return pickerView{}, fmt.Errorf("draft has %d picture candidates and no picture service", d.Candidates)
	}
	cs, err := h.pictures.Candidates(ctx, d.ProposalID)
	if err != nil {
		return pickerView{}, fmt.Errorf("read picture candidates: %w", err)
	}
	for _, c := range cs {
		checked := d.Picture == ingestcore.PictureChoice(c.Position)
		v.Chosen = v.Chosen || checked
		v.Options = append(v.Options, pickOption{
			Value: strconv.Itoa(c.Position), Src: webpicture.CandidateSrc(d.ProposalID, c.Position),
			Width: c.Width, Height: c.Height, Checked: checked,
		})
	}
	return v, nil
}

// choosePicture keeps the choice posted with the review form on the draft, and
// on base. No value leaves the draft as it is. A value that is not "none" or a
// whole position, or a position the draft has no candidate for, is a bad request.
func (h *handler) choosePicture(ctx context.Context, token, value string, base *ingestcore.Draft) error {
	if value == "" {
		return nil
	}
	choice := ingestcore.PictureNone
	if value != pictureNone {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 {
			return fmt.Errorf("picture %q: %w", value, layout.ErrInvalid)
		}
		choice = ingestcore.PictureChoice(n)
	}
	if choice == base.Picture {
		return nil
	}
	err := h.drafts.ChoosePicture(ctx, token, choice)
	if errors.Is(err, ingestcore.ErrBadPictureChoice) {
		return fmt.Errorf("%w: %w", layout.ErrInvalid, err)
	}
	if errors.Is(err, ingestcore.ErrDraftGone) {
		return errGone
	}
	if err != nil {
		return fmt.Errorf("choose picture: %w", err)
	}
	base.Picture = choice
	return nil
}
