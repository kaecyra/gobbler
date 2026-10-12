package shopping

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	domain "github.com/kaecyra/gobbler/internal/shopping"
)

// itemView is one list item as a page shows it.
type itemView struct {
	// DOMID is unique within the page: ItemKey is not (two identical unresolved
	// lines of one recipe share it), so the id is built from the item's position.
	DOMID   string
	Key     string
	Name    string
	Amounts string
	Sources string
	Checked bool
	// Next is the checked value a tap on the item sends: the opposite of its state.
	Next string
}

// aisleView is a named group of items.
type aisleView struct {
	Name  string
	Items []itemView
}

// droppedView is a pantry item set aside, with what the recipes asked for.
type droppedView struct {
	Ingredient int64
	Name       string
	Amounts    string
	Sources    string
}

// entryView is a recipe on the list.
type entryView struct {
	ID         int64
	RecipeID   int64
	Name       string
	Multiplier string
}

// manualView is a hand-added item.
type manualView struct {
	ID      int64
	Label   string
	Amounts string
}

type option struct {
	Value string
	Label string
}

// buildView is the build-a-list page.
type buildView struct {
	ID          int64
	Name        string
	Entries     []entryView
	Manual      []manualView
	Aisles      []aisleView
	Dropped     []droppedView
	Export      string
	Recipes     []option
	Ingredients []option
	Units       []string
	Problem     string
}

// liveView is the phone view.
type liveView struct {
	ID     int64
	Name   string
	Aisles []aisleView
}

// roundedAmounts renders amounts at kitchen precision, joined with " + ", the
// same rounding the text export applies.
func roundedAmounts(as []quantity.Amount) (string, error) {
	parts := make([]string, 0, len(as))
	for _, a := range as {
		r, err := quantity.Round(a)
		if err != nil {
			return "", fmt.Errorf("round amount %s: %w", a, err)
		}
		parts = append(parts, r.Rounded.String())
	}
	return strings.Join(parts, " + "), nil
}

// itemName is the item's display name. An unresolved recipe line already holds
// its amount, so the factor the recipe was multiplied by is shown instead.
func itemName(it domain.Item) string {
	if it.Unresolved && it.Scale.Sign() > 0 && !it.Scale.Equal(quantity.Int(1)) {
		return it.Name + " (x" + it.Scale.String() + ")"
	}
	return it.Name
}

func sourcesText(it domain.Item) string {
	names := make([]string, 0, len(it.Sources)+1)
	for _, s := range it.Sources {
		names = append(names, s.Name)
	}
	if it.Manual {
		names = append(names, "added by hand")
	}
	return strings.Join(names, ", ")
}

func newAisles(aisles []domain.ViewAisle) ([]aisleView, error) {
	out := make([]aisleView, 0, len(aisles))
	for ai, a := range aisles {
		av := aisleView{Name: a.Name, Items: make([]itemView, 0, len(a.Items))}
		for ii, it := range a.Items {
			amounts, err := roundedAmounts(it.Amounts)
			if err != nil {
				return nil, fmt.Errorf("item %q: %w", it.Name, err)
			}
			next := "1"
			if it.Checked {
				next = "0"
			}
			av.Items = append(av.Items, itemView{
				DOMID: fmt.Sprintf("item-%d-%d", ai, ii),
				Key:   string(it.Key), Name: itemName(it.Item), Amounts: amounts,
				Sources: sourcesText(it.Item), Checked: it.Checked, Next: next,
			})
		}
		out = append(out, av)
	}
	return out, nil
}

func newDropped(items []domain.Item) ([]droppedView, error) {
	out := make([]droppedView, 0, len(items))
	for _, it := range items {
		amounts, err := roundedAmounts(it.Amounts)
		if err != nil {
			return nil, fmt.Errorf("pantry item %q: %w", it.Name, err)
		}
		out = append(out, droppedView{
			Ingredient: int64(it.Ingredient), Name: it.Name, Amounts: amounts, Sources: sourcesText(it),
		})
	}
	return out, nil
}

// ingredientNames maps catalog ids to names.
func ingredientNames(all []catalog.Ingredient) map[recipe.IngredientID]string {
	m := make(map[recipe.IngredientID]string, len(all))
	for _, in := range all {
		m[recipe.IngredientID(in.ID)] = in.Name
	}
	return m
}

func newEntries(es []domain.EntryView) []entryView {
	out := make([]entryView, 0, len(es))
	for _, e := range es {
		out = append(out, entryView{ID: e.ID, RecipeID: e.RecipeID, Name: e.Name, Multiplier: e.Multiplier.String()})
	}
	return out
}

func newManual(ms []domain.ManualView, names map[recipe.IngredientID]string) ([]manualView, error) {
	out := make([]manualView, 0, len(ms))
	for _, m := range ms {
		label := m.Text
		if label == "" {
			label = names[m.Ingredient]
		}
		amounts, err := roundedAmounts([]quantity.Amount{m.Amount})
		if err != nil {
			return nil, fmt.Errorf("manual item %d: %w", m.ID, err)
		}
		out = append(out, manualView{ID: m.ID, Label: label, Amounts: amounts})
	}
	return out, nil
}

func ingredientOptions(all []catalog.Ingredient) []option {
	out := make([]option, 0, len(all))
	for _, in := range all {
		out = append(out, option{Value: strconv.FormatInt(in.ID, 10), Label: in.Name})
	}
	return out
}
