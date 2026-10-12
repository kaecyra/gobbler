package ingest

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/shopping"
	"github.com/kaecyra/gobbler/internal/store"
)

func confirm(t *testing.T, d Draft) Confirmed {
	t.Helper()
	c, err := Confirm(d)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	return c
}

func TestSaveJSONLDWithSnapshotRoundTrip(t *testing.T) {
	e := newEnv(t)
	page := readFixture(t, "pancakes.html")
	srv, _ := serve(t, page)
	d, err := e.pipeline(t, srv.Client()).FromURL(e.ctx, srv.URL+"/pancakes")
	if err != nil {
		t.Fatal(err)
	}

	saved, err := e.saver().Save(e.ctx, confirm(t, d))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := e.store.Get(e.ctx, saved.RecipeID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Fluffy Pancakes" || got.SourceURL != d.Source.URL || got.Author != "Pat Baker" {
		t.Errorf("recipe = %q / %q / %q", got.Name, got.SourceURL, got.Author)
	}
	if len(got.Components) != 1 || len(got.Components[0].Lines) != 7 || len(got.Components[0].Steps) != 3 {
		t.Fatalf("components = %+v", got.Components)
	}
	if got.Times.Prep != d.Prep.Value || got.Times.Cook != d.Cook.Value {
		t.Errorf("times = %+v", got.Times)
	}
	snap, err := e.store.GetSnapshot(e.ctx, saved.RecipeID, store.SnapshotHTML)
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if !bytes.Equal(snap.Content, page) {
		t.Errorf("snapshot differs from the fetched page")
	}
	if len(saved.NewIngredients) != 0 {
		t.Errorf("all matched: nothing should be created, got %+v", saved)
	}
}

func TestSavePastedTextCreatesProposalsUnreviewedOnlyOnSave(t *testing.T) {
	e := newEnv(t)
	text := readFixture(t, "mystery_stew.txt")
	d, err := e.pipeline(t, nil).FromText(e.ctx, string(text))
	if err != nil {
		t.Fatal(err)
	}

	// Ingesting proposes, it does not create (ADR-00008 obligation 3).
	if len(d.Proposed) == 0 {
		t.Fatal("fixture should propose ingredients")
	}
	if unrev, err := e.catalog.ListUnreviewedIngredients(e.ctx); err != nil || len(unrev) != 0 {
		t.Fatalf("after ingest: unreviewed ingredients %v, err %v", unrev, err)
	}

	saved, err := e.saver().Save(e.ctx, confirm(t, d))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if len(saved.NewIngredients) != len(d.Proposed) {
		t.Errorf("created %d ingredients for %d proposals", len(saved.NewIngredients), len(d.Proposed))
	}
	for _, ing := range saved.NewIngredients {
		if ing.Reviewed || ing.Aisle != proposedAisle {
			t.Errorf("created ingredient %+v must be unreviewed in %q", ing, proposedAisle)
		}
	}
	if unrev, err := e.catalog.ListUnreviewedUnits(e.ctx); err != nil || len(unrev) != 0 {
		t.Errorf("unreviewed units = %+v, err %v; saving must not create units", unrev, err)
	}

	got, err := e.store.Get(e.ctx, saved.RecipeID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Mystery Stew" || len(got.Components[0].Lines) != 3 {
		t.Errorf("recipe = %+v", got)
	}
	snap, err := e.store.GetSnapshot(e.ctx, saved.RecipeID, store.SnapshotText)
	if err != nil || string(snap.Content) != string(text) {
		t.Errorf("snapshot = %q, err %v", snap.Content, err)
	}
}

func TestSaveReusesIngredientCreatedSinceTheDraft(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipeline(t, nil).FromText(e.ctx, string(readFixture(t, "mystery_stew.txt")))
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.saver().Save(e.ctx, confirm(t, d))
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.saver().Save(e.ctx, confirm(t, d))
	if err != nil {
		t.Fatalf("a second save of the same draft must reuse what the first created: %v", err)
	}
	if len(second.NewIngredients) != 0 || second.RecipeID == first.RecipeID {
		t.Errorf("second save = %+v", second)
	}
}

// The recipe and its snapshot reach the store in one call, so a failure leaves
// nothing to compensate for.
func TestSaveWritesRecipeAndSnapshotInOneStoreCall(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipeline(t, nil).FromText(e.ctx, string(readFixture(t, "garlic_spaghetti.txt")))
	if err != nil {
		t.Fatal(err)
	}
	rec := &recordingStore{RecipeStore: e.store}
	saved, err := NewSaver(rec, e.catalog).Save(e.ctx, confirm(t, d))
	if err != nil {
		t.Fatal(err)
	}
	if rec.calls != 1 || len(rec.snaps) != 1 || rec.snaps[0].Kind != d.Source.Kind || !bytes.Equal(rec.snaps[0].Content, d.Source.Content) {
		t.Errorf("store calls = %d, snapshots = %+v", rec.calls, rec.snaps)
	}
	if _, err := e.store.GetSnapshot(e.ctx, saved.RecipeID, d.Source.Kind); err != nil {
		t.Errorf("snapshot not stored: %v", err)
	}
}

