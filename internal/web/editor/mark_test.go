package editor

import (
	"context"
	"strings"
	"testing"
)

// A low metadata field's label leads with the alert triangle, read as
// "Check", so its grain tint is not the only sign (ADR-00006), and no hint
// line follows the field. A field without the mark has neither.
func TestLowMetadataFieldSaysCheck(t *testing.T) {
	var b strings.Builder
	d := draft{Servings: "4", Author: "Ada", Marks: map[string]mark{"servings": {Low: true}, "author": {Low: true, LLM: true}}}
	if err := editorForm(view{D: d}).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := b.String()
	field := func(id string) (label, rest string) {
		i := strings.Index(body, `<label for="`+id+`">`)
		if i < 0 {
			t.Fatalf("no field %s", id)
		}
		end := strings.Index(body[i:], "</div>")
		if end < 0 {
			t.Fatalf("field %s has no end", id)
		}
		f := body[i : i+end]
		label, rest, _ = strings.Cut(f, "</label>")
		return label, rest
	}
	for _, id := range []string{"f-servings", "f-author"} {
		label, rest := field(id)
		if !strings.Contains(label, `<span class="chk">`) || !strings.Contains(label, `<span class="sr">Check </span>`) || !strings.Contains(label, `<path d="M12 3.5`) {
			t.Errorf("low field %s does not lead its label with the alert read as Check: %s", id, label)
		}
		if strings.Contains(rest, `class="hint"`) || strings.Contains(rest, "Check") {
			t.Errorf("low field %s has a hint line under it: %s", id, rest)
		}
	}
	if label, _ := field("f-calories"); strings.Contains(label, "Check") || strings.Contains(label, `class="chk"`) {
		t.Errorf("an unmarked field is flagged: %s", label)
	}
}
