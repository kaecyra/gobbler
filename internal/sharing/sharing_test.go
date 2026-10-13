package sharing_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/sharing"
	"github.com/kaecyra/gobbler/internal/shopping"
	"github.com/kaecyra/gobbler/internal/store"
)

const (
	senderOnly  = "zz sender only fruit"
	senderAlias = "zz sender alias"
	betaMember  = auth.DevOtherEmail
)

// world is two households, alpha (the sender) and beta, and a recipe of alpha's
// that uses an ingredient from alpha's overlay.
type world struct {
	svc           *sharing.Service
	cat           *catalog.Store
	recipes       *store.Store
	db            *sql.DB
	alpha, beta   context.Context
	alphaH, betaH household.Household
	recipe        int64
}

func newWorld(t *testing.T) world {
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
	w := world{cat: catalog.New(d), recipes: store.New(d), db: d}
	if w.alphaH, err = hs.Create(ctx, "Alpha Kitchen", auth.DevEmail); err != nil {
		t.Fatal(err)
	}
	if w.betaH, err = hs.Create(ctx, "Beta Kitchen", betaMember); err != nil {
		t.Fatal(err)
	}
	id := identity(t)
	w.alpha, w.beta = household.WithContext(id, w.alphaH), household.WithContext(id, w.betaH)
	w.svc = sharing.New(w.recipes, hs, w.cat)

	own, err := w.cat.CreateIngredient(w.alpha, catalog.Ingredient{Name: senderOnly, Aisle: "produce", Aliases: []string{senderAlias}})
	if err != nil {
		t.Fatal(err)
	}
	r := recipe.Recipe{
		Name: "Shared Stew",
		Components: []recipe.Component{{
			Lines: []recipe.IngredientLine{{
				Amount: quantity.Amount{Qty: quantity.Exact(quantity.Int(2))}, Ingredient: recipe.IngredientID(own.ID),
				Rule: quantity.RuleLinear, Raw: "2 " + senderOnly,
			}},
			Steps: []recipe.Step{{ID: "s1", Text: "Stew it."}},
		}},
	}
	if w.recipe, err = w.recipes.Create(w.alpha, r); err != nil {
		t.Fatal(err)
	}
	return w
}

// identity returns a context carrying the verified identity, which only the
// auth middleware can mint.
func identity(t *testing.T) context.Context {
	t.Helper()
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}})
	if err != nil {
		t.Fatal(err)
	}
	var got context.Context
	a.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Context() })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if got == nil {
		t.Fatal("middleware did not call the handler")
	}
	return got
}

