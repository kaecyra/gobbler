package household_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// deletionDB returns a migrated database holding household Beta, with a recipe
// and everything that hangs off it, and, when withAlpha, household Alpha with
// the same plus a share to Beta and invitations.
func deletionDB(t *testing.T, withAlpha bool) (*sql.DB, household.Household) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	hs := household.New(d)
	beta, err := hs.Create(ctx, "Beta", "bob@example.com")
	if err != nil {
		t.Fatal(err)
	}
	fill(t, d, beta, "Beta stew")
	if !withAlpha {
		return d, household.Household{}
	}
	alpha, err := hs.Create(ctx, "Alpha", "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	fill(t, d, alpha, "Alpha stew")
	if _, err := hs.Invite(ctx, household.Invite{Kind: household.KindJoin, Email: "cy@example.com", HouseholdID: alpha.ID, IssuedBy: "ann@example.com"}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"sender_household_id, recipient_household_id", "recipient_household_id, sender_household_id"} {
		if _, err := d.ExecContext(ctx, fmt.Sprintf(
			`INSERT INTO recipe_shares (%s, sent_by, recipe_document, created_at) VALUES (?, ?, 'x', 'x', 'x')`, q),
			alpha.ID, beta.ID); err != nil {
			t.Fatal(err)
		}
	}
	return d, alpha
}

func fill(t *testing.T, d *sql.DB, h household.Household, name string) {
	t.Helper()
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}})
	if err != nil {
		t.Fatal(err)
	}
	var ctx context.Context
	a.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { ctx = r.Context() })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	ctx = household.WithContext(ctx, h)

	s := store.New(d)
	r := recipe.Recipe{
		Name: name,
		Tags: []string{"private"},
		Components: []recipe.Component{{
			Lines: []recipe.IngredientLine{{Rule: quantity.RuleLinear, Raw: "water"}},
			Steps: []recipe.Step{{ID: "s1", Text: "Do it."}},
		}},
	}
	id, err := s.CreateWithSnapshots(ctx, r, store.SnapshotInput{Kind: store.SnapshotText, Content: []byte("source")})
	if err != nil {
		t.Fatal(err)
	}
	r.Description = "edited"
	if err := s.Update(ctx, id, r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNote(ctx, id, "a note"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMadeIt(ctx, id, store.MadeIt{Comment: "great"}); err != nil {
		t.Fatal(err)
	}
	shop(t, d, h, id)
	overlay(t, d, h)
}

// overlay gives the household catalog overlay rows: an ingredient of its own
// with an alias, its own alias and an aisle override on a central entry, so the
// deletion check does not pass trivially for those tables.
func overlay(t *testing.T, d *sql.DB, h household.Household) {
	t.Helper()
	var central int64
	if err := d.QueryRow(`SELECT id FROM ingredients WHERE household_id IS NULL ORDER BY id LIMIT 1`).Scan(&central); err != nil {
		t.Fatalf("the catalog has no central ingredient: %v", err)
	}
	var own int64
	if err := d.QueryRow(`INSERT INTO ingredients (household_id, name, aisle, suggested_at) VALUES (?, 'zz house ghee', 'pantry', 'x') RETURNING id`, h.ID).Scan(&own); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO ingredient_aliases (household_id, alias, ingredient_id) VALUES (?1, 'zz house clarified', ?2)`,
		`INSERT INTO ingredient_aliases (household_id, alias, ingredient_id) VALUES (?1, 'zz house bulb', ?3)`,
		`INSERT INTO ingredient_aisle_overrides (household_id, ingredient_id, aisle) VALUES (?1, ?3, 'zz veg')`,
	} {
		if _, err := d.Exec(q, h.ID, own, central); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// shop gives the household a shopping list holding an entry for its recipe, a
// manual item, a checked item and a pantry row, so the shopping cascades are
// exercised and not only the recipe ones.
func shop(t *testing.T, d *sql.DB, h household.Household, recipeID int64) {
	t.Helper()
	var ingredient int64
	if err := d.QueryRow(`SELECT id FROM ingredients ORDER BY id LIMIT 1`).Scan(&ingredient); err != nil {
		t.Fatalf("the catalog has no ingredient to put in a pantry: %v", err)
	}
	var list int64
	if err := d.QueryRow(`INSERT INTO shopping_lists (household_id, name, created_at) VALUES (?, 'Weekly', 'x') RETURNING id`, h.ID).Scan(&list); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO shopping_entries (household_id, list_id, recipe_id, multiplier) VALUES (?1, ?2, ?3, '1')`,
		`INSERT INTO shopping_manual_items (household_id, list_id, text) VALUES (?1, ?2, 'paper towels')`,
		`INSERT INTO shopping_checks (household_id, list_id, item_key, checked_at) VALUES (?1, ?2, 'k', 'x')`,
		`INSERT INTO shopping_pantry (household_id, ingredient_id) VALUES (?1, ?4)`,
	} {
		if _, err := d.Exec(q, h.ID, list, recipeID, ingredient); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func rowCounts(t *testing.T, d *sql.DB) map[string]int {
	t.Helper()
	rows, err := d.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		-- FTS5's shadow tables hold index segments, whose row count depends on
		-- merge history; recipe_search itself is counted by its content.
		AND name NOT LIKE 'recipe_search\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, n := range names {
		var c int
		if err := d.QueryRow(fmt.Sprintf(`SELECT count(*) FROM "%s"`, n)).Scan(&c); err != nil {
			t.Fatalf("count %s: %v", n, err)
		}
		out[n] = c
	}
	return out
}

// Deleting a household leaves every table exactly as it would be had the
// household never existed (ADR-00018): no recipe, note, revision, snapshot,
// search entry, share or invitation of it remains, and the other household is
// untouched. The tables are enumerated from the schema, so one added later is
// covered without editing this test.
func TestDeleteLeavesNothingOfTheHousehold(t *testing.T) {
	ctx := context.Background()
	without, _ := deletionDB(t, false)
	want := rowCounts(t, without)

	with, alpha := deletionDB(t, true)
	have := rowCounts(t, with)
	for _, table := range []string{"recipes", "shopping_lists", "shopping_entries", "shopping_manual_items", "shopping_checks", "shopping_pantry",
		"ingredients", "ingredient_aliases", "ingredient_aisle_overrides"} {
		if have[table] <= want[table] {
			t.Fatalf("the fixture did not add a row to %s for Alpha: %d rows, %d without Alpha", table, have[table], want[table])
		}
	}
	if err := household.New(with).Delete(ctx, alpha.ID, "ann@example.com", "Alpha"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got := rowCounts(t, with)
	for table, n := range want {
		if got[table] != n {
			t.Errorf("table %s has %d rows after the delete, want %d", table, got[table], n)
		}
	}
}
