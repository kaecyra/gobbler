package ingest

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/kaecyra/gobbler/internal/catalog"
	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// Form field names of the review form. Line and step fields carry the
// component and item index they edit, so a field can only address something
// the draft has.
const (
	fieldName        = "name"
	fieldAuthor      = "author"
	fieldDescription = "description"
)

func lineNameField(ci, li int) string     { return fmt.Sprintf("line-%d-%d-name", ci, li) }
func lineExistingField(ci, li int) string { return fmt.Sprintf("line-%d-%d-existing", ci, li) }
func stepField(ci, si int) string         { return fmt.Sprintf("step-%d-%d", ci, si) }

// ingredientMatcher resolves a typed name to a catalog ingredient, by name or
// alias. *catalog.Store satisfies it.
type ingredientMatcher interface {
	MatchIngredient(ctx context.Context, term string) (catalog.Ingredient, bool, error)
}

// applyEdits copies what the user changed on the review form into d. A field
// absent from the form is left alone. An edited value is taken as reviewed, so
// its confidence becomes 1 and it is no longer flagged. The returned problems
// are things the user typed that cannot be applied, in words for the page; an
// error means the catalog could not be read.
func applyEdits(ctx context.Context, form url.Values, d *ingestcore.Draft, m ingredientMatcher) (problems []string, err error) {
	editString(form, fieldName, &d.Name)
	editString(form, fieldAuthor, &d.Author)
	editString(form, fieldDescription, &d.Description)

	for ci := range d.Components {
		c := &d.Components[ci]
		for li := range c.Lines {
			l := &c.Lines[li]
			if l.Ingredient != 0 {
				continue
			}
			if v, ok := form[lineNameField(ci, li)]; ok {
				if name := strings.TrimSpace(v[0]); name != l.Name {
					l.Name, l.Confidence = name, 1
				}
			}
			existing := strings.TrimSpace(form.Get(lineExistingField(ci, li)))
			if existing == "" {
				continue
			}
			ing, ok, err := m.MatchIngredient(ctx, existing)
			if err != nil {
				return nil, fmt.Errorf("look up ingredient %q: %w", existing, err)
			}
			if !ok {
				problems = append(problems, fmt.Sprintf("Line %q: no existing ingredient is called %q. Clear the field to add it as new.", l.Raw, existing))
				continue
			}
			l.Ingredient, l.Name, l.Confidence = recipe.IngredientID(ing.ID), ing.Name, 1
		}
		for si := range c.Steps {
			if v, ok := form[stepField(ci, si)]; ok {
				if text := strings.TrimSpace(v[0]); text != c.Steps[si].Text {
					c.Steps[si].Text, c.Steps[si].Confidence = text, 1
				}
			}
		}
	}
	d.Proposed = ingestcore.ProposedFor(d.Components, d.Proposed)
	return problems, nil
}

func editString(form url.Values, key string, f *ingestcore.Field[string]) {
	v, ok := form[key]
	if !ok {
		return
	}
	if s := strings.TrimSpace(v[0]); s != f.Value {
		f.Value, f.Confidence = s, 1
	}
}
