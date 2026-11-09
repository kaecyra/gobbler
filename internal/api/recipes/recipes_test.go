package recipes_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/recipe"
)

type fixture struct {
	t       *testing.T
	d       *sql.DB
	svc     *apirecipes.Service
	ann     apirecipes.Caller // member of the Smiths
	jo      apirecipes.Caller // member of the Joneses
	smithsK apirecipes.Caller // the Smiths' household key
	recipe  int64             // a Smiths recipe, at version 1
	reaper  *fakeReaper
}

// fakeReaper records what the service asks the media store to delete.
type fakeReaper struct{ collected, removed []string }

func (f *fakeReaper) CollectRecipe(_ context.Context, _ *sql.Tx, _, _ int64) ([]string, error) {
	f.collected = append(f.collected, "recipe/picture")
	return []string{"recipe/picture"}, nil
}

func (f *fakeReaper) Remove(_ context.Context, keys []string) { f.removed = append(f.removed, keys...) }

func verified(t *testing.T, email string) context.Context {
	t.Helper()
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:0", DevAuthBypass: true}})
	if err != nil {
		t.Fatal(err)
	}
	var got context.Context
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(auth.DevIdentityHeader, email)
	a.Middleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Context() })).ServeHTTP(httptest.NewRecorder(), req)
	if got == nil {
		t.Fatal("auth middleware did not pass the request on")
	}
	return got
}

