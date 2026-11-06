package journal_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	apijournal "github.com/kaecyra/gobbler/internal/api/journal"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

type fixture struct {
	svc    *apijournal.Service
	rec    *store.Store
	ann    apijournal.Caller // member of the Smiths
	jo     apijournal.Caller // member of the Joneses
	smiths household.Household
	recipe int64 // a Smiths recipe
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
	rec := store.New(d)
	svc, err := apijournal.New(apijournal.Deps{Recipes: rec, Members: hs})
	if err != nil {
		t.Fatal(err)
	}
	id, err := rec.Create(household.WithContext(verified(t, "ann@example.com"), smiths), recipe.Recipe{
		Name:       "Zz Journal Bread",
		Components: []recipe.Component{{Steps: []recipe.Step{{ID: "s1", Text: "Bake."}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{
		svc: svc, rec: rec, smiths: smiths, recipe: id,
		ann: apijournal.Caller{Person: "ann@example.com", Household: smiths},
		jo:  apijournal.Caller{Person: "jo@example.com", Household: joneses},
	}
}

// verified is a context carrying a verified identity, minted the only way it
// can be: by the auth middleware.
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

func TestNewRefusesAMissingCollaborator(t *testing.T) {
	if _, err := apijournal.New(apijournal.Deps{}); err == nil {
		t.Fatal("New with no collaborators succeeded")
	}
}

// Nobody who is not a verified member of a household gets an operation done,
// whatever the context carries (ADR-00018).
func TestEveryOperationRefusesANonMember(t *testing.T) {
	f := newFixture(t)
	// The context names the Smiths: the Caller, not the context, decides.
	ctx := household.WithContext(verified(t, "ann@example.com"), f.smiths)
	id, rating := f.recipe, 3
	ops := map[string]func(c apijournal.Caller) error{
		"Recipe":     func(c apijournal.Caller) error { _, err := f.svc.Recipe(ctx, c, id); return err },
		"ListNotes":  func(c apijournal.Caller) error { _, err := f.svc.ListNotes(ctx, c, id); return err },
		"ListMadeIt": func(c apijournal.Caller) error { _, err := f.svc.ListMadeIt(ctx, c, id); return err },
		"AddNote":    func(c apijournal.Caller) error { _, err := f.svc.AddNote(ctx, c, id, "salt", false); return err },
		"SetNotePinned": func(c apijournal.Caller) error {
			return f.svc.SetNotePinned(ctx, c, id, 1, true)
		},
		"UpdateNoteBody": func(c apijournal.Caller) error { return f.svc.UpdateNoteBody(ctx, c, id, 1, "x") },
		"DeleteNote":     func(c apijournal.Caller) error { return f.svc.DeleteNote(ctx, c, id, 1) },
		"AddMadeIt": func(c apijournal.Caller) error {
			_, err := f.svc.AddMadeIt(ctx, c, id, apijournal.MadeIt{Comment: "x"})
			return err
		},
		"SetRating": func(c apijournal.Caller) error { return f.svc.SetRating(ctx, c, id, &rating) },
	}
	callers := map[string]apijournal.Caller{
		"zero caller":     {},
		"no household":    {Person: "ann@example.com"},
		"no person":       {Household: f.ann.Household},
		"household no id": {Person: "ann@example.com", Household: household.Household{Name: "Smiths"}},
		"wrong household": {Person: "ann@example.com", Household: f.jo.Household},
	}
	for op, call := range ops {
		for who, c := range callers {
			if err := call(c); !errors.Is(err, apijournal.ErrNotMember) {
				t.Errorf("%s as %s: err = %v, want ErrNotMember", op, who, err)
			}
		}
	}
	notes, err := f.rec.ListNotes(ctx, id)
	if err != nil || len(notes) != 0 {
		t.Errorf("a refused AddNote must not write: notes = %v, err = %v", notes, err)
	}
}

// A member of another household cannot reach the recipe: it is not found.
func TestAnotherHouseholdSeesNoRecipe(t *testing.T) {
	f := newFixture(t)
	ctx := verified(t, "ann@example.com")
	if _, err := f.svc.AddNote(ctx, f.ann, f.recipe, "salt early", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ListNotes(ctx, f.jo, f.recipe); !errors.Is(err, apijournal.ErrNotFound) {
		t.Errorf("ListNotes as jo: err = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.AddNote(ctx, f.jo, f.recipe, "mine now", false); !errors.Is(err, apijournal.ErrNotFound) {
		t.Errorf("AddNote as jo: err = %v, want ErrNotFound", err)
	}
	if err := f.svc.SetRating(ctx, f.jo, f.recipe, nil); !errors.Is(err, apijournal.ErrNotFound) {
		t.Errorf("SetRating as jo: err = %v, want ErrNotFound", err)
	}
}

func TestNotesAndMadeItRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := verified(t, "ann@example.com")
	n, err := f.svc.AddNote(ctx, f.ann, f.recipe, "salt early", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UpdateNoteBody(ctx, f.ann, f.recipe, n.ID, "salt very early"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetNotePinned(ctx, f.ann, f.recipe, n.ID, false); err != nil {
		t.Fatal(err)
	}
	notes, err := f.svc.ListNotes(ctx, f.ann, f.recipe)
	if err != nil || len(notes) != 1 || notes[0].Body != "salt very early" || notes[0].Pinned {
		t.Fatalf("notes = %+v, err = %v", notes, err)
	}
	if err := f.svc.DeleteNote(ctx, f.ann, f.recipe, n.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteNote(ctx, f.ann, f.recipe, n.ID); !errors.Is(err, apijournal.ErrNotFound) {
		t.Errorf("second delete: err = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.AddNote(ctx, f.ann, f.recipe, " ", false); !errors.Is(err, apijournal.ErrInvalid) {
		t.Errorf("empty note: err = %v, want ErrInvalid", err)
	}
	if _, err := f.svc.AddMadeIt(ctx, f.ann, f.recipe, apijournal.MadeIt{Comment: "good"}); err != nil {
		t.Fatal(err)
	}
	made, err := f.svc.ListMadeIt(ctx, f.ann, f.recipe)
	if err != nil || len(made) != 1 || made[0].Comment != "good" {
		t.Fatalf("made = %+v, err = %v", made, err)
	}
}

// SetRating changes the recipe's rating and writes a revision.
func TestSetRatingWritesARevision(t *testing.T) {
	f := newFixture(t)
	ctx := verified(t, "ann@example.com")
	before, err := f.rec.ListRevisions(household.WithContext(ctx, f.smiths), f.recipe)
	if err != nil {
		t.Fatal(err)
	}
	four := 4
	if err := f.svc.SetRating(ctx, f.ann, f.recipe, &four); err != nil {
		t.Fatal(err)
	}
	rec, err := f.svc.Recipe(ctx, f.ann, f.recipe)
	if err != nil || rec.Rating == nil || *rec.Rating != 4 {
		t.Fatalf("rating = %v, err = %v", rec.Rating, err)
	}
	after, _ := f.rec.ListRevisions(household.WithContext(ctx, f.smiths), f.recipe)
	if len(after) != len(before)+1 {
		t.Errorf("revisions %d -> %d, want one more", len(before), len(after))
	}
	if err := f.svc.SetRating(ctx, f.ann, f.recipe, nil); err != nil {
		t.Fatal(err)
	}
	if rec, _ = f.svc.Recipe(ctx, f.ann, f.recipe); rec.Rating != nil {
		t.Errorf("rating after clear = %v, want nil", *rec.Rating)
	}
}
