package ingest

import (
	"context"
	"errors"

	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/picture"
	"github.com/kaecyra/gobbler/internal/store"
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
	// proposals is the table the drafts are held in, for ended drafts.
	proposals *store.Store
	// recipeName names a recipe the saver stored; saved maps each token
	// SaveProposed saved to its recipe. The fake saver ends a draft without
	// marking it accepted, so a saved draft is named from here.
	recipeName func(id int64) string
	saved      map[string]int64
}

func (r *testRecipes) PendingDrafts(ctx context.Context, _ apirecipes.Caller) ([]apirecipes.DraftSummary, error) {
	return r.drafts.Pending(ctx)
}

func (r *testRecipes) EndedDraft(ctx context.Context, _ apirecipes.Caller, token string) (apirecipes.EndedDraft, error) {
	if id, ok := r.saved[token]; ok {
		return apirecipes.EndedDraft{Saved: true, RecipeID: id, Name: r.recipeName(id)}, nil
	}
	return r.proposals.EndedDraftByToken(ctx, token)
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
	saved, err := r.saver.SaveProposed(ctx, token, c)
	if err == nil {
		if r.saved == nil {
			r.saved = map[string]int64{}
		}
		r.saved[token] = saved.RecipeID
	}
	return saved, err
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
