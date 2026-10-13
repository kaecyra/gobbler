package catalog

import (
	"context"
	"database/sql"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
)

// newTestDB opens a real SQLite file in a temp directory and applies every
// embedded migration, as the binary does at start-up.
func newTestDB(t *testing.T) *sql.DB {
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
	return d
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return New(newTestDB(t))
}

func count(t *testing.T, d *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// mustIngredient creates an ingredient or fails the test.
func mustIngredient(t *testing.T, s *Store, in Ingredient) Ingredient {
	t.Helper()
	got, err := s.CreateIngredient(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateIngredient(%q): %v", in.Name, err)
	}
	return got
}

// householdCtx creates a household called name and returns a context that
// carries it, as a member's request does. The catalog acts in its scope.
func householdCtx(t *testing.T, d *sql.DB, name string) context.Context {
	t.Helper()
	h, err := household.New(d).Create(context.Background(), name, name+"@example.com")
	if err != nil {
		t.Fatalf("create household %q: %v", name, err)
	}
	return household.WithContext(context.Background(), h)
}

// mustIngredientIn creates an ingredient in the scope of ctx or fails the test.
func mustIngredientIn(ctx context.Context, t *testing.T, s *Store, in Ingredient) Ingredient {
	t.Helper()
	got, err := s.CreateIngredient(ctx, in)
	if err != nil {
		t.Fatalf("CreateIngredient(%q): %v", in.Name, err)
	}
	return got
}

func f64(v float64) *float64 { return &v }
func i64(v int64) *int64     { return &v }
