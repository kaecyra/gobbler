package ingest

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/store"
)

// reimportEnv has a recipe imported from a URL whose page names two pictures,
// saved with the first, and the page served again for the re-import.
type reimportEnv struct {
	*pictureEnv
	reimporter *Reimporter
	recipeID   int64
	pageURL    string
}

func newReimportEnv(t *testing.T) *reimportEnv {
	t.Helper()
	e := newPictureEnv(t)
	imgs := imageSite(t)
	page := bytes.Replace(readFixture(t, "pancakes.html"), []byte(`"@type":"Recipe",`),
		[]byte(`"@type":"Recipe","image":["`+imgs.URL+`/a.png","`+imgs.URL+`/b.png"],`), 1)
	srv, _ := serve(t, page)
	pipe := e.pipeline(t, srv.Client())
	d, err := pipe.FromURL(e.ctx, srv.URL+"/pancakes")
	if err != nil {
		t.Fatal(err)
	}
	token, held := e.hold(t, d)
	if err := e.drafts.ChoosePicture(e.ctx, token, choice(held, 0)); err != nil {
		t.Fatal(err)
	}
	held, _ = e.drafts.Get(e.ctx, token)
	saved, err := e.keeper.SaveProposed(e.ctx, token, confirm(t, held))
	if err != nil || saved.PictureErr != nil {
		t.Fatalf("first import = %v, picture %v", err, saved.PictureErr)
	}
	return &reimportEnv{pictureEnv: e, reimporter: NewReimporter(e.store, pipe, e.drafts), recipeID: saved.RecipeID, pageURL: srv.URL + "/pancakes"}
}

