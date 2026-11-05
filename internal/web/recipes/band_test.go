package recipes

import "testing"

// The band of a recipe without a picture is a function of its id alone
// (ADR-00039, Showing): the same recipe always gets the same band, and
// recipes imported one after another differ.
func TestBandVariantIsTheIDModuloTheSet(t *testing.T) {
	for id := int64(0); id < 40; id++ {
		got := bandVariant(id)
		if want := int(id % 6); got != want {
			t.Errorf("bandVariant(%d) = %d, want %d", id, got, want)
		}
		if again := bandVariant(id); again != got {
			t.Errorf("bandVariant(%d) changed between calls: %d then %d", id, got, again)
		}
		if got < 0 || got >= bandVariants {
			t.Errorf("bandVariant(%d) = %d, outside 0..%d", id, got, bandVariants-1)
		}
	}
	if bandVariants != 6 {
		t.Errorf("bandVariants = %d, but app.css draws .ph0 to .ph5", bandVariants)
	}
}

func TestNeighbouringRecipesGetDifferentBands(t *testing.T) {
	for id := int64(1); id < 20; id++ {
		if bandVariant(id) == bandVariant(id+1) {
			t.Errorf("recipes %d and %d share band %d", id, id+1, bandVariant(id))
		}
	}
}
