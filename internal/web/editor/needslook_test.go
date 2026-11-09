package editor

import (
	"bytes"
	"strings"
	"testing"

	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
)

// The count of items that need a look is the number of items the form flags:
// a line the rules read above the threshold but could not match is flagged by
// its issue, so it is counted too (#588).
func TestNeedsLookCountsEveryFlaggedItem(t *testing.T) {
	e := newEnv(t)
	base := graphBase()
	notMatched := func(name string) ingestcore.Line {
		return ingestcore.Line{Raw: "1 " + name, Name: name, Confidence: 0.8, Issues: []string{`ingredient "` + name + `" not matched`}}
	}
	base.Components[0].Lines = []ingestcore.Line{
		{Raw: "1 cup zest", Name: "zest", Confidence: 0.5},
		notMatched("chicken cutlets"), notMatched("dragonfruit"), notMatched("zorbs"),
		{Raw: "1 cup flour", Name: "flour", Confidence: 1},
	}
	form, err := NewUnsaved(e.cat).Open(e.ctx, base, ReviewOptions{Action: "/x"})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := form.Component().Render(e.ctx, &buf); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	if got := strings.Count(body, `<p class="hint">`); got != 4 {
		t.Errorf("the form flags %d items, want 4", got)
	}
	if n := form.NeedsLook(); n != 4 {
		t.Errorf("NeedsLook = %d, want 4: the low line and the three not matched", n)
	}
	if !strings.Contains(body, "4 items still need a look") {
		t.Error("the save bar does not count the four flagged items")
	}
}
