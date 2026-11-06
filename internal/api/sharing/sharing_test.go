package sharing_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	apisharing "github.com/kaecyra/gobbler/internal/api/sharing"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

const (
	annEmail = "ann@example.com"
	joEmail  = "jo@example.com"
)

type fixture struct {
	svc         *apisharing.Service
	rec         *store.Store
	ann, jo     apisharing.Caller
	stranger    apisharing.Caller
	annRecipeID int64
}

// identity returns a context carrying the verified identity, which only the
// auth middleware can mint; a decline records who made it.
func identity(t *testing.T) context.Context {
	t.Helper()
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}})
	if err != nil {
		t.Fatal(err)
	}
	var got context.Context
	a.Middleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Context() })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if got == nil {
		t.Fatal("middleware did not call the handler")
	}
	return got
}

func newFixture(t *testing.T) fixture {
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
	smiths, err := hs.Create(ctx, "Smiths", annEmail)
	if err != nil {
		t.Fatal(err)
	}
	joneses, err := hs.Create(ctx, "Joneses", joEmail)
	if err != nil {
		t.Fatal(err)
	}
	rec := store.New(d)
	svc, err := apisharing.New(apisharing.Deps{Recipes: rec, Households: hs, Catalog: catalog.New(d), Members: hs})
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{
		svc: svc, rec: rec,
		ann:      apisharing.Caller{Person: annEmail, Household: smiths},
		jo:       apisharing.Caller{Person: joEmail, Household: joneses},
		stranger: apisharing.Caller{Person: "nobody@example.com", Household: smiths},
	}
	f.annRecipeID, err = rec.Create(household.WithContext(identity(t), smiths), recipe.Recipe{
		Name:       "Stew",
		Components: []recipe.Component{{Steps: []recipe.Step{{ID: "s1", Text: "Stew it."}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestNewRefusesAMissingCollaborator(t *testing.T) {
	if _, err := apisharing.New(apisharing.Deps{}); err == nil {
		t.Fatal("New with no collaborators succeeded")
	}
}

func TestEveryOperationRefusesACallerThatIsNotAMember(t *testing.T) {
	f := newFixture(t)
	ctx := identity(t)
	ops := map[string]func() error{
		"Send":    func() error { return f.svc.Send(ctx, f.stranger, f.annRecipeID, joEmail) },
		"Inbox":   func() error { _, err := f.svc.Inbox(ctx, f.stranger); return err },
		"Accept":  func() error { _, err := f.svc.Accept(ctx, f.stranger, 1); return err },
		"Decline": func() error { return f.svc.Decline(ctx, f.stranger, 1) },
	}
	for name, op := range ops {
		if err := op(); !errors.Is(err, apisharing.ErrNotMember) {
			t.Errorf("%s as a non-member = %v, want ErrNotMember", name, err)
		}
	}
}

func TestAMemberSendsAndTheOtherHouseholdAccepts(t *testing.T) {
	f := newFixture(t)
	ctx := identity(t)
	if err := f.svc.Send(ctx, f.ann, f.annRecipeID, joEmail); err != nil {
		t.Fatalf("Send: %v", err)
	}
	offers, err := f.svc.Inbox(ctx, f.jo)
	if err != nil || len(offers) != 1 {
		t.Fatalf("Inbox = %v (%v), want one offer", offers, err)
	}
	if mine, err := f.svc.Inbox(ctx, f.ann); err != nil || len(mine) != 0 {
		t.Errorf("sender's Inbox = %v (%v), want empty", mine, err)
	}
	id, err := f.svc.Accept(ctx, f.jo, offers[0].ID)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if _, err := f.rec.Get(household.WithContext(ctx, f.jo.Household), id); err != nil {
		t.Errorf("accepted recipe is not in the Joneses' library: %v", err)
	}
}

func TestSendOfARecipeTheCallerLacksIsNotFound(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.Send(identity(t), f.jo, f.annRecipeID, annEmail); !errors.Is(err, apisharing.ErrNotFound) {
		t.Errorf("Send = %v, want ErrNotFound", err)
	}
}

func TestSendOfAMalformedAddressIsInvalid(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.Send(identity(t), f.ann, f.annRecipeID, "x"); !errors.Is(err, apisharing.ErrInvalid) {
		t.Errorf("Send = %v, want ErrInvalid", err)
	}
}

func TestDeclineLeavesTheLibraryAlone(t *testing.T) {
	f := newFixture(t)
	ctx := identity(t)
	if err := f.svc.Send(ctx, f.ann, f.annRecipeID, joEmail); err != nil {
		t.Fatal(err)
	}
	offers, _ := f.svc.Inbox(ctx, f.jo)
	if err := f.svc.Decline(ctx, f.jo, offers[0].ID); err != nil {
		t.Fatalf("Decline: %v", err)
	}
	if left, _ := f.svc.Inbox(ctx, f.jo); len(left) != 0 {
		t.Errorf("Inbox after decline = %v, want empty", left)
	}
}
