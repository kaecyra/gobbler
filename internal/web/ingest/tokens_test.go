package ingest

import (
	"os"
	"regexp"
	"testing"
)

// Pages take colours and radii from the tokens (docs/ui/design.md); a literal
// is a second palette waiting to drift.
func TestTemplatesUseTokensNotLiteralColoursOrRadii(t *testing.T) {
	literal := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|\brgba?\(|\bhsla?\(|border-radius:\s*[0-9.]+(px|rem|em)|\bborder-left:\s*[1-9]`)
	for _, f := range []string{"ingest.templ", "photo.templ"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range literal.FindAllString(string(b), -1) {
			t.Errorf("%s has the literal %q; use a token", f, m)
		}
	}
}
