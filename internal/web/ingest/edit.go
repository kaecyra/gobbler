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
	d.Proposed = proposedFor(*d)
	return problems, nil
}

// proposedAisle files a proposed ingredient until it is reviewed. It matches
// the aisle ingest.Saver creates it under.
const proposedAisle = "uncategorised"

// proposedFor lists what confirming d would add: the names of lines still
// matched to no catalog ingredient, once each, in reading order. The edits just
// applied can map a line or rename it, so the draft's list from ingest time is
// stale; a name already proposed keeps its aisle.
func proposedFor(d ingestcore.Draft) []ingestcore.ProposedIngredient {
	aisle := map[string]string{}
	for _, p := range d.Proposed {
		aisle[p.Name] = p.Aisle
	}
	var out []ingestcore.ProposedIngredient
	seen := map[string]bool{}
	for _, c := range d.Components {
		for _, l := range c.Lines {
			name := strings.Join(strings.Fields(strings.ToLower(l.Name)), " ")
			if l.Ingredient != 0 || name == "" || seen[name] {
				continue
			}
			seen[name] = true
			a, ok := aisle[name]
			if !ok {
				a = proposedAisle
			}
			out = append(out, ingestcore.ProposedIngredient{Name: name, Aisle: a})
		}
	}
	return out
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
