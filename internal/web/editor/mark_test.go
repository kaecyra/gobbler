package editor

import (
	"context"
	"strings"
	"testing"
)

// A low metadata field says "Check:" in words under its input, as a line, a
// step, a note and equipment do, so its grain tint is not the only sign
// (ADR-00006). A field without the mark says nothing.
func TestLowMetadataFieldSaysCheck(t *testing.T) {
	var b strings.Builder
	d := draft{Servings: "4", Author: "Ada", Marks: map[string]mark{"servings": {Low: true}, "author": {Low: true, LLM: true}}}
	if err := editorForm(view{D: d}).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := b.String()
	field := func(id string) string {
		i := strings.Index(body, `<label for="`+id+`">`)
		if i < 0 {
			t.Fatalf("no field %s", id)
		}
		return body[i : i+strings.Index(body[i:], "</div>")]
	}
	for _, id := range []string{"f-servings", "f-author"} {
		if !strings.Contains(field(id), `<strong class="flag">Check:</strong>`) {
			t.Errorf("low field %s does not say Check: %s", id, field(id))
		}
	}
	if strings.Contains(field("f-calories"), "Check:") {
		t.Error("an unmarked field says Check:")
	}
}