func TestSaveReportsStoreFailureAndReturnsNoRecipe(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipeline(t, nil).FromText(e.ctx, string(readFixture(t, "garlic_spaghetti.txt")))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := NewSaver(failingStore{}, e.catalog).Save(e.ctx, confirm(t, d))
	if err == nil || !strings.Contains(err.Error(), "recipe not stored") || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("err = %v", err)
	}
	if saved.RecipeID != 0 {
		t.Errorf("RecipeID = %d on failure", saved.RecipeID)
	}
}

type recordingStore struct {
	RecipeStore
	calls int
	snaps []store.SnapshotInput
}

func (r *recordingStore) CreateWithSnapshots(ctx context.Context, rec recipe.Recipe, snaps ...store.SnapshotInput) (int64, error) {
	r.calls++
	r.snaps = append(r.snaps, snaps...)
	return r.RecipeStore.CreateWithSnapshots(ctx, rec, snaps...)
}

type failingStore struct{}

func (failingStore) CreateWithSnapshots(context.Context, recipe.Recipe, ...store.SnapshotInput) (int64, error) {
	return 0, errors.New("disk full")
}

func TestSaveRefusesUnconfirmed(t *testing.T) {
	e := newEnv(t)
	if _, err := e.saver().Save(e.ctx, Confirmed{}); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("err = %v, want ErrNotConfirmed", err)
	}
}

func TestConfirmRejectsIncompleteDrafts(t *testing.T) {
	e := newEnv(t)
	good, err := e.pipeline(t, nil).FromText(e.ctx, string(readFixture(t, "garlic_spaghetti.txt")))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Draft){
		"no name":         func(d *Draft) { d.Name = Field[string]{} },
		"no lines":        func(d *Draft) { d.Components = nil },
		"line no name":    func(d *Draft) { d.Components[0].Lines[0].Name = ""; d.Components[0].Lines[0].Ingredient = 0 },
		"edited snapshot": func(d *Draft) { d.Source.Content = append([]byte("x"), d.Source.Content...) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := good
			d.Components = append([]Component(nil), good.Components...)
			d.Components[0].Lines = append([]Line(nil), good.Components[0].Lines...)
			mutate(&d)
			if _, err := Confirm(d); !errors.Is(err, ErrIncompleteDraft) {
				t.Fatalf("err = %v, want ErrIncompleteDraft", err)
			}
		})
	}
	if _, err := Confirm(good); err != nil {
		t.Fatalf("the unmodified draft must confirm: %v", err)
	}
}

// TestNoExportedPathSavesAnUnconfirmedDraft pins the API surface (ADR-00006
// obligation 1): Confirm is the only function that takes a Draft, and Save the
// only way to write, taking only a Confirmed.
func TestNoExportedPathSavesAnUnconfirmedDraft(t *testing.T) {
	draft := reflect.TypeOf(Draft{})
	confirmed := reflect.TypeOf(Confirmed{})

	for _, tp := range []reflect.Type{reflect.TypeOf(&Saver{}), reflect.TypeOf(&Pipeline{})} {
		for i := 0; i < tp.NumMethod(); i++ {
			m := tp.Method(i)
			for j := 1; j < m.Type.NumIn(); j++ {
				if m.Type.In(j) == draft {
					t.Errorf("%s.%s accepts a Draft", tp, m.Name)
				}
			}
		}
	}
	save, ok := reflect.TypeOf(&Saver{}).MethodByName("Save")
	if !ok || save.Type.NumIn() != 3 || save.Type.In(2) != confirmed {
		t.Errorf("Saver.Save must take exactly a Confirmed")
	}
	for i := 0; i < confirmed.NumField(); i++ {
		if confirmed.Field(i).IsExported() {
			t.Errorf("Confirmed.%s is exported; a caller could build one by hand", confirmed.Field(i).Name)
		}
	}
}

func TestPackageDependsOnNoLLMProviderSDK(t *testing.T) { // ADR-00007 obligation 1
	if testing.Short() {
		t.Skip("shells out to go list")
	}
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		for _, sdk := range []string{"anthropic", "generative-ai-go", "google.golang.org/genai", "cloud.google.com", "openai"} {
			if strings.Contains(dep, sdk) {
				t.Errorf("dependency %s looks like a provider SDK", dep)
			}
		}
	}
}

func TestProposedAisleIsTheShoppingListsUncategorised(t *testing.T) {
	if proposedAisle != shopping.Uncategorised {
		t.Errorf("proposedAisle %q != shopping.Uncategorised %q", proposedAisle, shopping.Uncategorised)
	}
}

// The catalog owns units, so a unit it does not know fails the save instead of
// being created.
func TestSaveRefusesAUnitTheCatalogDoesNotKnow(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipeline(t, nil).FromText(e.ctx, string(readFixture(t, "garlic_spaghetti.txt")))
	if err != nil {
		t.Fatal(err)
	}
	d.Components[0].Lines[0].Amount.Unit = quantity.OtherUnit("zorb")
	if _, err := e.saver().Save(e.ctx, confirm(t, d)); !errors.Is(err, ErrUnknownUnit) || !strings.Contains(err.Error(), `unit "zorb" is not in the catalog`) {
		t.Fatalf("Save error = %v, want one naming the unknown unit", err)
	}
	if unrev, err := e.catalog.ListUnreviewedUnits(e.ctx); err != nil || len(unrev) != 0 {
		t.Errorf("unreviewed units = %+v, err %v; a refused save must not create units", unrev, err)
	}
}
