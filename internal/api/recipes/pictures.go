package recipes

import "context"

// The pictures offered for a saved recipe on its edit page (ADR-00051). A
// search or an upload changes no recipe: the picture changes only when the
// edit form is saved (SaveWithPicture).

// FindPictures searches recipe id's source page for pictures, live and then
// in its saved snapshot, and keeps what it finds as the recipe's offered
// pictures, replacing an earlier search's. It returns what it found, which may
// be nothing. With no picture service it is an error.
func (s *Service) FindPictures(ctx context.Context, c Caller, id int64) ([]Offered, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	if s.pictures == nil {
		return nil, errNoPictures
	}
	return s.pictures.Search(ctx, id)
}

// OfferPicture keeps an uploaded file as recipe id's uploaded offered picture,
// with the limits and refusals of an avatar (ADR-00033).
func (s *Service) OfferPicture(ctx context.Context, c Caller, id int64, data []byte) (Offered, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Offered{}, err
	}
	if s.pictures == nil {
		return Offered{}, errNoPictures
	}
	return s.pictures.Offer(ctx, id, data)
}

// OfferedPictures returns recipe id's offered pictures that have not expired.
// With no picture service there are none.
func (s *Service) OfferedPictures(ctx context.Context, c Caller, id int64) ([]Offered, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	if s.pictures == nil {
		return nil, nil
	}
	return s.pictures.Offered(ctx, id)
}

// SaveWithPicture runs save, the edit form's write of recipe id, and then
// carries out the form's picture choice and deletes every offered picture of
// the recipe (picture.Service.Apply). The choice is checked and applied under
// the recipe's picture lock, with save in between: a choice no longer offered
// is ErrOfferGone and save does not run; an error from save is returned as it
// is and the picture is left alone; a failure to set the picture after save
// succeeded wraps ErrPictureNotApplied. With no picture service only
// KeepPicture is accepted, and save just runs.
func (s *Service) SaveWithPicture(ctx context.Context, c Caller, id int64, choice PictureChoice, save func(context.Context) error) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	if s.pictures == nil {
		if choice != KeepPicture {
			return errNoPictures
		}
		if save == nil {
			return nil
		}
		return save(ctx)
	}
	return s.pictures.Apply(ctx, id, choice, save)
}
