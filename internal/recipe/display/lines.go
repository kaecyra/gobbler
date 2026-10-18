package display

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// Ingredients is what line building reads from the catalog.
type Ingredients interface {
	GetIngredient(ctx context.Context, id int64) (catalog.Ingredient, error)
}

// Equipment is what equipment lines read from the catalog.
type Equipment interface {
	GetEquipment(ctx context.Context, id int64) (catalog.Equipment, error)
}

// Line is one formatted ingredient line.
type Line struct {
	Text string
	// Exact is the unrounded amount, shown on hover, empty when nothing was
	// rounded.
	Exact string
	// Unscaled marks a line whose ingredient is unresolved: its original text
	// is shown and no scaling or conversion applies to it.
	Unscaled bool
}

// LineBuilder formats ingredient lines under one set of options, caching
// catalog lookups for the life of the builder.
type LineBuilder struct {
	cat   Ingredients
	opts  Options
	cache map[recipe.IngredientID]catalog.Ingredient
}

// NewLineBuilder returns a builder that resolves ingredients through cat.
func NewLineBuilder(cat Ingredients, o Options) *LineBuilder {
	return &LineBuilder{cat: cat, opts: o, cache: map[recipe.IngredientID]catalog.Ingredient{}}
}

// Build scales, converts and rounds one line.
func (b *LineBuilder) Build(ctx context.Context, l recipe.IngredientLine) (Line, error) {
	if l.Ingredient == 0 {
		return Line{Text: l.Raw, Unscaled: true}, nil
	}
	ing, ok := b.cache[l.Ingredient]
	if !ok {
		var err error
		if ing, err = b.cat.GetIngredient(ctx, int64(l.Ingredient)); err != nil {
			return Line{}, Opaque(err, fmt.Sprintf("ingredient %d", l.Ingredient))
		}
		b.cache[l.Ingredient] = ing
	}

	o := b.opts
	amount, err := quantity.Scale(l.Amount, l.Rule, o.Mult)
	if err != nil {
		return Line{}, fmt.Errorf("scale %q: %w", l.Raw, err)
	}
	density, err := density(ing)
	if err != nil {
		return Line{}, err
	}
	amount, err = quantity.Display(amount, o.Mode, density)
	if err != nil {
		return Line{}, fmt.Errorf("convert %q: %w", l.Raw, err)
	}
	var lv Line
	shown := amount
	if o.Scaled() || o.Mode != quantity.ModeAsWritten {
		rounded, err := quantity.Round(amount)
		if err != nil {
			return Line{}, fmt.Errorf("round %q: %w", l.Raw, err)
		}
		shown = rounded.Rounded
		if exact := rounded.Exact.String(); exact != shown.String() {
			lv.Exact = exact
		}
	}

	text := strings.Join(nonEmpty([]string{shown.String(), ing.Name}), " ")
	var quals []string
	for _, q := range l.Qualifiers {
		quals = append(quals, q.Text)
	}
	if len(quals) > 0 {
		text += ", " + strings.Join(quals, ", ")
	}
	if l.Rule == quantity.RuleToTaste {
		text += " to taste"
	}
	if l.Optional {
		text += " (optional)"
	}
	lv.Text = text
	return lv, nil
}

// EquipmentLine is an equipment link resolved with one catalog read: its
// rendered text, and the catalog name and sized flag that callers key chip
// icons and hints on.
type EquipmentLine struct {
	Text  string // "2 baking sheet"
	Name  string // the catalog name, without the quantity
	Sized bool
}

// ResolveEquipment renders an equipment link as "2 baking sheet" and returns
// the catalog fields it read, so a caller needs no second read.
func ResolveEquipment(ctx context.Context, cat Equipment, link recipe.EquipmentLink) (EquipmentLine, error) {
	eq, err := cat.GetEquipment(ctx, int64(link.Equipment))
	if err != nil {
		return EquipmentLine{}, Opaque(err, fmt.Sprintf("equipment %d", link.Equipment))
	}
	return EquipmentLine{
		Text:  strings.TrimSpace(link.Qty.String() + " " + eq.Name),
		Name:  eq.Name,
		Sized: eq.Sized,
	}, nil
}

// Opaque flattens a catalog "not found" met while resolving a reference of an
// existing recipe: it is a broken reference, not a missing page, so it must
// not map to 404 (the page would claim the recipe does not exist).
func Opaque(err error, what string) error {
	if errors.Is(err, catalog.ErrNotFound) {
		return fmt.Errorf("%s: dangling reference: %s", what, err.Error())
	}
	return fmt.Errorf("%s: %w", what, err)
}

// density returns the ingredient's grams per millilitre, or the zero Rat when
// the catalog has none (quantity then leaves volume as volume).
func density(ing catalog.Ingredient) (quantity.Rat, error) {
	if ing.DensityGPerML == nil {
		return quantity.Rat{}, nil
	}
	d, err := quantity.ParseRat(strconv.FormatFloat(*ing.DensityGPerML, 'f', -1, 64))
	if err != nil {
		return quantity.Rat{}, fmt.Errorf("density of %q: %w", ing.Name, err)
	}
	return d, nil
}

func nonEmpty(in []string) []string {
	out := in[:0:0]
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
