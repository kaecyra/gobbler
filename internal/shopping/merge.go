package shopping

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// Sentinel errors returned (wrapped) by Generate.
var (
	// ErrInvalidMultiplier is returned for a recipe entry whose multiplier is
	// zero or negative.
	ErrInvalidMultiplier = errors.New("recipe multiplier must be positive")
	// ErrUnknownIngredient is returned for a resolved ingredient that the
	// catalog input does not contain.
	ErrUnknownIngredient = errors.New("ingredient not in catalog")
	// ErrInvalidManualItem is returned for a manual item with neither text nor
	// an ingredient.
	ErrInvalidManualItem = errors.New("manual item needs text or an ingredient")
)

// Uncategorised is the aisle of ingredients with no aisle and of free-text
// items. It sorts after every named aisle.
const Uncategorised = "uncategorised"

// IngredientInfo is what generation needs to know about a canonical
// ingredient. DensityGPerML is grams per millilitre, nil when unknown.
type IngredientInfo struct {
	Name          string
	Aisle         string
	DensityGPerML *float64
}

// RecipeEntry is one recipe on the list. ID identifies the entry's recipe to
// the caller and is echoed in Source.
type RecipeEntry struct {
	ID         int64
	Recipe     recipe.Recipe
	Multiplier quantity.Rat
}

// ManualItem is an item added by hand: free text, or a canonical ingredient
// with an optional amount. Manual amounts are not scaled.
type ManualItem struct {
	Text       string
	Ingredient recipe.IngredientID
	Amount     quantity.Amount
}

// Input is everything Generate needs.
type Input struct {
	Entries []RecipeEntry
	Manual  []ManualItem
	// Catalog describes every resolved ingredient the entries and manual items
	// mention.
	Catalog map[recipe.IngredientID]IngredientInfo
	// Pantry holds ingredients always on hand. Recipe lines for them are set
	// aside in List.Pantry, unless the ingredient is in AddBack or a manual
	// item names it.
	Pantry map[recipe.IngredientID]bool
	// AddBack holds pantry ingredients the owner wants on the list anyway.
	AddBack map[recipe.IngredientID]bool
	// IncludeOptional keeps optional lines; they are dropped by default.
	IncludeOptional bool
}

// Source says where part of an item came from. A manual contribution has no
// source; Item.Manual is set instead.
type Source struct {
	RecipeID int64
	Name     string
}

// Item is one line of the list: an ingredient or free text with its merged
// amounts. Every consumer reads it the same way:
//
//   - Resolved ingredient (Ingredient != 0): Name is the catalog name and
//     Amounts the merged amounts; Amounts is empty when no source gave a
//     number.
//   - Manual free text (Ingredient == 0, Unresolved false): Name is the text
//     and Amounts its summed amounts, if any.
//   - Unresolved recipe line (Unresolved true): Name is the line as written,
//     amount included, and Scale is the factor the recipe was multiplied by.
//     Amounts is always empty, so the amount is counted once, in Name; show
//     Name with Scale and never total it.
type Item struct {
	// Ingredient is zero for a free-text item.
	Ingredient recipe.IngredientID
	Name       string
	Amounts    []quantity.Amount
	Sources    []Source
	// Manual reports whether a manual item contributed.
	Manual bool
	// Unresolved marks a recipe line with no resolved ingredient. Its text
	// cannot be parsed or rescaled.
	Unresolved bool
	// Scale applies to the text of an unresolved line scaled linearly; Text
	// renders it as "(x2)". It is zero for every other item.
	Scale quantity.Rat
}

// Aisle is a named group of items.
type Aisle struct {
	Name  string
	Items []Item
}

// List is the generated result. Pantry holds the dropped pantry items, merged
// the same way, so any of them can be added back.
type List struct {
	Aisles []Aisle
	Pantry []Item
}

type itemKey struct {
	id   recipe.IngredientID
	text string // lower-cased manual free text, only when id is zero
	// seq is nonzero for an unresolved recipe line. Its text cannot be parsed
	// into an ingredient and an amount, so two such lines are never merged:
	// merging would drop one's scale or count.
	seq int
}

type accum struct {
	item    Item
	aisle   string
	density quantity.Rat
	amounts []quantity.Amount
	seq     int // creation order, for deterministic output
}

