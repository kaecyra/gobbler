package layout

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func renderOperatorPage(t *testing.T, current OperatorSection, here string) string {
	t.Helper()
	ctx := templ.WithChildren(context.Background(), templ.Raw("<p>body</p>"))
	var b bytes.Buffer
	if err := OperatorPage("T", current, here).Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestOperatorPageBreadcrumb(t *testing.T) {
	tests := map[string]struct {
		current OperatorSection
		here    string
		want    []string
		notWant []string
	}{
		"overview has none": {OperatorOverview, "", nil, []string{`data-testid="operator-crumbs"`}},
		"a section page ends at the section": {
			OperatorEquipment, "",
			[]string{`aria-label="Breadcrumb"`, `<a href="/operator">Operator</a>`, `<span aria-current="page">Equipment</span>`},
			[]string{`<a href="/operator/equipment">Equipment</a>`},
		},
		"a page inside a section links the section": {
			OperatorIngredients, "Flour",
			[]string{`<a href="/operator">Operator</a>`, `<a href="/operator/ingredients">Central ingredients</a>`, `<span aria-current="page">Flour</span>`},
			nil,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := renderOperatorPage(t, tc.current, tc.here)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q", w)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(got, w) {
					t.Errorf("unexpected %q", w)
				}
			}
		})
	}
}
