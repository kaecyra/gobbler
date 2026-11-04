package layout

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// Every icon in the set draws a line picture that screen readers skip.
func TestEveryLineIconDraws(t *testing.T) {
	shape := regexp.MustCompile(`<(path|circle|rect)\b`)
	for name := range lineGlyphs {
		var b strings.Builder
		if err := Icon(name).Render(context.Background(), &b); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out := b.String()
		if !shape.MatchString(out) || !strings.Contains(out, `aria-hidden="true"`) || !strings.Contains(out, `stroke="currentColor"`) {
			t.Errorf("%s draws %s", name, out)
		}
	}
}

// A name outside the set is an error, never an empty picture.
func TestUnknownLineIconFailsTheRender(t *testing.T) {
	var b strings.Builder
	if err := Icon("nope").Render(context.Background(), &b); err == nil || b.Len() != 0 {
		t.Errorf("unknown icon: err %v, wrote %q", err, b.String())
	}
}
