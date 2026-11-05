package recipes

import (
	"testing"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

func TestCardRatingLabelIsTheLayoutVerdict(t *testing.T) {
	want := []string{"", "Not again", "It was fine", "Good", "Make it again", "House favourite"}
	for n, label := range want {
		c := card(1, "Soup", recipe.MealMain)
		c.Rating = &n
		v := cardFor(c, today)
		if v.RatingLabel != label {
			t.Errorf("rating %d: label %q, want %q", n, v.RatingLabel, label)
		}
		if n > 0 && v.RatingLabel != layout.Verdict(n) {
			t.Errorf("rating %d: label %q differs from layout.Verdict %q", n, v.RatingLabel, layout.Verdict(n))
		}
	}
	for _, n := range []int{-1, 6} {
		c := card(1, "Soup", recipe.MealMain)
		c.Rating = &n
		if v := cardFor(c, today); v.Rating != 0 || v.RatingLabel != "" {
			t.Errorf("out-of-range rating %d produced %d %q", n, v.Rating, v.RatingLabel)
		}
	}
}
