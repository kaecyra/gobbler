package ingest

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// richDraft has a value in every field of a draft the document carries.
func richDraft(ing int64) Draft {
	content := []byte("<html>the page</html>")
	rng, err := quantity.NewRange(quantity.Int(1), quantity.Int(2))
	if err != nil {
		panic(err)
	}
	half, _ := quantity.NewRat(1, 2)
	cup, _ := quantity.LookupUnit("cup")
	d := newSource(store.SnapshotHTML, "https://example.test/pie", content)
	return Draft{
		ParserVersion: ParserVersion, Source: d, Method: extract.MethodJSONLD,
		Name: Field[string]{"Apple pie", 1, false}, Author: Field[string]{"Ada", 0.8, true}, Description: Field[string]{"Tart.", 0.5, false},
		Yield:    Field[recipe.Yield]{recipe.Yield{Qty: quantity.Exact(quantity.Int(8)), Unit: "slices"}, 0.9, false},
		Servings: Field[quantity.Quantity]{rng, 0.7, true}, Calories: Field[int]{310, 0.6, false},
		Prep: Field[time.Duration]{20 * time.Minute, 1, false}, Cook: Field[time.Duration]{time.Hour, 0.4, true}, Total: Field[time.Duration]{80 * time.Minute, 0.3, false},
		VideoID: "dQw4w9WgXcQ", MealType: recipe.MealType("dessert"), SpiceLevel: func() *int { n := 1; return &n }(), Cuisine: recipe.Cuisine(""),
		Components: []Component{{
			Name: "Filling", FromLLM: true,
			Lines: []Line{
				{Raw: "1/2 cup (14 oz) butter, melted", Name: "butter", Ingredient: recipe.IngredientID(ing), ToTaste: false, Optional: true, Confidence: 0.9,
					Amount:     quantity.Amount{Qty: quantity.Exact(half), Unit: cup, HasPackage: true, Package: quantity.PackageSize{Qty: quantity.Exact(quantity.Int(14)), Unit: quantity.OtherUnit("oz")}},
					Qualifiers: []recipe.Qualifier{{Kind: recipe.QualPreparation, Text: "melted"}}, Issues: []string{"unit_guessed"}, FromLLM: true},
				{Raw: "salt to taste", Name: "salt", ToTaste: true, Confidence: 0.4, Issues: []string{"no_quantity"}},
			},
			Steps: []Step{
				{Text: "Mix.", Duration: 5 * time.Minute, Confidence: 1, Kind: recipe.StageKind("prep"), Title: "Mix"},
				{Text: "Bake.", Duration: time.Hour, Confidence: 0.5, FromLLM: true, Passive: true, DependsOn: []int{1}, GraphCue: true, GraphFromLLM: true, GraphByUser: true},
			},
		}},
		Equipment: []Equipment{{Name: "dutch oven", Equipment: 5}, {Name: "flux capacitor", FromLLM: true}},
		Overall:   0.5, Threshold: 0.7, BelowThreshold: true, UsedLLM: true, LLMError: "timed out earlier",
		Issues:   []string{"no_steps"},
		Proposed: []ProposedIngredient{{Name: "dragonfruit zest", Aisle: "uncategorised"}},
	}
}

func snapshotOf(d Draft) store.Snapshot {
	return store.Snapshot{Kind: d.Source.Kind, Content: d.Source.Content, SHA256: d.Source.SHA256}
}

