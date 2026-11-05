package journal

import "testing"

// madeRatingIs picks the radio of the made-it form: 0 is "No rating", checked
// when nothing was typed, and n is checked only for its own value.
func TestMadeRatingIs(t *testing.T) {
	for _, c := range []struct {
		typed string
		n     int
		want  bool
	}{
		{"", 0, true},
		{"", 3, false},
		{"3", 3, true},
		{"3", 0, false},
		{"3", 4, false},
		{"9", 0, false},
		{"9", 5, false},
	} {
		if got := (view{MadeRating: c.typed}).madeRatingIs(c.n); got != c.want {
			t.Errorf("MadeRating %q, madeRatingIs(%d) = %v, want %v", c.typed, c.n, got, c.want)
		}
	}
}

func TestMadeTimes(t *testing.T) {
	for n, want := range map[int]string{1: "1 time", 2: "2 times", 12: "12 times"} {
		if got := madeTimes(n); got != want {
			t.Errorf("madeTimes(%d) = %q, want %q", n, got, want)
		}
	}
}
