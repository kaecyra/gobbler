package shopping

import (
	"fmt"
	"strings"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// ItemKey identifies a generated item across regenerations, so check state
// can follow it. It is independent of the item's amounts and sources:
//
//   - "i:<ingredient id>" for a resolved ingredient, wherever it came from;
//   - "t:<text>" for a manual free-text item, the lower-cased trimmed text;
//   - "u:<recipe id>:<line>" for an unresolved recipe line, the lower-cased
//     line as written. Two identical lines of one recipe share a key.
//
// An item whose amount changes keeps its key and so its check; one that no
// longer exists loses its check at the next Regenerate.
type ItemKey string

// KeyOf returns the key of a generated item.
func KeyOf(it Item) ItemKey {
	name := strings.ToLower(strings.TrimSpace(it.Name))
	switch {
	case it.Ingredient != 0:
		return ItemKey(fmt.Sprintf("i:%d", it.Ingredient))
	case it.Unresolved:
		var recipeID int64
		if len(it.Sources) > 0 {
			recipeID = it.Sources[0].RecipeID
		}
		return ItemKey(fmt.Sprintf("u:%d:%s", recipeID, name))
	default:
		return ItemKey("t:" + name)
	}
}

// valid reports whether k has the shape KeyOf produces.
func (k ItemKey) valid() bool {
	kind, rest, ok := strings.Cut(string(k), ":")
	return ok && rest != "" && (kind == "i" || kind == "t" || kind == "u")
}

// ViewItem is a generated item with its key and check state. Key is not unique
// within a view: two identical unresolved lines of one recipe are separate
// items that share it, and check together. Use Key to set or clear a check,
// and the item's position (aisle index and item index in View.Aisles) for
// anything that must be unique, such as a DOM id.
type ViewItem struct {
	Item
	Key     ItemKey
	Checked bool
}

// ViewAisle is a named group of view items.
type ViewAisle struct {
	Name  string
	Items []ViewItem
}

// annotate attaches keys and check state to the generated aisles. checked holds
// the keys with a stored check; keys of items that do not exist are ignored.
func annotate(l List, checked map[ItemKey]bool) []ViewAisle {
	out := make([]ViewAisle, 0, len(l.Aisles))
	for _, a := range l.Aisles {
		va := ViewAisle{Name: a.Name, Items: make([]ViewItem, 0, len(a.Items))}
		for _, it := range a.Items {
			k := KeyOf(it)
			va.Items = append(va.Items, ViewItem{Item: it, Key: k, Checked: checked[k]})
		}
		out = append(out, va)
	}
	return out
}

// staleKeys returns the stored keys that name no item on the list: the check
// state Regenerate drops. Pantry items set aside are not on the list.
func staleKeys(l List, stored []ItemKey) []ItemKey {
	live := map[ItemKey]bool{}
	for _, a := range l.Aisles {
		for _, it := range a.Items {
			live[KeyOf(it)] = true
		}
	}
	var stale []ItemKey
	for _, k := range stored {
		if !live[k] {
			stale = append(stale, k)
		}
	}
	return stale
}

// checkMultiplier checks a multiplier before it is stored.
func checkMultiplier(m quantity.Rat) error {
	if m.Sign() <= 0 {
		return fmt.Errorf("%w: %w: %s", ErrInvalid, ErrInvalidMultiplier, m)
	}
	return nil
}
