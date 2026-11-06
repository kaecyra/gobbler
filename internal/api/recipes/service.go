package recipes

import (
	"context"
	"errors"
	"log/slog"

	"github.com/kaecyra/gobbler/internal/api/caller"
	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/video"
)

// Caller is who an operation is for (caller.Caller).
type Caller = caller.Caller

// Members is the membership lookup the service enforces (caller.Members).
type Members = caller.Members

// ErrNotMember is returned for a Caller that is not a verified member of a
// household (caller.ErrNotMember).
var ErrNotMember = caller.ErrNotMember

// Deps is what the service is built from.
type Deps struct {
	// Recipes is the recipe store. Required.
	Recipes *Store
	// Catalog is what saving a confirmed draft writes the ingredients and
	// equipment it proposed to. Required. *catalog.Store satisfies it.
	Catalog ingest.CatalogWriter
	// Members is the membership lookup. Required.
	Members Members
	// Pictures is the picture service. Nil leaves a saved recipe without a
	// picture, and a draft that has candidates cannot be reviewed.
	Pictures *Pictures
	// Reaper deletes the media objects of what the store deletes. Nil leaves
	// them to the sweep.
	Reaper Reaper
	// Thumbnails downloads the preview image of a recipe's video; nil means
	// YouTube's image host. Tests substitute a fake (ADR-00021).
	Thumbnails video.ThumbnailSource
}

// Service is the recipe operations. It is safe for concurrent use.
type Service struct {
	recipes  *Store
	members  Members
	pictures *Pictures
	reaper   Reaper
	thumbs   video.ThumbnailSource
	drafts   *ingest.Drafts
	saver    *ingest.Saver
}

// New returns a Service. Recipes, Catalog and Members are required; a missing
// one is an error here, not a nil dereference on the first request. The store
// the service uses deletes the picture candidates of re-imports that a new
// proposal ends (store.WithObjects), so every path through it cleans up the
// same way (ADR-00042).
func New(d Deps) (*Service, error) {
	if d.Recipes == nil || d.Catalog == nil || d.Members == nil {
		return nil, errors.New("recipes service: recipes, catalog and members are required")
	}
	s := &Service{recipes: d.Recipes, members: d.Members, pictures: d.Pictures, reaper: d.Reaper, thumbs: d.Thumbnails}
	if d.Reaper != nil {
		s.recipes = d.Recipes.WithObjects(d.Reaper)
	}
	s.drafts = ingest.NewDrafts(s.recipes)
	s.saver = ingest.NewSaver(s.recipes, d.Catalog).WithThumbnails(d.Thumbnails)
	if d.Pictures != nil {
		s.drafts = s.drafts.WithPictures(d.Pictures)
		s.saver = s.saver.WithPictures(d.Pictures)
	}
	return s, nil
}

// enter checks the Caller and returns a context that runs in its household
// (caller.Enter). It is the first line of every method.
func (s *Service) enter(ctx context.Context, c Caller) (context.Context, error) {
	return caller.Enter(ctx, s.members, c)
}

// Get returns recipe id, or ErrNotFound.
func (s *Service) Get(ctx context.Context, c Caller, id int64) (recipe.Recipe, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return recipe.Recipe{}, err
	}
	return s.recipes.Get(ctx, id)
}

// Info returns recipe id's bookkeeping: its version and when it changed.
func (s *Service) Info(ctx context.Context, c Caller, id int64) (Info, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Info{}, err
	}
	return s.recipes.Info(ctx, id)
}

// Search lists the household's recipes matching q.
func (s *Service) Search(ctx context.Context, c Caller, q SearchQuery) ([]Summary, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.recipes.Search(ctx, q)
}

// Cards lists the household's recipes matching q as library cards.
func (s *Service) Cards(ctx context.Context, c Caller, q SearchQuery) ([]Card, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.recipes.Cards(ctx, q)
}

