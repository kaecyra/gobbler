package catalog

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// refTable stands in for a table owned by another store (recipe ingredient
// lines, pantry items): a real table with a real foreign key to ingredients.
func refTable(t *testing.T, d *sql.DB, name string) {
	t.Helper()
	if _, err := d.Exec(`CREATE TABLE ` + name + ` (id INTEGER PRIMARY KEY, ingredient_id INTEGER NOT NULL REFERENCES ingredients (id))`); err != nil {
		t.Fatal(err)
	}
}

func repoint(table string) MergeHook {
	return func(ctx context.Context, tx *sql.Tx, loser, winner int64) error {
		_, err := tx.ExecContext(ctx, `UPDATE `+table+` SET ingredient_id = ? WHERE ingredient_id = ?`, winner, loser)
		return err
	}
}

func mergeFixture(t *testing.T) (*Store, *sql.DB, Ingredient, Ingredient) {
	t.Helper()
	d := newTestDB(t)
	s := New(d)
	ctx := context.Background()
	winner, err := s.CreateIngredient(ctx, Ingredient{Name: "zz test butter", Aisle: "dairy", Aliases: []string{"zz test sweet butter"}, Reviewed: true})
	if err != nil {
		t.Fatal(err)
	}
	loser, err := s.CreateIngredient(ctx, Ingredient{Name: "zz test butta", Aisle: "dairy", Aliases: []string{"zz test buttah"}, DensityGPerML: f64(0.96), USDAID: i64(1001)})
	if err != nil {
		t.Fatal(err)
	}
	refTable(t, d, "zz_lines")
	refTable(t, d, "zz_pantry")
	for _, tbl := range []string{"zz_lines", "zz_pantry"} {
		for range 2 {
			if _, err := d.Exec(`INSERT INTO `+tbl+` (ingredient_id) VALUES (?)`, loser.ID); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := d.Exec(`INSERT INTO `+tbl+` (ingredient_id) VALUES (?)`, winner.ID); err != nil {
			t.Fatal(err)
		}
	}
	s.OnIngredientMerge(repoint("zz_lines"))
	s.OnIngredientMerge(repoint("zz_pantry"))
	return s, d, winner, loser
}

func TestMergeIngredientsRepointsEverything(t *testing.T) {
	s, d, winner, loser := mergeFixture(t)
	ctx := context.Background()

	merged, err := s.MergeIngredients(ctx, winner.ID, loser.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, tbl := range []string{"zz_lines", "zz_pantry"} {
		if n := count(t, d, `SELECT count(*) FROM `+tbl+` WHERE ingredient_id = ?`, loser.ID); n != 0 {
			t.Errorf("%s still has %d rows on the loser", tbl, n)
		}
		if n := count(t, d, `SELECT count(*) FROM `+tbl+` WHERE ingredient_id = ?`, winner.ID); n != 3 {
			t.Errorf("%s has %d rows on the winner, want all 3", tbl, n)
		}
	}
	if _, err := s.GetIngredient(ctx, loser.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("loser still exists: %v", err)
	}
	for _, term := range []string{"zz test butta", "zz test buttah", "zz test sweet butter", "zz test butter"} {
		if m, ok, err := s.MatchIngredient(ctx, term); err != nil || !ok || m.ID != winner.ID {
			t.Errorf("%q resolves to %+v ok=%v err=%v, want winner", term, m, ok, err)
		}
	}
	if merged.DensityGPerML == nil || *merged.DensityGPerML != 0.96 || merged.USDAID == nil || *merged.USDAID != 1001 {
		t.Errorf("winner did not take the loser's density and USDA id: %+v", merged)
	}
	if !merged.Reviewed {
		t.Error("winner lost its reviewed flag")
	}
}

func TestMergeKeepsWinnerValues(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	w := mustIngredient(t, s, Ingredient{Name: "zz test w", Aisle: "a", DensityGPerML: f64(1.5), USDAID: i64(5)})
	l := mustIngredient(t, s, Ingredient{Name: "zz test l", Aisle: "b", DensityGPerML: f64(2.5), USDAID: i64(6)})
	merged, err := s.MergeIngredients(ctx, w.ID, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *merged.DensityGPerML != 1.5 || *merged.USDAID != 5 || merged.Aisle != "a" {
		t.Errorf("winner values overwritten: %+v", merged)
	}
}

func TestMergeFailureMidwayChangesNothing(t *testing.T) {
	s, d, winner, loser := mergeFixture(t)
	ctx := context.Background()
	boom := errors.New("boom")
	// Registered after the working hooks, so it fails once they and the alias
	// moves have been applied inside the transaction. The delete and the
	// carry-over update come later: TestMergeFailureAfterDeleteChangesNothing.
	s.OnIngredientMerge(func(context.Context, *sql.Tx, int64, int64) error { return boom })

	if _, err := s.MergeIngredients(ctx, winner.ID, loser.ID); !errors.Is(err, boom) {
		t.Fatalf("MergeIngredients = %v, want the hook error", err)
	}

	for _, tbl := range []string{"zz_lines", "zz_pantry"} {
		if n := count(t, d, `SELECT count(*) FROM `+tbl+` WHERE ingredient_id = ?`, loser.ID); n != 2 {
			t.Errorf("%s has %d rows on the loser after rollback, want 2", tbl, n)
		}
	}
	got, err := s.GetIngredient(ctx, loser.ID)
	if err != nil {
		t.Fatalf("loser missing after rollback: %v", err)
	}
	if len(got.Aliases) != 1 || got.DensityGPerML == nil {
		t.Errorf("loser changed: %+v", got)
	}
	w, err := s.GetIngredient(ctx, winner.ID)
	if err != nil {
		t.Fatalf("winner missing after rollback: %v", err)
	}
	if len(w.Aliases) != 1 || w.DensityGPerML != nil || w.USDAID != nil {
		t.Errorf("winner changed by a failed merge: %+v", w)
	}
}

func TestMergeFailureAfterDeleteChangesNothing(t *testing.T) {
	s, d, winner, loser := mergeFixture(t)
	ctx := context.Background()
	// The carry-over UPDATE of the winner runs after the loser row has been
	// deleted; a trigger that aborts every ingredient update makes it fail.
	if _, err := d.Exec(`CREATE TRIGGER zz_block BEFORE UPDATE ON ingredients BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}

	if _, err := s.MergeIngredients(ctx, winner.ID, loser.ID); err == nil {
		t.Fatal("merge succeeded although the winner update was blocked")
	}

	got, err := s.GetIngredient(ctx, loser.ID)
	if err != nil {
		t.Fatalf("loser deleted although the merge failed: %v", err)
	}
	if len(got.Aliases) != 1 {
		t.Errorf("loser aliases = %v, want its one original alias", got.Aliases)
	}
	for _, tbl := range []string{"zz_lines", "zz_pantry"} {
		if n := count(t, d, `SELECT count(*) FROM `+tbl+` WHERE ingredient_id = ?`, loser.ID); n != 2 {
			t.Errorf("%s has %d rows on the loser after rollback, want 2", tbl, n)
		}
	}
	if m, ok, err := s.MatchIngredient(ctx, "zz test butta"); err != nil || !ok || m.ID != loser.ID {
		t.Errorf("loser name no longer resolves to the loser: %+v ok=%v err=%v", m, ok, err)
	}
}

func TestMergeErrors(t *testing.T) {
	s, _, winner, _ := mergeFixture(t)
	ctx := context.Background()
	if _, err := s.MergeIngredients(ctx, winner.ID, winner.ID); !errors.Is(err, ErrInvalid) {
		t.Errorf("merge into itself: %v, want ErrInvalid", err)
	}
	if _, err := s.MergeIngredients(ctx, winner.ID, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing loser: %v, want ErrNotFound", err)
	}
	if _, err := s.MergeIngredients(ctx, 999999, winner.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing winner: %v, want ErrNotFound", err)
	}
}

func TestMergeWithoutHookIsBlockedByReferences(t *testing.T) {
	// A referencing table with no registered hook must stop the merge rather
	// than orphan its rows: the foreign key refuses the delete.
	d := newTestDB(t)
	s := New(d)
	ctx := context.Background()
	w := mustIngredient(t, s, Ingredient{Name: "zz test w", Aisle: "a"})
	l := mustIngredient(t, s, Ingredient{Name: "zz test l", Aisle: "a"})
	refTable(t, d, "zz_unhooked")
	if _, err := d.Exec(`INSERT INTO zz_unhooked (ingredient_id) VALUES (?)`, l.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MergeIngredients(ctx, w.ID, l.ID); err == nil {
		t.Fatal("merge succeeded with an unhooked referencing row")
	}
	if _, err := s.GetIngredient(ctx, l.ID); err != nil {
		t.Errorf("loser lost despite failed merge: %v", err)
	}
}
