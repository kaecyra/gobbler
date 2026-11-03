package profile

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Identicon is the default avatar (ADR-00032): a 5 by 5 grid, mirrored left to
// right, drawn from a hash of the email, so one email always gets the same
// picture and nothing is fetched from outside. It holds no colours, only the
// index of a pair from the stylesheet's palette, so the colours come from the
// design tokens.
type Identicon struct {
	// Cells[row][col] is whether the cell is filled.
	Cells [identiconSize][identiconSize]bool
	// Fg and Bg index the stylesheet's foreground and background classes
	// (av-fg-N, av-bg-N).
	Fg, Bg int
}

const (
	identiconSize = 5
	// IdenticonFgs is how many foreground classes (av-fg-N) the stylesheet
	// defines.
	IdenticonFgs = 5
	// IdenticonBgs is how many background classes (av-bg-N) it defines.
	IdenticonBgs = 3
	// identiconHalf is the columns drawn from the hash: the left three, and the
	// middle one, mirrored.
	identiconHalf = 3
)

// NewIdenticon draws the default avatar of an email. The email is trimmed and
// lower-cased first, the form every table stores, so the avatar does not
// depend on how it was typed.
func NewIdenticon(email string) Identicon {
	return identiconOf(sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email)))))
}

// identiconOf draws the picture a hash names.
func identiconOf(sum [sha256.Size]byte) Identicon {
	bits := uint16(sum[0])<<8 | uint16(sum[1])
	var ic Identicon
	for row := range identiconSize {
		for col := range identiconHalf {
			on := bits&(1<<(row*identiconHalf+col)) != 0
			ic.Cells[row][col] = on
			ic.Cells[row][identiconSize-1-col] = on
		}
	}
	if ic.empty() {
		// Fifteen clear bits would draw a blank square; one email in 32768.
		ic.Cells[2][2] = true
	}
	ic.Fg = int(sum[2]) % IdenticonFgs
	ic.Bg = int(sum[3]) % IdenticonBgs
	return ic
}

func (ic Identicon) empty() bool {
	for _, row := range ic.Cells {
		for _, on := range row {
			if on {
				return false
			}
		}
	}
	return true
}

// Path is the filled cells as one SVG path in a 5 by 5 viewBox.
func (ic Identicon) Path() string {
	var b strings.Builder
	for row, cells := range ic.Cells {
		for col, on := range cells {
			if on {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", col, row)
			}
		}
	}
	return b.String()
}