// RecentlyMade lists the most recent made-it entries, newest first.
func (s *Service) RecentlyMade(ctx context.Context, c Caller, limit int) ([]MadeEntry, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.recipes.RecentlyMade(ctx, limit)
}

// Idle lists the cards of recipes not made lately.
func (s *Service) Idle(ctx context.Context, c Caller) ([]Card, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.recipes.Idle(ctx)
}

// ListNotes returns recipe id's notes.
func (s *Service) ListNotes(ctx context.Context, c Caller, id int64) ([]Note, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.recipes.ListNotes(ctx, id)
}

// ListMadeIt returns recipe id's made-it entries.
func (s *Service) ListMadeIt(ctx context.Context, c Caller, id int64) ([]MadeIt, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.recipes.ListMadeIt(ctx, id)
}

// ListRevisions returns recipe id's saved versions, newest first.
func (s *Service) ListRevisions(ctx context.Context, c Caller, id int64) ([]Revision, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.recipes.ListRevisions(ctx, id)
}

// GetVersion returns recipe id as it was at version.
func (s *Service) GetVersion(ctx context.Context, c Caller, id, version int64) (recipe.Recipe, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return recipe.Recipe{}, err
	}
	return s.recipes.GetVersion(ctx, id, version)
}

// UpdateIfVersion saves r over recipe id if the recipe is still at
// expectedVersion, and is ErrStaleVersion if it is not. Nothing is written
// when it refuses.
func (s *Service) UpdateIfVersion(ctx context.Context, c Caller, id, expectedVersion int64, r recipe.Recipe) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	return s.recipes.UpdateIfVersion(ctx, id, expectedVersion, r)
}

// Restore saves recipe id's version as a new version.
func (s *Service) Restore(ctx context.Context, c Caller, id, version int64) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	return s.recipes.Restore(ctx, id, version)
}

// Clone copies recipe id and returns the copy's id.
func (s *Service) Clone(ctx context.Context, c Caller, id int64) (int64, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return 0, err
	}
	return s.recipes.Clone(ctx, id)
}

// Delete removes recipe id with its picture objects and the candidates of the
// re-imports waiting on it (ADR-00039). It is ErrNotFound for a recipe that is
// not the household's, and refuses a recipe a shopping list holds.
func (s *Service) Delete(ctx context.Context, c Caller, id int64) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	if s.reaper == nil {
		return s.recipes.Delete(ctx, id)
	}
	return s.recipes.DeleteWithPicture(ctx, id, s.reaper)
}

// HasThumbnail says whether the preview image of a video is held.
func (s *Service) HasThumbnail(ctx context.Context, c Caller, videoID string) (bool, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return false, err
	}
	return s.recipes.HasThumbnail(ctx, videoID)
}

// Thumbnail returns the preview image of recipe id's video.
func (s *Service) Thumbnail(ctx context.Context, c Caller, id int64) (Thumbnail, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Thumbnail{}, err
	}
	return s.recipes.Thumbnail(ctx, id)
}

// KeepThumbnail makes sure the preview image of videoID is held for recipe
// id after it was saved (video.KeepThumbnail). It never fails the save.
func (s *Service) KeepThumbnail(ctx context.Context, c Caller, id int64, videoID string) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		slog.ErrorContext(ctx, "keep video thumbnail refused", "recipe_id", id, "error", err.Error())
		return
	}
	video.KeepThumbnail(ctx, s.recipes, s.thumbs, id, videoID)
}

// KeepThumbnailAfter is KeepThumbnail for a recipe whose video is read after
// the save (video.KeepThumbnailAfter).
func (s *Service) KeepThumbnailAfter(ctx context.Context, c Caller, id int64, readVideoID func(context.Context) (string, error)) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		slog.ErrorContext(ctx, "keep video thumbnail refused", "recipe_id", id, "error", err.Error())
		return
	}
	video.KeepThumbnailAfter(ctx, s.recipes, s.thumbs, id, readVideoID)
}
