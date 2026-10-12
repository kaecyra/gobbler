package ingest

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"

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
	if len(saved.NewIngredients) != 0 || len(saved.NewUnits) != 0 {
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
	if unrev, err := e.catalog.ListUnreviewedUnits(e.ctx); err != nil || len(unrev) != 0 {
		t.Fatalf("after ingest: unreviewed units %v, err %v", unrev, err)
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
	unrev, err := e.catalog.ListUnreviewedUnits(e.ctx)
	if err != nil || len(unrev) != 1 || unrev[0].Unit.Name != "bag" {
		t.Errorf("unreviewed units = %+v, err %v; want bag", unrev, err)
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
	if len(second.NewIngredients) != 0 || len(second.NewUnits) != 0 || second.RecipeID == first.RecipeID {
		t.Errorf("second save = %+v", second)
	}
}

func TestSaveRemovesRecipeWhenSnapshotFails(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipeline(t, nil).FromText(e.ctx, string(readFixture(t, "garlic_spaghetti.txt")))
	if err != nil {
		t.Fatal(err)
	}
	failing := failingSnapshots{RecipeStore: e.store}
	_, err = NewSaver(failing, e.catalog).Save(e.ctx, confirm(t, d))
	if err == nil || !strings.Contains(err.Error(), "recipe not saved") {
		t.Fatalf("err = %v", err)
	}
	if _, err := e.store.Get(e.ctx, 1); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("recipe left behind without its snapshot: %v", err)
	}
}

type failingSnapshots struct{ RecipeStore }

func (failingSnapshots) AddSnapshot(_ context.Context, _ int64, _ store.SnapshotKind, _ []byte) error {
	return errors.New("disk full")
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
