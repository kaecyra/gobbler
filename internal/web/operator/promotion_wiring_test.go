package operator_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/web"
)

// Promoting through the real router merges another household's duplicate and
// re-points the rows that referenced it. The router wires the recipe store's and
// the shopping store's merge hooks onto the catalog store the promotion uses;
// without them the merge is refused by the foreign keys of the other
// household's pantry and recipe lines.
func TestPromotionRepointsTheDuplicatesRows(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{
		Server:   config.Server{ListenAddr: "127.0.0.1:0", DataDir: t.TempDir(), DevAuthBypass: true},
		Operator: config.Operator{Emails: []string{auth.DevEmail}},
	}
	d, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(cfg, auth.WithLogger(slog.New(slog.NewJSONHandler(io.Discard, nil))))
	if err != nil {
		t.Fatal(err)
	}
	router, err := web.New(web.Deps{Config: cfg, DB: d, Auth: a})
	if err != nil {
		t.Fatal(err)
	}

	hs := household.New(d)
	member := func(name, email string) (household.Household, context.Context) {
		t.Helper()
		if err := hs.Seed(ctx, name, email); err != nil {
			t.Fatal(err)
		}
		h, err := hs.ForEmail(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		return h, household.WithContext(ctx, h)
	}
	_, dev := member("Dev household", auth.DevEmail)
	otherHome, other := member("Other household", auth.DevOtherEmail)

	cat := catalog.New(d)
	ghee, err := cat.CreateIngredient(dev, catalog.Ingredient{Name: "zz ghee", Aisle: "dairy"})
	if err != nil {
		t.Fatal(err)
	}
	dup, err := cat.CreateIngredient(other, catalog.Ingredient{Name: "zz ghee", Aisle: "oils"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.SuggestIngredient(dev, ghee.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO shopping_pantry (household_id, ingredient_id) VALUES (?, ?)`, otherHome.ID, dup.ID); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/operator/promotions/"+strconv.FormatInt(ghee.ID, 10), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("promote: status %d, body %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM shopping_pantry WHERE household_id = ? AND ingredient_id = ?`, otherHome.ID, ghee.ID).Scan(&n); err != nil || n != 1 {
		t.Errorf("the other household's pantry rows on the promoted entry = %d (err %v), want its one", n, err)
	}
	if err := d.QueryRow(`SELECT count(*) FROM ingredients WHERE id = ?`, dup.ID).Scan(&n); err != nil || n != 0 {
		t.Errorf("the duplicate still exists: %d rows (err %v)", n, err)
	}
}
