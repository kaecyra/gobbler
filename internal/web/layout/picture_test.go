package layout

import "testing"

// The band of a recipe without a picture is a function of its id alone
// (ADR-00039, Showing): the same recipe always gets the same band, and
// recipes imported one after another differ.
func TestBandVariantIsTheIDModuloTheSet(t *testing.T) {
	for id := int64(0); id < 40; id++ {
		got := BandVariant(id)
		if want := int(id % 6); got != want {
			t.Errorf("BandVariant(%d) = %d, want %d", id, got, want)
		}
		if again := BandVariant(id); again != got {
			t.Errorf("BandVariant(%d) changed between calls: %d then %d", id, got, again)
		}
		if got < 0 || got >= BandVariants {
			t.Errorf("BandVariant(%d) = %d, outside 0..%d", id, got, BandVariants-1)
		}
	}
	if BandVariants != 6 {
		t.Errorf("BandVariants = %d, but app.css draws .ph0 to .ph5", BandVariants)
	}
}

func TestNeighbouringRecipesGetDifferentBands(t *testing.T) {
	for id := int64(1); id < 20; id++ {
		if BandVariant(id) == BandVariant(id+1) {
			t.Errorf("recipes %d and %d share band %d", id, id+1, BandVariant(id))
		}
	}
}