// Generate builds the list described in ADR-00009. See the package comment.
func Generate(in Input) (List, error) {
	accs := map[itemKey]*accum{}
	seq := 0

	for _, e := range in.Entries {
		if e.Multiplier.Sign() <= 0 {
			return List{}, fmt.Errorf("recipe %q: %w: %s", e.Recipe.Name, ErrInvalidMultiplier, e.Multiplier)
		}
		src := Source{RecipeID: e.ID, Name: e.Recipe.Name}
		for _, c := range e.Recipe.Components {
			for _, l := range c.Lines {
				if l.Optional && !in.IncludeOptional {
					continue
				}
				a, err := quantity.Scale(l.Amount, l.Rule, e.Multiplier)
				if err != nil {
					return List{}, fmt.Errorf("recipe %q line %q: %w", e.Recipe.Name, l.Raw, err)
				}
				key := itemKey{id: l.Ingredient}
				if l.Ingredient == 0 {
					seq++
					key.seq = seq
				}
				acc, err := in.accumFor(accs, key, l.Raw)
				if err != nil {
					return List{}, fmt.Errorf("recipe %q line %q: %w", e.Recipe.Name, l.Raw, err)
				}
				acc.addSource(src)
				if l.Ingredient != 0 {
					acc.amounts = append(acc.amounts, a)
					continue
				}
				// The line's text already holds its amount: record the factor
				// rather than a second, countable amount.
				acc.item.Unresolved = true
				if l.Rule == quantity.RuleLinear {
					acc.item.Scale = e.Multiplier
				}
			}
		}
	}

	for i, m := range in.Manual {
		if m.Ingredient == 0 && strings.TrimSpace(m.Text) == "" {
			return List{}, fmt.Errorf("manual item %d: %w", i+1, ErrInvalidManualItem)
		}
		key := itemKey{id: m.Ingredient}
		if m.Ingredient == 0 {
			key.text = strings.ToLower(strings.TrimSpace(m.Text))
		}
		acc, err := in.accumFor(accs, key, m.Text)
		if err != nil {
			return List{}, fmt.Errorf("manual item %q: %w", m.Text, err)
		}
		acc.item.Manual = true
		acc.amounts = append(acc.amounts, m.Amount)
	}

	var list List
	byAisle := map[string][]Item{}
	ordered := make([]*accum, 0, len(accs))
	for _, acc := range accs {
		ordered = append(ordered, acc)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].seq < ordered[j].seq })
	for _, acc := range ordered {
		amounts, err := mergeAmounts(acc.amounts, acc.density)
		if err != nil {
			return List{}, fmt.Errorf("merge %q: %w", acc.item.Name, err)
		}
		acc.item.Amounts = amounts
		if acc.item.Ingredient != 0 && in.Pantry[acc.item.Ingredient] && !in.AddBack[acc.item.Ingredient] &&
			!acc.item.Manual {
			list.Pantry = append(list.Pantry, acc.item)
			continue
		}
		byAisle[acc.aisle] = append(byAisle[acc.aisle], acc.item)
	}

	sortItems(list.Pantry)
	names := make([]string, 0, len(byAisle))
	for n := range byAisle {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == Uncategorised) != (names[j] == Uncategorised) {
			return names[j] == Uncategorised
		}
		return names[i] < names[j]
	})
	for _, n := range names {
		sortItems(byAisle[n])
		list.Aisles = append(list.Aisles, Aisle{Name: n, Items: byAisle[n]})
	}
	return list, nil
}

// accumFor returns the accumulator for key k, creating it. text names a
// free-text item (zero ingredient id).
func (in Input) accumFor(accs map[itemKey]*accum, k itemKey, text string) (*accum, error) {
	text = strings.TrimSpace(text)
	id := k.id
	if acc, ok := accs[k]; ok {
		return acc, nil
	}
	acc := &accum{seq: len(accs)}
	if id == 0 {
		acc.item = Item{Name: text}
		acc.aisle = Uncategorised
	} else {
		info, ok := in.Catalog[id]
		if !ok {
			return nil, fmt.Errorf("%w: id %d", ErrUnknownIngredient, id)
		}
		acc.item = Item{Ingredient: id, Name: info.Name}
		acc.aisle = strings.TrimSpace(info.Aisle)
		if acc.aisle == "" {
			acc.aisle = Uncategorised
		}
		if info.DensityGPerML != nil {
			d, err := quantity.ParseRat(strconv.FormatFloat(*info.DensityGPerML, 'f', -1, 64))
			if err != nil {
				return nil, fmt.Errorf("ingredient %q density %v: %w", info.Name, *info.DensityGPerML, err)
			}
			acc.density = d
		}
	}
	accs[k] = acc
	return acc, nil
}

func (a *accum) addSource(s Source) {
	for _, have := range a.item.Sources {
		if have == s {
			return
		}
	}
	a.item.Sources = append(a.item.Sources, s)
}

func sortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Ingredient < b.Ingredient
	})
}
