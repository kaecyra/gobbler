package store_test

import (
	"context"
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
	"github.com/kaecyra/gobbler/internal/shopping"
	"github.com/kaecyra/gobbler/internal/store"
)

// ADR-00035 obligation 6. A pending proposal is not the recipe: the change it
// proposes appears in neither search nor a shopping list built from the recipe.
// The test lives outside package store because shopping imports it.
func TestPendingProposalIsInvisibleToSearchAndShopping(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	h, err := household.New(d).Create(ctx, "Home", auth.DevEmail)
	if err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}})
	if err != nil {
		t.Fatal(err)
	}
	var rctx context.Context
	a.Middleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { rctx = r.Context() })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	rctx = household.WithContext(rctx, h)

	cat := catalog.New(d)
	mk := func(name string) int64 {
		in, err := cat.CreateIngredient(rctx, catalog.Ingredient{Name: name, Aisle: "test"})
		if err != nil {
			t.Fatal(err)
		}
		return in.ID
	}
	salt, sumac := mk("zz visibility salt"), mk("zz visibility sumac")
	line := func(ing int64, raw string) recipe.IngredientLine {
		return recipe.IngredientLine{Ingredient: recipe.IngredientID(ing), Rule: quantity.RuleLinear, Raw: raw, Amount: quantity.Amount{Qty: quantity.Exact(quantity.Int(2))}}
	}
	rec := store.New(d)
	live := recipe.Recipe{Name: "Plain dish", Components: []recipe.Component{{Lines: []recipe.IngredientLine{line(salt, "2 salt")}, Steps: []recipe.Step{{ID: "a", Text: "Mix."}}}}}
	id, err := rec.Create(rctx, live)
	if err != nil {
		t.Fatal(err)
	}
	proposed := live
	proposed.Name = "Sumac dish"
	proposed.Components = []recipe.Component{{Lines: []recipe.IngredientLine{line(sumac, "2 sumac")}, Steps: []recipe.Step{{ID: "a", Text: "Mix."}}}}
	if _, err := rec.ProposeEdit(rctx, id, 1, proposed, store.Provenance{Person: "agent@example.test"}); err != nil {
		t.Fatal(err)
	}

	for _, text := range []string{"sumac", "Sumac dish"} {
		got, err := rec.Search(rctx, store.SearchQuery{Text: text})
		if err != nil || len(got) != 0 {
			t.Errorf("Search(%q) = %v, %v; want nothing: the proposal is not in search", text, got, err)
		}
	}
	if got, err := rec.Search(rctx, store.SearchQuery{Text: "salt"}); err != nil || len(got) != 1 {
		t.Errorf("Search(salt) = %v, %v; want the live recipe", got, err)
	}
	cards, err := rec.Cards(rctx, store.SearchQuery{})
	if err != nil || len(cards) != 1 || cards[0].Name != "Plain dish" {
		t.Errorf("library cards = %+v, %v; want the live recipe only", cards, err)
	}

	shop := shopping.New(d, rec, cat)
	list, err := shop.CreateList(rctx, "Week")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := shop.AddEntry(rctx, list.ID, id, quantity.Int(1)); err != nil {
		t.Fatal(err)
	}
	view, err := shop.Load(rctx, list.ID)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range view.Aisles {
		for _, it := range a.Items {
			names = append(names, it.Name)
		}
	}
	if len(names) != 1 || names[0] != "zz visibility salt" {
		t.Errorf("shopping list items = %v, want only the live recipe's salt", names)
	}
}
