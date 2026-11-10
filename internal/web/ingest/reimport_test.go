package ingest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// fakeReimporter stands in for the pipeline, so no test reaches the network.
type fakeReimporter struct {
	token string
	err   error
	ids   []int64
}

func (f *fakeReimporter) Start(_ context.Context, id int64, _ store.Provenance) (string, error) {
	f.ids = append(f.ids, id)
	return f.token, f.err
}

// savedRecipe stores a recipe in f's database and returns its id.
func (f *fixture) savedRecipe(t *testing.T) int64 {
	t.Helper()
	id, err := store.New(f.db).Create(f.ctx(t), recipe.Recipe{Name: "Old pie", SourceURL: "https://example.test/pie", Components: []recipe.Component{{
		Lines: []recipe.IngredientLine{{Ingredient: 7, Rule: quantity.RuleLinear, Raw: "butter", Amount: quantity.Amount{Qty: quantity.Exact(quantity.Int(1))}}},
		Steps: []recipe.Step{{ID: "a", Text: "Bake."}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// holdReimport holds the sample draft as a re-import of recipe id from version 1.
func (f *fixture) holdReimport(t *testing.T, id int64) string {
	t.Helper()
	d := sampleDraft()
	// Only what the real catalog behind the store knows: the fake catalog's
	// proposed entries have ids it has not got.
	d.Components[0].Lines = d.Components[0].Lines[:1]
	d.Equipment = []ingestcore.Equipment{{Name: "dutch oven", Equipment: 5}}
	d.Proposed = nil
	d.Reimport = &ingestcore.Reimport{RecipeID: id, BaseVersion: 1}
	d.Picture = ingestcore.PictureKeep
	token, err := f.rs.drafts.Add(f.ctx(t), d, store.Provenance{Person: "dev@localhost"})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// reimportForm is reviewForm for the draft holdReimport holds.
func reimportForm() url.Values {
	v := reviewForm()
	for k := range v {
		if strings.HasPrefix(k, "c.0.l.1.") || strings.HasPrefix(k, "e.1.") || strings.HasPrefix(k, "e.2.") {
			delete(v, k)
		}
	}
	return v
}

func TestReimportRouteStartsTheReimportAndLandsOnReview(t *testing.T) {
	f := newFixture(t)
	fr := &fakeReimporter{token: "abc123"}
	f.rs.reimporter = fr
	rec := f.do(http.MethodPost, "/reimport/42", url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ingest/abc123" {
		t.Fatalf("POST /reimport/42 = %d to %q, want 303 to /ingest/abc123", rec.Code, rec.Header().Get("Location"))
	}
	if len(fr.ids) != 1 || fr.ids[0] != 42 {
		t.Errorf("started for %v, want recipe 42", fr.ids)
	}
	if rec := f.do(http.MethodGet, "/reimport/42", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /reimport/42 = %d, want 405: starting one writes", rec.Code)
	}
	if rec := f.do(http.MethodPost, "/reimport/zero", url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("POST /reimport/zero = %d, want 404", rec.Code)
	}
}

func TestReimportFailureShowsItsMessageAndHoldsNoDraft(t *testing.T) {
	f := newFixture(t)
	f.rs.reimporter = &fakeReimporter{err: fmt.Errorf("%w: get https://example.test/pie: connection refused", ingestcore.ErrFetch)}
	rec := f.do(http.MethodPost, "/reimport/7", url.Values{})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Could not fetch that page") || !strings.Contains(body, "The recipe is unchanged.") {
		t.Errorf("body lacks the fetch message and that the recipe is unchanged: %s", body)
	}
	if f.pendingDrafts(t) != 0 {
		t.Error("a failed re-import held a draft")
	}
	f.rs.reimporter = &fakeReimporter{err: fmt.Errorf("re-import: %w", ingestcore.ErrNoSourceURL)}
	if rec := f.do(http.MethodPost, "/reimport/7", url.Values{}); rec.Code != http.StatusConflict {
		t.Errorf("no source URL = %d, want 409", rec.Code)
	}
	f.rs.reimporter = &fakeReimporter{err: errors.New("disk on fire")}
	if rec := f.do(http.MethodPost, "/reimport/7", url.Values{}); rec.Code != http.StatusInternalServerError {
		t.Errorf("a pipeline failure = %d, want 500", rec.Code)
	}
}

// ADR-00042 obligations 7 and 8.
func TestReviewOfAReimportHasTheCalloutAndKeepsThePictureByDefault(t *testing.T) {
	f := newFixture(t)
	f.rs.pictures = twoCandidates{}
	f.rs.drafts = f.rs.drafts.WithPictures(twoCandidates{})
	token := f.holdReimport(t, f.savedRecipe(t))
	body := f.do(http.MethodGet, "/"+token, nil).Body.String()
	if !strings.Contains(body, "Saving replaces the current recipe. Your earlier edits are kept in History.") {
		t.Error("the re-import review lacks the callout")
	}
	keep := between(t, body, `data-testid="pick-keep"`, "</label>")
	if !strings.Contains(keep, "Keep current picture") || !strings.Contains(keep, " checked") || !strings.Contains(keep, `value="keep"`) {
		t.Errorf("the Keep current picture option = %s, want it offered and checked", keep)
	}
	if !strings.Contains(keep, "/recipes/1/picture") {
		t.Errorf("Keep current picture shows %s, want the recipe's picture", keep)
	}
	plain := f.do(http.MethodGet, "/"+f.ingestText(t), nil).Body.String()
	if strings.Contains(plain, "Saving replaces the current recipe") || strings.Contains(plain, "Keep current picture") {
		t.Error("an import's review shows the re-import callout or Keep current picture")
	}
}

func TestChoosingKeepOnAReimportIsAcceptedAndOnAnImportRefused(t *testing.T) {
	f := newFixture(t)
	f.rs.pictures = twoCandidates{}
	f.rs.drafts = f.rs.drafts.WithPictures(twoCandidates{})
	re := f.holdReimport(t, f.savedRecipe(t))
	if err := f.rs.drafts.ChoosePicture(f.ctx(t), re, offerB); err != nil {
		t.Fatal(err)
	}
	d, _ := f.rs.drafts.Get(f.ctx(t), re)
	if err := f.h.choosePicture(f.ctx(t), re, pictureKeep, &d); err != nil || d.Picture != ingestcore.PictureKeep {
		t.Fatalf("choosing keep = %v, picture %q; want Keep current", err, d.Picture)
	}
	held, _ := f.rs.drafts.Get(f.ctx(t), re)
	if held.Picture != ingestcore.PictureKeep {
		t.Errorf("held choice = %q, want Keep current kept", held.Picture)
	}
	imp := f.ingestText(t)
	d2, _ := f.rs.drafts.Get(f.ctx(t), imp)
	if err := f.h.choosePicture(f.ctx(t), imp, pictureKeep, &d2); err == nil {
		t.Error("an import accepted Keep current picture")
	}
}

// ADR-00042 obligation 5: the save writes a revision of the recipe.
func TestConfirmingAReimportReplacesTheRecipe(t *testing.T) {
	f := newFixture(t)
	id := f.savedRecipe(t)
	token := f.holdReimport(t, id)
	rec := f.do(http.MethodPost, "/"+token+"/confirm", reimportForm())
	if want := fmt.Sprintf("/recipes/%d-apple-pie", id); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
		t.Fatalf("confirm = %d to %q, want 303 to %s; body %s", rec.Code, rec.Header().Get("Location"), want, rec.Body)
	}
	info, err := store.New(f.db).Info(f.ctx(t), id)
	if err != nil || info.Version != 2 {
		t.Errorf("version = %d (%v), want 2: a revision of the same recipe", info.Version, err)
	}
	if n := len(f.store.recipes); n != 0 {
		t.Errorf("%d recipes created, want none: a re-import creates none", n)
	}
}

// ADR-00042 obligation 6, through the screen: the save is refused, says why and
// keeps the draft.
func TestConfirmingAStaleReimportIsRefusedAndExplained(t *testing.T) {
	f := newFixture(t)
	id := f.savedRecipe(t)
	token := f.holdReimport(t, id)
	rec := store.New(f.db)
	r, _ := rec.Get(f.ctx(t), id)
	r.Name = "Edited meanwhile"
	if err := rec.Update(f.ctx(t), id, r); err != nil {
		t.Fatal(err)
	}
	resp := f.do(http.MethodPost, "/"+token+"/confirm", reimportForm())
	if resp.Code != http.StatusUnprocessableEntity || !strings.Contains(resp.Body.String(), "The recipe changed since this re-import was made") {
		t.Fatalf("confirm = %d, want 422 saying the recipe changed; body %s", resp.Code, resp.Body)
	}
	if cur, _ := rec.Get(f.ctx(t), id); cur.Name != "Edited meanwhile" {
		t.Errorf("name = %q, want the edit untouched", cur.Name)
	}
	if f.pendingDrafts(t) != 1 {
		t.Error("the refused re-import is no longer pending")
	}
}

func TestCancellingAReimportReturnsToTheRecipe(t *testing.T) {
	f := newFixture(t)
	id := f.savedRecipe(t)
	token := f.holdReimport(t, id)
	resp := f.do(http.MethodPost, "/"+token+"/cancel", url.Values{})
	if want := fmt.Sprintf("/recipes/%d", id); resp.Code != http.StatusSeeOther || resp.Header().Get("Location") != want {
		t.Errorf("cancel = %d to %q, want 303 to %s", resp.Code, resp.Header().Get("Location"), want)
	}
	if f.pendingDrafts(t) != 0 {
		t.Error("the re-import is still pending")
	}
	other := f.ingestText(t)
	if resp := f.do(http.MethodPost, "/"+other+"/cancel", url.Values{}); resp.Header().Get("Location") != "/ingest?done="+other {
		t.Errorf("cancelling an import went to %q, want /ingest?done=%s", resp.Header().Get("Location"), other)
	}
}