// A draft is stored as a document and read back whole, and the document is the
// same for the same draft: keeping drafts durably adds no state to ingestion
// and keeps it deterministic (ADR-00006, ADR-00035).
func TestDraftDocumentRoundTripsAndIsDeterministic(t *testing.T) {
	d := richDraft(7)
	doc, err := EncodeDraft(d)
	if err != nil {
		t.Fatal(err)
	}
	again, err := EncodeDraft(d)
	if err != nil || !bytes.Equal(doc, again) {
		t.Fatalf("one draft encoded twice differs (%v):\n%s\n%s", err, doc, again)
	}
	back, err := DecodeDraft(doc, snapshotOf(d))
	if err != nil {
		t.Fatal(err)
	}
	redone, err := EncodeDraft(back)
	if err != nil || !bytes.Equal(doc, redone) {
		t.Fatalf("a decoded draft encodes differently (%v):\n%s\n%s", err, doc, redone)
	}
	if back.Source.Kind != store.SnapshotHTML || back.Source.URL != d.Source.URL || !bytes.Equal(back.Source.Content, d.Source.Content) || !back.Source.intact() {
		t.Errorf("source = %+v, want the page, intact", back.Source)
	}
	l := back.Components[0].Lines[0]
	if l.Ingredient != 7 || !l.Amount.HasPackage || l.Amount.Package.Unit.Name != "oz" || l.Amount.Unit.Name != "cup" || !l.Amount.Unit.Converts() {
		t.Errorf("line = %+v, want the ingredient, the package and the cup with its factor", l)
	}
	if !back.Servings.Value.IsRange() || back.Servings.Value.Max().String() != "2" || !back.Servings.FromLLM {
		t.Errorf("servings = %+v, want the range and its LLM mark", back.Servings)
	}
	if st := back.Components[0].Steps[1]; !st.Passive || len(st.DependsOn) != 1 || st.DependsOn[0] != 1 || !st.GraphByUser || !st.GraphFromLLM || !st.GraphCue {
		t.Errorf("step = %+v, want its graph and the marks on it", st)
	}
	if len(back.Equipment) != 2 || back.Equipment[1].Name != "flux capacitor" || !back.Equipment[1].FromLLM {
		t.Errorf("equipment = %+v", back.Equipment)
	}
	if back.Method != extract.MethodJSONLD || back.UsedLLM != true || back.LLMError != "timed out earlier" || back.Name.Confidence != 1 || back.Overall != 0.5 {
		t.Errorf("draft = %+v, want parse method, LLM state and confidence kept", back)
	}
}

func (e *env) otherCtx(t *testing.T) context.Context {
	t.Helper()
	h, err := household.New(e.db).Create(e.ctx, "Other", auth.DevOtherEmail)
	if err != nil {
		t.Fatal(err)
	}
	return household.WithContext(e.ctx, h)
}

