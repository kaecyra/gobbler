package shopping

import (
	"testing"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/quantity"
)

// The counter reports the rows RegisterMergeHooks re-points, so the merge
// confirmation matches what the merge does.
func TestUsageCountsTheRowsAMergeRepoints(t *testing.T) {
	f := newFixture(t)
	usage := catalog.NewUsage(f.catalog)
	f.s.RegisterUsageCounters(usage)

	count := func(id int64) int64 {
		t.Helper()
		n, err := usage.IngredientUsage(f.ctx, id)
		if err != nil {
			t.Fatalf("IngredientUsage: %v", err)
		}
		return n
	}
	if n := count(f.egg); n != 0 {
		t.Fatalf("unused ingredient counted %d", n)
	}

	l := f.newList("L")
	if _, err := f.s.AddManualItem(f.ctx, l.ID, ManualItem{Ingredient: recipeID(f.egg), Amount: quantity.Amount{Qty: quantity.Exact(quantity.Int(2))}}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.AddToPantry(f.ctx, recipeID(f.egg)); err != nil {
		t.Fatal(err)
	}
	f.check(l, ingKey(f.egg), true)
	if n := count(f.egg); n != 3 {
		t.Errorf("usage = %d, want a manual item, a pantry row and a check", n)
	}
	if n := count(f.flour); n != 0 {
		t.Errorf("another ingredient's usage = %d, want 0", n)
	}
}
