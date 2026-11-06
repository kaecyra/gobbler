package ingest

import (
	"context"
	"errors"

	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/picture"
)

// The seams these tests swap, as the handler used to hold them before it
// reached them through the recipe service. The service ignores nothing about a
// Caller; these stand-ins do, because what they pin is the handler's response
// to what each seam returns, not membership (the service's own tests cover it).

// Saver stores a confirmed draft that is held under token.
type Saver interface {
	SaveProposed(ctx context.Context, token string, c ingestcore.Confirmed) (ingestcore.Saved, error)
}

// Reimporter starts a re-import of a saved recipe.
type Reimporter interface {
	Start(ctx context.Context, id int64, by apirecipes.Provenance) (string, error)
}

// PictureService is the picture service as the review screen read it.
type PictureService interface {
	Candidates(ctx context.Context, proposalID int64) ([]picture.Candidate, error)
	Key(ctx context.Context, recipeID int64) (string, bool, error)
}

// testRecipes is a Recipes over real drafts and swappable save, re-import and
// picture seams.
type testRecipes struct {
	drafts     *ingestcore.Drafts
	saver      Saver
	reimporter Reimporter
	pictures   PictureService
}

func (r *testRecipes) DraftsFull(ctx context.Context, _ apirecipes.Caller) (bool, error) {
	return r.drafts.Full(ctx)
}

func (r *testRecipes) AddDraft(ctx context.Context, _ apirecipes.Caller, d ingestcore.Draft, by apirecipes.Provenance) (string, error) {
	return r.drafts.Add(ctx, d, by)
}

func (r *testRecipes) GetDraft(ctx context.Context, _ apirecipes.Caller, token string) (ingestcore.Draft, error) {
	return r.drafts.Get(ctx, token)
}

func (r *testRecipes) ReplaceDraft(ctx context.Context, _ apirecipes.Caller, token string, d ingestcore.Draft) error {
	return r.drafts.Replace(ctx, token, d)
}

func (r *testRecipes) DiscardDraft(ctx context.Context, _ apirecipes.Caller, token string) error {
	return r.drafts.Discard(ctx, token)
}

func (r *testRecipes) ChoosePicture(ctx context.Context, _ apirecipes.Caller, token string, choice ingestcore.PictureChoice) error {
	return r.drafts.ChoosePicture(ctx, token, choice)
}

func (r *testRecipes) SaveProposed(ctx context.Context, _ apirecipes.Caller, token string, c ingestcore.Confirmed) (ingestcore.Saved, error) {
	return r.saver.SaveProposed(ctx, token, c)
}

func (r *testRecipes) StartReimport(ctx context.Context, _ apirecipes.Caller, id int64, _ ingestcore.URLDrafter, by apirecipes.Provenance) (string, error) {
	return r.reimporter.Start(ctx, id, by)
}

func (r *testRecipes) PictureKey(ctx context.Context, _ apirecipes.Caller, id int64) (string, bool, error) {
	if r.pictures == nil {
		return "", false, nil
	}
	return r.pictures.Key(ctx, id)
}

func (r *testRecipes) Candidates(ctx context.Context, _ apirecipes.Caller, proposalID int64) ([]apirecipes.Candidate, error) {
	if r.pictures == nil {
		return nil, errNoPictureService
	}
	return r.pictures.Candidates(ctx, proposalID)
}

var errNoPictureService = errors.New("no picture service")