func TestDraftsHoldReplaceAndDiscard(t *testing.T) {
	e := newEnv(t)
	drafts := NewDrafts(e.store)
	d := richDraft(0)
	token, err := drafts.Add(e.ctx, d, store.Provenance{Person: auth.DevEmail})
	if err != nil {
		t.Fatal(err)
	}
	got, err := drafts.Get(e.ctx, token)
	if err != nil || got.Name.Value != "Apple pie" || !bytes.Equal(got.Source.Content, d.Source.Content) {
		t.Fatalf("Get = %+v, %v; want the draft with its source", got, err)
	}
	other := e.otherCtx(t)
	if _, err := drafts.Get(other, token); !errors.Is(err, ErrDraftGone) {
		t.Errorf("another household's Get = %v, want ErrDraftGone", err)
	}
	if err := drafts.Discard(other, token); !errors.Is(err, ErrDraftGone) {
		t.Errorf("another household's Discard = %v, want ErrDraftGone", err)
	}
	if _, err := drafts.Get(e.ctx, token); err != nil {
		t.Errorf("the owner's draft after a stranger's discard: %v", err)
	}
	if _, err := drafts.Get(e.ctx, "no-such-token"); !errors.Is(err, ErrDraftGone) {
		t.Errorf("an unknown token = %v, want ErrDraftGone", err)
	}

	d.Name = Field[string]{Value: "Better pie", Confidence: 1}
	d.Source.Content = []byte("tampered; the snapshot is what counts")
	if err := drafts.Replace(e.ctx, token, d); err != nil {
		t.Fatal(err)
	}
	got, _ = drafts.Get(e.ctx, token)
	if got.Name.Value != "Better pie" || string(got.Source.Content) != "<html>the page</html>" {
		t.Errorf("after Replace: name %q, source %q; want the new name and the source as first ingested", got.Name.Value, got.Source.Content)
	}

	if err := drafts.Discard(e.ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := drafts.Get(e.ctx, token); !errors.Is(err, ErrDraftGone) {
		t.Errorf("a discarded draft = %v, want ErrDraftGone", err)
	}
	if err := drafts.Replace(e.ctx, token, d); !errors.Is(err, ErrDraftGone) {
		t.Errorf("Replace of a discarded draft = %v, want ErrDraftGone", err)
	}
}

// ADR-00035 obligation 16, the ingestion end of the contract the store's merge
// relies on: the ingredient ids of a draft's document are found and re-pointed
// by the store, and the draft reads back whole with the winner in place.
func TestDraftDocumentIngredientsSurviveMerge(t *testing.T) {
	e := newEnv(t)
	e.store.RegisterMergeHooks(e.catalog)
	mk := func(name string) int64 {
		in, err := e.catalog.CreateIngredient(e.ctx, catalog.Ingredient{Name: name, Aisle: "test"})
		if err != nil {
			t.Fatal(err)
		}
		return in.ID
	}
	loser, winner := mk("zz draft loser"), mk("zz draft winner")
	drafts := NewDrafts(e.store)
	d := richDraft(loser)
	token, err := drafts.Add(e.ctx, d, store.Provenance{Person: auth.DevEmail})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.catalog.MergeIngredients(e.ctx, winner, loser); err != nil {
		t.Fatalf("a merge of an ingredient a pending draft names: %v", err)
	}
	got, err := drafts.Get(e.ctx, token)
	if err != nil {
		t.Fatalf("the draft no longer loads: %v", err)
	}
	if id := got.Components[0].Lines[0].Ingredient; id != recipe.IngredientID(winner) {
		t.Errorf("the draft's line names ingredient %d, want the winner %d", id, winner)
	}
	d.Components[0].Lines[0].Ingredient = recipe.IngredientID(winner)
	want, _ := EncodeDraft(d)
	have, _ := EncodeDraft(got)
	if !bytes.Equal(want, have) {
		t.Errorf("the merge changed more than the ingredient:\nwant %s\n got %s", want, have)
	}
}

// Confirming a held draft stores the recipe, attaches the source snapshot
// written when the draft was made, keeps the parser version, and cannot be done
// twice.
func TestSaveProposedAttachesTheSnapshotAndConfirmsOnce(t *testing.T) {
	e := newEnv(t)
	text := string(readFixture(t, "mystery_stew.txt"))
	d, err := e.pipeline(t, nil).FromText(e.ctx, text)
	if err != nil {
		t.Fatal(err)
	}
	drafts := NewDrafts(e.store)
	token, err := drafts.Add(e.ctx, d, store.Provenance{Person: auth.DevEmail})
	if err != nil {
		t.Fatal(err)
	}
	held, err := drafts.Get(e.ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := e.saver().SaveProposed(e.ctx, token, confirm(t, held))
	if err != nil {
		t.Fatalf("SaveProposed: %v", err)
	}
	snap, err := e.store.GetSnapshot(e.ctx, saved.RecipeID, store.SnapshotText)
	if err != nil || string(snap.Content) != text || snap.ParserVersion != ParserVersion {
		t.Errorf("snapshot = %+v, %v; want the pasted text and the parser version", snap, err)
	}
	r, err := e.store.Get(e.ctx, saved.RecipeID)
	if err != nil {
		t.Fatal(err)
	}
	for ci, c := range r.Components {
		for _, s := range c.Steps {
			if !recipe.IsStepKey(s.ID) {
				t.Errorf("step %q of component %d is not a stored key: the draft's labels become keys on save", s.ID, ci+1)
			}
		}
		for _, l := range c.Lines {
			if !recipe.IsLineKey(l.Key) {
				t.Errorf("line key %q is not a stored key", l.Key)
			}
		}
	}
	if _, err := e.saver().SaveProposed(e.ctx, token, confirm(t, held)); !errors.Is(err, store.ErrNotPending) {
		t.Errorf("confirming the same draft twice = %v, want store.ErrNotPending", err)
	}
	if _, err := drafts.Get(e.ctx, token); !errors.Is(err, ErrDraftGone) {
		t.Errorf("a confirmed draft still waits for review: %v", err)
	}
	cards, err := e.store.Cards(e.ctx, store.SearchQuery{})
	if err != nil || len(cards) != 1 {
		t.Errorf("%d recipes after confirming twice (%v), want 1", len(cards), err)
	}
}

// The draft ingestion makes is the same whether or not it is held: the same
// input and parser version give the same document (ADR-00006).
func TestHeldDraftsOfTheSameInputAreTheSameDocument(t *testing.T) {
	e := newEnv(t)
	text := string(readFixture(t, "mystery_stew.txt"))
	var docs [2][]byte
	for i := range docs {
		d, err := e.pipeline(t, nil).FromText(e.ctx, text)
		if err != nil {
			t.Fatal(err)
		}
		if docs[i], err = EncodeDraft(d); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(docs[0], docs[1]) {
		t.Errorf("one input gave two documents:\n%s\n%s", docs[0], docs[1])
	}
	if strings.Contains(string(docs[0]), "stp-") || strings.Contains(string(docs[0]), "lin-") {
		t.Error("the draft document holds keys: the store assigns them on save, not ingestion")
	}
}
