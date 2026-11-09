package mcp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	apiingest "github.com/kaecyra/gobbler/internal/api/ingest"
)

// jsonKeys maps each JSON key of struct type t to the type under it, the
// element type for a slice.
func jsonKeys(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Slice {
			ft = ft.Elem()
		}
		out[name] = ft
	}
	return out
}

// sameKeys checks that in accepts every key out writes, at every level, so a
// draft get_recipe_draft gave can be sent back to revise_recipe_draft as it
// is: its input schema refuses keys it does not know.
func sameKeys(t *testing.T, path string, out, in reflect.Type) {
	t.Helper()
	ok, ik := jsonKeys(out), jsonKeys(in)
	for k, ot := range ok {
		it, found := ik[k]
		if !found {
			t.Errorf("%s.%s is in the draft get_recipe_draft gives but revise_recipe_draft refuses it", path, k)
			continue
		}
		if ot.Kind() == reflect.Struct && it.Kind() == reflect.Struct && ot != it {
			sameKeys(t, path+"."+k, ot, it)
		}
	}
	for k := range ik {
		if _, found := ok[k]; !found {
			t.Errorf("%s.%s is read by revise_recipe_draft but get_recipe_draft never gives it", path, k)
		}
	}
}

func TestRevisedDraftAcceptsEveryPreviewKey(t *testing.T) {
	sameKeys(t, "draft", reflect.TypeFor[previewOut](), reflect.TypeFor[revisedDraft]())
}

// ADR-00048: the preview gives every field with its confidence, a zero one
// included, and every step its number.
func TestPreviewCarriesZeroConfidences(t *testing.T) {
	d := apiingest.Draft{
		Name: apiingest.Field[string]{Value: "Pie", Confidence: 1},
		Components: []apiingest.Component{{
			Lines: []apiingest.Line{{Raw: "a pinch of something", Confidence: 0}},
			Steps: []apiingest.Step{{Text: "Mix.", Confidence: 0}},
		}},
		AuthorNotes: []apiingest.AuthorNote{{Text: "Note.", Confidence: 0}},
	}
	raw, err := json.Marshal(previewOf(d))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Author     map[string]any `json:"author"`
		Components []struct {
			Lines []map[string]any `json:"lines"`
			Steps []map[string]any `json:"steps"`
		} `json:"components"`
		AuthorNotes []map[string]any `json:"author_notes"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	line, step := got.Components[0].Lines[0], got.Components[0].Steps[0]
	for what, m := range map[string]map[string]any{"author": got.Author, "line": line, "step": step, "note": got.AuthorNotes[0]} {
		if c, ok := m["confidence"]; !ok || c != 0.0 {
			t.Errorf("%s confidence = %v (present %v), want 0 and present: %s", what, c, ok, raw)
		}
	}
	if n, ok := step["number"]; !ok || n != 1.0 {
		t.Errorf("step number = %v (present %v), want 1", n, ok)
	}
}
