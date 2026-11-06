package store_test

import (
	"context"
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
	"github.com/kaecyra/gobbler/internal/shopping"
	"github.com/kaecyra/gobbler/internal/store"
)

// Migration 00005 makes the link from a shopping-list entry to its recipe block
// the recipe's deletion on purpose. The store names that refusal, so a caller
// can say why instead of reporting a raw foreign-key failure. The test lives
// outside package store because shopping imports it.
func TestDeleteOfARecipeOnAShoppingListIsRefusedAndNamed(t *testing.T) {
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
	salt, err := cat.CreateIngredient(rctx, catalog.Ingredient{Name: "zz delete salt", Aisle: "test"})
	if err != nil {
		t.Fatal(err)
	}
	rec := store.New(d)
	id, err := rec.Create(rctx, recipe.Recipe{Name: "Listed dish", Components: []recipe.Component{{
		Lines: []recipe.IngredientLine{{Ingredient: recipe.IngredientID(salt.ID), Rule: quantity.RuleLinear, Raw: "2 salt", Amount: quantity.Amount{Qty: quantity.Exact(quantity.Int(2))}}},
		Steps: []recipe.Step{{ID: "a", Text: "Mix."}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	lists := shopping.New(d, rec, cat)
	list, err := lists.CreateList(rctx, "Week")
	if err != nil {
		t.Fatal(err)
	}
	entry, err := lists.AddEntry(rctx, list.ID, id, quantity.Int(1))
	if err != nil {
		t.Fatal(err)
	}

	if err := rec.DeleteWithPicture(rctx, id, nil); !errors.Is(err, store.ErrOnShoppingList) {
		t.Fatalf("delete of a listed recipe = %v, want ErrOnShoppingList", err)
	}
	if _, err := rec.Get(rctx, id); err != nil {
		t.Fatalf("the recipe did not survive the refused delete: %v", err)
	}

	if err := lists.RemoveEntry(rctx, entry); err != nil {
		t.Fatal(err)
	}
	if err := rec.DeleteWithPicture(rctx, id, nil); err != nil {
		t.Fatalf("delete after taking the recipe off the list: %v", err)
	}
}