func (e *reimportEnv) start(t *testing.T) (string, Draft) {
	t.Helper()
	token, err := e.reimporter.Start(e.ctx, e.recipeID, store.Provenance{Person: auth.DevEmail})
	if err != nil {
		t.Fatalf("Start = %v", err)
	}
	held, err := e.drafts.Get(e.ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	return token, held
}

func (e *reimportEnv) picture(t *testing.T) string {
	t.Helper()
	key, _, err := e.svc.Key(e.ctx, e.recipeID)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func (e *reimportEnv) recipes(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM recipes`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// ADR-00042 obligations 3 and 7: the draft is the whole ingest draft, read by
// the rules without the LLM, and it starts on "Keep current picture".
func TestStartHoldsTheWholeDraftWithoutTheLLMAndKeepsThePicture(t *testing.T) {
	e := newReimportEnv(t)
	before := e.picture(t)
	_, held := e.start(t)
	if e.spy.count() != 0 {
		t.Errorf("LLM calls = %d, want 0: the rules reached the threshold", e.spy.count())
	}
	if held.BelowThreshold || held.UsedLLM {
		t.Fatalf("precondition: draft below threshold %v, used LLM %v", held.BelowThreshold, held.UsedLLM)
	}
	if held.Reimport == nil || held.Reimport.RecipeID != e.recipeID || held.Reimport.BaseVersion != 1 {
		t.Errorf("Reimport = %+v, want recipe %d at version 1", held.Reimport, e.recipeID)
	}
	if held.Picture != PictureKeep || len(held.Candidates) != 2 {
		t.Errorf("picture %q, candidates %q; want Keep current and 2 candidates", held.Picture, held.Candidates)
	}
	if _, err := Confirm(held); err != nil {
		t.Errorf("a re-import that chose nothing must confirm on Keep current: %v", err)
	}
	if got := e.picture(t); got != before {
		t.Errorf("starting a re-import changed the picture: %q -> %q", before, got)
	}
	if n := e.recipes(t); n != 1 {
		t.Errorf("%d recipes, want 1", n)
	}
}

func TestSavingWithTheDefaultLeavesThePictureAndReplacesTheRecipe(t *testing.T) {
	e := newReimportEnv(t)
	before := e.picture(t)
	token, held := e.start(t)
	if len(e.objects(t)) != 3 {
		t.Fatalf("objects while waiting = %v, want the picture and 2 candidates", e.objects(t))
	}
	saved, err := e.keeper.SaveProposed(e.ctx, token, confirm(t, held))
	if err != nil || saved.PictureErr != nil {
		t.Fatalf("SaveProposed = %v, picture %v", err, saved.PictureErr)
	}
	if saved.RecipeID != e.recipeID || e.recipes(t) != 1 {
		t.Errorf("saved recipe %d of %d; want recipe %d replaced and no second recipe", saved.RecipeID, e.recipes(t), e.recipeID)
	}
	if got := e.picture(t); got != before {
		t.Errorf("picture = %q, want it left as it was: %q", got, before)
	}
	if objs := e.objects(t); len(objs) != 1 || objs[0] != before {
		t.Errorf("objects = %v, want only the recipe's picture", objs)
	}
	info, _ := e.store.Info(e.ctx, e.recipeID)
	if info.Version != 2 {
		t.Errorf("version = %d, want 2", info.Version)
	}
}

func TestSavingWithACandidateReplacesThePictureAndDeletesThePreviousObject(t *testing.T) {
	e := newReimportEnv(t)
	before := e.picture(t)
	token, held := e.start(t)
	if err := e.drafts.ChoosePicture(e.ctx, token, choice(held, 1)); err != nil {
		t.Fatal(err)
	}
	held, _ = e.drafts.Get(e.ctx, token)
	if saved, err := e.keeper.SaveProposed(e.ctx, token, confirm(t, held)); err != nil || saved.PictureErr != nil {
		t.Fatalf("SaveProposed = %v, picture %v", err, saved.PictureErr)
	}
	after := e.picture(t)
	if after == "" || after == before {
		t.Fatalf("picture = %q, want a new one replacing %q", after, before)
	}
	if objs := e.objects(t); len(objs) != 1 || objs[0] != after {
		t.Errorf("objects = %v, want only the new picture (the previous object deleted)", objs)
	}
}

func TestSavingWithNoPictureRemovesIt(t *testing.T) {
	e := newReimportEnv(t)
	token, _ := e.start(t)
	if err := e.drafts.ChoosePicture(e.ctx, token, PictureNone); err != nil {
		t.Fatal(err)
	}
	held, _ := e.drafts.Get(e.ctx, token)
	if saved, err := e.keeper.SaveProposed(e.ctx, token, confirm(t, held)); err != nil || saved.PictureErr != nil {
		t.Fatalf("SaveProposed = %v, picture %v", err, saved.PictureErr)
	}
	if e.picture(t) != "" || len(e.objects(t)) != 0 {
		t.Errorf("picture %q, objects %v; want none", e.picture(t), e.objects(t))
	}
}

func TestDiscardedAndSupersededReimportsLeaveNoCandidateObjects(t *testing.T) {
	e := newReimportEnv(t)
	kept := e.picture(t)
	first, _ := e.start(t)
	second, _ := e.start(t)
	if p, _ := e.store.GetProposal(e.ctx, first); p.State != store.ProposalSuperseded {
		t.Errorf("first re-import is %s, want superseded", p.State)
	}
	if objs := e.objects(t); len(objs) != 3 {
		t.Errorf("objects = %v, want the picture and only the second's 2 candidates", objs)
	}
	if err := e.drafts.Discard(e.ctx, second); err != nil {
		t.Fatal(err)
	}
	if objs := e.objects(t); len(objs) != 1 || objs[0] != kept {
		t.Errorf("objects = %v, want only the recipe's picture", objs)
	}
}

// ADR-00042 obligation 6: a recipe edited after the re-import started refuses
// the save; the recipe, its picture and the draft are as they were.
func TestSavingAStaleReimportIsRefused(t *testing.T) {
	e := newReimportEnv(t)
	before := e.picture(t)
	token, held := e.start(t)
	r, err := e.store.Get(e.ctx, e.recipeID)
	if err != nil {
		t.Fatal(err)
	}
	r.Name = "Edited meanwhile"
	if err := e.store.Update(e.ctx, e.recipeID, r); err != nil {
		t.Fatal(err)
	}
	_, err = e.keeper.SaveProposed(e.ctx, token, confirm(t, held))
	if !errors.Is(err, store.ErrStaleProposal) {
		t.Fatalf("SaveProposed after an edit = %v, want ErrStaleProposal", err)
	}
	if cur, _ := e.store.Get(e.ctx, e.recipeID); cur.Name != "Edited meanwhile" {
		t.Errorf("name = %q, want the edit kept", cur.Name)
	}
	if e.picture(t) != before || len(e.objects(t)) != 3 {
		t.Errorf("picture %q, objects %v; want the picture and candidates untouched", e.picture(t), e.objects(t))
	}
}

func TestStartKeepsTheRecipesSpiceLevel(t *testing.T) {
	e := newReimportEnv(t)
	r, _ := e.store.Get(e.ctx, e.recipeID)
	two := 2
	r.SpiceLevel = &two
	if err := e.store.Update(e.ctx, e.recipeID, r); err != nil {
		t.Fatal(err)
	}
	_, held := e.start(t)
	if held.SpiceLevel == nil || *held.SpiceLevel != 2 {
		t.Errorf("spice level = %v, want the recipe's 2", held.SpiceLevel)
	}
}

// ADR-00042 obligation 4: a failed fetch is an error and stores no draft.
func TestAFailedFetchHoldsNoDraft(t *testing.T) {
	e := newReimportEnv(t)
	down := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("connection refused") })
	pipe := e.pipeline(t, down)
	_, err := NewReimporter(e.store, pipe, e.drafts).Start(e.ctx, e.recipeID, store.Provenance{Person: auth.DevEmail})
	if !errors.Is(err, ErrFetch) {
		t.Fatalf("Start = %v, want ErrFetch", err)
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM proposals WHERE state = 'pending'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d proposals (%v), want none", n, err)
	}
}

func TestARecipeWithNoSourceURLCannotBeReimported(t *testing.T) {
	e := newReimportEnv(t)
	token, held := e.hold(t, e.urlDraft(t))
	saved, err := e.keeper.SaveProposed(e.ctx, token, confirm(t, held))
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.reimporter.Start(e.ctx, saved.RecipeID, store.Provenance{Person: auth.DevEmail})
	if !errors.Is(err, ErrNoSourceURL) {
		t.Errorf("Start = %v, want ErrNoSourceURL", err)
	}
}

func TestReimportMarkerSurvivesAnEditOfTheDraft(t *testing.T) {
	e := newReimportEnv(t)
	token, held := e.start(t)
	held.Name.Value = "Fluffier pancakes"
	if err := e.drafts.Replace(e.ctx, token, held); err != nil {
		t.Fatal(err)
	}
	back, _ := e.drafts.Get(e.ctx, token)
	if back.Reimport == nil || back.Name.Value != "Fluffier pancakes" {
		t.Errorf("draft after replace = %+v, want the marker and the edit", back.Reimport)
	}
	held.Reimport = nil
	if err := e.drafts.Replace(e.ctx, token, held); err == nil || strings.Contains(err.Error(), "no longer available") {
		t.Errorf("dropping the marker = %v, want a refusal", err)
	}
}