func (w world) shareCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM recipe_shares`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSendLooksTheSameForAMemberAndAStranger(t *testing.T) {
	w := newWorld(t)
	for name, email := range map[string]string{
		"a member of another household": betaMember,
		"the same address, shouted":     "  OTHER@LOCALHOST ",
		"an address nobody uses":        "nobody@e2e.example",
		"a member of the sender's own":  auth.DevEmail,
	} {
		if err := w.svc.Send(w.alpha, w.recipe, email); err != nil {
			t.Errorf("Send to %s = %v, want nil", name, err)
		}
	}
	// Only the two sends that reached beta's member made a share.
	if n := w.shareCount(t); n != 2 {
		t.Errorf("shares stored = %d, want the two sent to beta's member", n)
	}
	if offers, err := w.svc.Inbox(w.beta); err != nil || len(offers) != 2 {
		t.Errorf("beta's inbox = %v (%v)", offers, err)
	}
	if offers, err := w.svc.Inbox(w.alpha); err != nil || len(offers) != 0 {
		t.Errorf("alpha's inbox = %v (%v), want none", offers, err)
	}
}

func TestSendRefusesAMalformedAddressAndARecipeThatIsNotTheSenders(t *testing.T) {
	w := newWorld(t)
	if err := w.svc.Send(w.alpha, w.recipe, "not an email"); !errors.Is(err, household.ErrInvalid) {
		t.Errorf("malformed address = %v, want household.ErrInvalid", err)
	}
	// Beta does not have alpha's recipe: not found, for a member and a stranger alike.
	for _, email := range []string{auth.DevEmail, "nobody@e2e.example"} {
		if err := w.svc.Send(w.beta, w.recipe, email); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("send of a recipe beta does not have, to %s = %v, want store.ErrNotFound", email, err)
		}
	}
	if n := w.shareCount(t); n != 0 {
		t.Errorf("refused sends stored %d shares", n)
	}
}

func accepted(t *testing.T, w world) int64 {
	t.Helper()
	if err := w.svc.Send(w.alpha, w.recipe, betaMember); err != nil {
		t.Fatal(err)
	}
	offers, err := w.svc.Inbox(w.beta)
	if err != nil || len(offers) != 1 {
		t.Fatalf("inbox = %v (%v)", offers, err)
	}
	id, err := w.svc.Accept(w.beta, offers[0].ID)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	return id
}

func TestAcceptAddsAnUnreviewedEntryToTheRecipientsOverlayOnly(t *testing.T) {
	w := newWorld(t)
	id := accepted(t, w)

	got, err := w.recipes.Get(w.beta, id)
	if err != nil {
		t.Fatal(err)
	}
	ing, err := w.cat.GetIngredient(w.beta, int64(got.Components[0].Lines[0].Ingredient))
	if err != nil {
		t.Fatalf("the copy's ingredient is not in beta's catalog: %v", err)
	}
	if ing.Central || ing.Reviewed || ing.Name != senderOnly || ing.Aisle != shopping.Uncategorised {
		t.Errorf("new entry = %+v, want an unreviewed overlay entry in %q", ing, shopping.Uncategorised)
	}
	// Not the sender's entry: neither its alias nor its aisle came along.
	if len(ing.Aliases) != 0 {
		t.Errorf("the sender's aliases reached beta: %v", ing.Aliases)
	}
	if _, ok, err := w.cat.MatchIngredient(w.beta, senderAlias); err != nil || ok {
		t.Errorf("beta matches the sender's alias %q: ok=%v err=%v", senderAlias, ok, err)
	}
	own, _, err := w.cat.MatchIngredient(w.alpha, senderOnly)
	if err != nil {
		t.Fatal(err)
	}
	if own.ID == ing.ID {
		t.Errorf("the copy points at the sender's overlay entry %d", own.ID)
	}
	// And the sender's catalog gained nothing from the recipient.
	all, err := w.cat.ListIngredients(w.alpha)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range all {
		if a.ID == ing.ID {
			t.Errorf("beta's new entry is visible to alpha: %+v", a)
		}
	}
}

func TestAcceptReusesWhatTheRecipientsCatalogAlreadyKnows(t *testing.T) {
	w := newWorld(t)
	// Beta already has the ingredient, under its own name for it.
	mine, err := w.cat.CreateIngredient(w.beta, catalog.Ingredient{Name: "zz beta fruit", Aisle: "produce", Reviewed: true, Aliases: []string{senderOnly}})
	if err != nil {
		t.Fatal(err)
	}
	id := accepted(t, w)
	got, err := w.recipes.Get(w.beta, id)
	if err != nil {
		t.Fatal(err)
	}
	if int64(got.Components[0].Lines[0].Ingredient) != mine.ID {
		t.Errorf("the line points at %d, want beta's matching entry %d", got.Components[0].Lines[0].Ingredient, mine.ID)
	}
	list, err := w.cat.ListUnreviewedIngredients(w.beta)
	if err != nil || len(list) != 0 {
		t.Errorf("beta has unreviewed entries %v (%v), want none", list, err)
	}
}

func TestOnlyTheRecipientHouseholdCanActOnAShare(t *testing.T) {
	w := newWorld(t)
	if err := w.svc.Send(w.alpha, w.recipe, betaMember); err != nil {
		t.Fatal(err)
	}
	offers, err := w.svc.Inbox(w.beta)
	if err != nil || len(offers) != 1 {
		t.Fatal(offers, err)
	}
	for name, fn := range map[string]func() error{
		"offer":   func() error { _, err := w.svc.Offer(w.alpha, offers[0].ID); return err },
		"accept":  func() error { _, err := w.svc.Accept(w.alpha, offers[0].ID); return err },
		"decline": func() error { return w.svc.Decline(w.alpha, offers[0].ID) },
	} {
		if err := fn(); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("sender's %s = %v, want store.ErrNotFound", name, err)
		}
	}
	if recipes, err := w.recipes.Search(w.alpha, store.SearchQuery{}); err != nil || len(recipes) != 1 {
		t.Errorf("sender's recipes = %v (%v), want only its own", recipes, err)
	}
}
