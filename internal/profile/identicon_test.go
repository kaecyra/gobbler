package profile

import (
	"crypto/sha256"
	"strings"
	"testing"
)

func TestIdenticonIsStableAndIgnoresHowTheEmailWasTyped(t *testing.T) {
	a := NewIdenticon("ann@example.com")
	if b := NewIdenticon("  Ann@Example.com "); a != b {
		t.Errorf("the same email drew two pictures:\n%+v\n%+v", a, b)
	}
}

func TestIdenticonIsMirroredAndWithinThePalette(t *testing.T) {
	seen := map[string]bool{}
	for _, email := range []string{"ann@example.com", "bob@example.com", "cy@example.com", "dee@example.org", "dev@localhost", "x@y.z"} {
		ic := NewIdenticon(email)
		for row, cells := range ic.Cells {
			for col := range 2 {
				if cells[col] != cells[4-col] {
					t.Errorf("%s row %d is not mirrored: %v", email, row, cells)
				}
			}
		}
		if ic.Fg < 0 || ic.Fg >= IdenticonFgs || ic.Bg < 0 || ic.Bg >= IdenticonBgs {
			t.Errorf("%s palette index out of range: fg %d bg %d", email, ic.Fg, ic.Bg)
		}
		if ic.Path() == "" {
			t.Errorf("%s drew an empty picture", email)
		}
		seen[ic.Path()] = true
	}
	if len(seen) < 4 {
		t.Errorf("six emails drew only %d distinct pictures", len(seen))
	}
}

func TestIdenticonPathDrawsFilledCellsOnly(t *testing.T) {
	var ic Identicon
	ic.Cells[1][2] = true
	ic.Cells[4][0] = true
	if got, want := ic.Path(), "M2 1h1v1h-1zM0 4h1v1h-1z"; got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
	if strings.Contains(ic.Path(), "http") {
		t.Error("the picture refers to something outside")
	}
}

// A hash whose fifteen bits are all clear would draw nothing; the picture
// always has something in it.
func TestIdenticonNeverBlank(t *testing.T) {
	ic := identiconOf([sha256.Size]byte{})
	if ic.Path() == "" {
		t.Fatal("an all-zero hash drew a blank picture")
	}
}