func sample(name string) recipe.Recipe {
	return recipe.Recipe{Name: name, Components: []recipe.Component{{Steps: []recipe.Step{{ID: "s1", Text: "Bake."}}}}}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	hs := household.New(d)
	smiths, err := hs.Create(ctx, "Smiths", "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	joneses, err := hs.Create(ctx, "Joneses", "jo@example.com")
	if err != nil {
		t.Fatal(err)
	}
	reaper := &fakeReaper{}
	svc, err := apirecipes.New(apirecipes.Deps{Recipes: apirecipes.NewStore(d), Catalog: catalog.New(d), Members: hs, Reaper: reaper})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{
		t: t, d: d, svc: svc, reaper: reaper,
		ann:     apirecipes.Caller{Person: "ann@example.com", Household: smiths},
		jo:      apirecipes.Caller{Person: "jo@example.com", Household: joneses},
		smithsK: apirecipes.Caller{Household: smiths, HouseholdKey: true},
	}
	as := household.WithContext(verified(t, "ann@example.com"), smiths)
	f.recipe, err = apirecipes.NewStore(d).Create(as, sample("Zz Api Bread"))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestNewRefusesAMissingCollaborator(t *testing.T) {
	if _, err := apirecipes.New(apirecipes.Deps{}); err == nil {
		t.Fatal("New with no collaborators succeeded")
	}
}

// Nobody who is not a verified member of a household gets an operation done,
// whatever the context carries (ADR-00018).
func TestEveryOperationRefusesANonMember(t *testing.T) {
	f := newFixture(t)
	// The context names the Smiths and their member: the Caller, not the
	// context, decides.
	ctx := household.WithContext(verified(t, "ann@example.com"), f.ann.Household)
	id, one := f.recipe, int64(1)
	ops := map[string]func(c apirecipes.Caller) error{
		"Get":          func(c apirecipes.Caller) error { _, err := f.svc.Get(ctx, c, id); return err },
		"Info":         func(c apirecipes.Caller) error { _, err := f.svc.Info(ctx, c, id); return err },
		"Search":       func(c apirecipes.Caller) error { _, err := f.svc.Search(ctx, c, apirecipes.SearchQuery{}); return err },
		"Cards":        func(c apirecipes.Caller) error { _, err := f.svc.Cards(ctx, c, apirecipes.SearchQuery{}); return err },
		"RecentlyMade": func(c apirecipes.Caller) error { _, err := f.svc.RecentlyMade(ctx, c, 5); return err },
		"Idle":         func(c apirecipes.Caller) error { _, err := f.svc.Idle(ctx, c); return err },
		"RecipeCountsByIngredient": func(c apirecipes.Caller) error {
			_, err := f.svc.RecipeCountsByIngredient(ctx, c)
			return err
		},
		"ListNotes":  func(c apirecipes.Caller) error { _, err := f.svc.ListNotes(ctx, c, id); return err },
		"ListMadeIt": func(c apirecipes.Caller) error { _, err := f.svc.ListMadeIt(ctx, c, id); return err },
		"ListRevisions": func(c apirecipes.Caller) error {
			_, err := f.svc.ListRevisions(ctx, c, id)
			return err
		},
		"GetVersion": func(c apirecipes.Caller) error { _, err := f.svc.GetVersion(ctx, c, id, one); return err },
		"UpdateIfVersion": func(c apirecipes.Caller) error {
			return f.svc.UpdateIfVersion(ctx, c, id, one, sample("Hijacked"))
		},
		"Restore":      func(c apirecipes.Caller) error { return f.svc.Restore(ctx, c, id, one) },
		"Clone":        func(c apirecipes.Caller) error { _, err := f.svc.Clone(ctx, c, id); return err },
		"Delete":       func(c apirecipes.Caller) error { return f.svc.Delete(ctx, c, id) },
		"HasThumbnail": func(c apirecipes.Caller) error { _, err := f.svc.HasThumbnail(ctx, c, "abcdefghijk"); return err },
		"Thumbnail":    func(c apirecipes.Caller) error { _, err := f.svc.Thumbnail(ctx, c, id); return err },
		"GetProposal":  func(c apirecipes.Caller) error { _, err := f.svc.GetProposal(ctx, c, "tok"); return err },
		"PendingEdit":  func(c apirecipes.Caller) error { _, _, err := f.svc.PendingEdit(ctx, c, id); return err },
		"PendingReimport": func(c apirecipes.Caller) error {
			_, _, err := f.svc.PendingReimport(ctx, c, id)
			return err
		},
		"ProposeEdit": func(c apirecipes.Caller) error {
			_, err := f.svc.ProposeEdit(ctx, c, id, one, sample("Proposed"), apirecipes.Provenance{Person: "ann@example.com"})
			return err
		},
		"RejectProposal": func(c apirecipes.Caller) error { return f.svc.RejectProposal(ctx, c, "tok") },
		"AcceptEdit": func(c apirecipes.Caller) error {
			return f.svc.AcceptEdit(ctx, c, "tok", one, sample("Accepted"))
		},
		"DraftsFull": func(c apirecipes.Caller) error { _, err := f.svc.DraftsFull(ctx, c); return err },
		"AddDraft": func(c apirecipes.Caller) error {
			_, err := f.svc.AddDraft(ctx, c, ingest.Draft{}, apirecipes.Provenance{Person: "ann@example.com"})
			return err
		},
		"GetDraft":     func(c apirecipes.Caller) error { _, err := f.svc.GetDraft(ctx, c, "tok"); return err },
		"ReplaceDraft": func(c apirecipes.Caller) error { return f.svc.ReplaceDraft(ctx, c, "tok", ingest.Draft{}) },
		"DiscardDraft": func(c apirecipes.Caller) error { return f.svc.DiscardDraft(ctx, c, "tok") },
		"ReviseDraft": func(c apirecipes.Caller) error {
			_, err := f.svc.ReviseDraft(ctx, c, "tok", "rev", apirecipes.DraftRevision{}, "because")
			return err
		},
		"PendingDrafts": func(c apirecipes.Caller) error { _, err := f.svc.PendingDrafts(ctx, c); return err },
		"ChoosePicture": func(c apirecipes.Caller) error {
			return f.svc.ChoosePicture(ctx, c, "tok", ingest.PictureNone)
		},
		"SaveProposed": func(c apirecipes.Caller) error {
			_, err := f.svc.SaveProposed(ctx, c, "tok", ingest.Confirmed{})
			return err
		},
		"StartReimport": func(c apirecipes.Caller) error {
			_, err := f.svc.StartReimport(ctx, c, id, nil, apirecipes.Provenance{Person: "ann@example.com"})
			return err
		},
		"PictureKey": func(c apirecipes.Caller) error { _, _, err := f.svc.PictureKey(ctx, c, id); return err },
		"Candidates": func(c apirecipes.Caller) error { _, err := f.svc.Candidates(ctx, c, one); return err },
	}
	callers := map[string]apirecipes.Caller{
		"zero caller":     {},
		"no household":    {Person: "ann@example.com"},
		"no person":       {Household: f.ann.Household},
		"household no id": {Person: "ann@example.com", Household: household.Household{Name: "Smiths"}},
		"a member of another household naming the Smiths": {Person: "jo@example.com", Household: f.ann.Household},
		"a stranger": {Person: "eve@example.com", Household: f.ann.Household},
	}
	for op, call := range ops {
		for who, c := range callers {
			if err := call(c); !errors.Is(err, apirecipes.ErrNotMember) {
				t.Errorf("%s as %s: err = %v, want ErrNotMember", op, who, err)
			}
		}
	}
	// Nothing a refused call tried was written.
	got, err := f.svc.Get(ctx, f.ann, id)
	if err != nil || got.Name != "Zz Api Bread" {
		t.Errorf("recipe after refused calls = %q, %v, want it untouched", got.Name, err)
	}
	if len(f.reaper.collected) != 0 {
		t.Errorf("a refused Delete reached the media store: %v", f.reaper.collected)
	}
}

// A member of another household cannot reach a recipe: it is not found, never
// forbidden, so its existence is not disclosed (ADR-00018).
func TestAnotherHouseholdsRecipeIsNotFound(t *testing.T) {
	f := newFixture(t)
	ctx := verified(t, "jo@example.com")
	if _, err := f.svc.Get(ctx, f.jo, f.recipe); !errors.Is(err, apirecipes.ErrNotFound) {
		t.Errorf("Get as the other household = %v, want ErrNotFound", err)
	}
	if err := f.svc.UpdateIfVersion(ctx, f.jo, f.recipe, 1, sample("Hijacked")); !errors.Is(err, apirecipes.ErrNotFound) {
		t.Errorf("UpdateIfVersion as the other household = %v, want ErrNotFound", err)
	}
	if err := f.svc.Delete(ctx, f.jo, f.recipe); !errors.Is(err, apirecipes.ErrNotFound) {
		t.Errorf("Delete as the other household = %v, want ErrNotFound", err)
	}
	if found, err := f.svc.Search(ctx, f.jo, apirecipes.SearchQuery{}); err != nil || len(found) != 0 {
		t.Errorf("Search as the other household = %v, %v, want none", found, err)
	}
	if _, err := f.svc.Get(verified(t, "ann@example.com"), f.ann, f.recipe); err != nil {
		t.Errorf("the recipe is gone for its owner: %v", err)
	}
}

// A household key has no person, only its household (ADR-00031). It reads that
// household's recipes and nothing of another's. The store's writes need a
// verified person on the context, so a key with none cannot save over a
// recipe: it reaches a recipe's content only through a proposal a person
// reviews (ADR-00035).
func TestAHouseholdKeyIsScopedToItsHousehold(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	got, err := f.svc.Get(ctx, f.smithsK, f.recipe)
	if err != nil || got.Name != "Zz Api Bread" {
		t.Fatalf("Get with the Smiths' key = %q, %v", got.Name, err)
	}
	if found, err := f.svc.Search(ctx, f.smithsK, apirecipes.SearchQuery{}); err != nil || len(found) != 1 {
		t.Errorf("Search with the Smiths' key = %v, %v, want their one recipe", found, err)
	}
	// The Joneses' key reaches the Joneses, not the Smiths.
	jonesK := apirecipes.Caller{Household: f.jo.Household, HouseholdKey: true}
	if _, err := f.svc.Get(ctx, jonesK, f.recipe); !errors.Is(err, apirecipes.ErrNotFound) {
		t.Errorf("Get of the Smiths' recipe with the Joneses' key = %v, want ErrNotFound", err)
	}
	if found, err := f.svc.Search(ctx, jonesK, apirecipes.SearchQuery{}); err != nil || len(found) != 0 {
		t.Errorf("Search with the Joneses' key = %v, %v, want none", found, err)
	}
	// A key with no household is admitted nowhere.
	if _, err := f.svc.Get(ctx, apirecipes.Caller{HouseholdKey: true}, f.recipe); !errors.Is(err, apirecipes.ErrNotMember) {
		t.Errorf("Get with a key that names no household = %v, want ErrNotMember", err)
	}
	// Proposing an edit is how an agent changes a recipe: it holds the change.
	token, err := f.svc.ProposeEdit(ctx, f.smithsK, f.recipe, 1, sample("Zz Api Bread v2"), apirecipes.Provenance{Person: "household:1", Credential: &apirecipes.Credential{Kind: "api_key", ID: 1, Owner: "household:1"}})
	if err != nil {
		t.Fatalf("ProposeEdit with the Smiths' key: %v", err)
	}
	p, err := f.svc.GetProposal(ctx, f.smithsK, token)
	if err != nil || p.RecipeID != f.recipe || p.State != apirecipes.ProposalPending {
		t.Errorf("proposal = %+v, %v, want pending for the recipe", p, err)
	}
	if _, err := f.svc.GetProposal(ctx, jonesK, token); !errors.Is(err, apirecipes.ErrNotFound) {
		t.Errorf("GetProposal with the Joneses' key = %v, want ErrNotFound", err)
	}
	if got, _ := f.svc.Get(ctx, f.smithsK, f.recipe); got.Name != "Zz Api Bread" {
		t.Errorf("a proposal changed the recipe to %q", got.Name)
	}
}

// A save over a recipe that has moved on is refused and writes nothing.
func TestAStaleSaveIsRefusedAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	ctx := verified(t, "ann@example.com")
	if err := f.svc.UpdateIfVersion(ctx, f.ann, f.recipe, 1, sample("Second")); err != nil {
		t.Fatal(err)
	}
	err := f.svc.UpdateIfVersion(ctx, f.ann, f.recipe, 1, sample("Stale"))
	if !errors.Is(err, apirecipes.ErrStaleVersion) {
		t.Fatalf("a save from version 1 at version 2 = %v, want ErrStaleVersion", err)
	}
	if got, _ := f.svc.Get(ctx, f.ann, f.recipe); got.Name != "Second" {
		t.Errorf("recipe is %q after a refused stale save, want Second", got.Name)
	}
	if _, err := f.svc.ProposeEdit(ctx, f.ann, f.recipe, 1, sample("Stale proposal"), apirecipes.Provenance{Person: "ann@example.com"}); !errors.Is(err, apirecipes.ErrStaleVersion) {
		t.Errorf("a proposal from version 1 at version 2 = %v, want ErrStaleVersion", err)
	}
}

// Delete takes the recipe's picture objects with it, through the service's
// reaper (ADR-00039).
func TestDeleteRemovesThePictureObjects(t *testing.T) {
	f := newFixture(t)
	ctx := verified(t, "ann@example.com")
	if err := f.svc.Delete(ctx, f.ann, f.recipe); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Get(ctx, f.ann, f.recipe); !errors.Is(err, apirecipes.ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
	if len(f.reaper.removed) != 1 || f.reaper.removed[0] != "recipe/picture" {
		t.Errorf("objects removed = %v, want the recipe's picture", f.reaper.removed)
	}
}

func TestAMemberReadsAndRestoresAndClones(t *testing.T) {
	f := newFixture(t)
	ctx := verified(t, "ann@example.com")
	if err := f.svc.UpdateIfVersion(ctx, f.ann, f.recipe, 1, sample("Second")); err != nil {
		t.Fatal(err)
	}
	revs, err := f.svc.ListRevisions(ctx, f.ann, f.recipe)
	if err != nil || len(revs) != 1 {
		t.Fatalf("revisions = %v, %v, want the one replaced version", revs, err)
	}
	if err := f.svc.Restore(ctx, f.ann, f.recipe, 1); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.Get(ctx, f.ann, f.recipe); got.Name != "Zz Api Bread" {
		t.Errorf("restored recipe is %q", got.Name)
	}
	cloneID, err := f.svc.Clone(ctx, f.ann, f.recipe)
	if err != nil || cloneID == f.recipe {
		t.Fatalf("Clone = %d, %v", cloneID, err)
	}
	if cards, err := f.svc.Cards(ctx, f.ann, apirecipes.SearchQuery{}); err != nil || len(cards) != 2 {
		t.Errorf("cards = %d, %v, want the recipe and its clone", len(cards), err)
	}
}
