package shopping

import (
	"fmt"
	"sort"
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

// ShareItem is an item as one contributor gives it: the item (key, check state
// and merged amounts as on the by-aisle list) with that contributor's share.
// Checking it checks the item, wherever else it appears.
type ShareItem struct {
	ViewItem
	Share Share
}

// RecipeGroup is the items one recipe contributes to the list, or the items
// added by hand (Manual set, zero Source).
type RecipeGroup struct {
	Source Source
	Manual bool
	Items  []ShareItem
}

// ByRecipe regroups the view's items under the recipes that give them: one
// group per recipe on the list, in entry order, then one for manual items. An
// item several contributors give appears in each of their groups with that
// contributor's share. A recipe that contributes nothing to the list (all its
// lines are in the pantry or optional) has no group. Within a group items keep
// the aisle order of the by-aisle list.
func (v View) ByRecipe() []RecipeGroup {
	byKey := map[Source]*RecipeGroup{} // the manual group is keyed by the zero Source
	var groups []*RecipeGroup
	for _, a := range v.Aisles {
		for _, it := range a.Items {
			for _, s := range it.Shares {
				g, ok := byKey[s.Source]
				if !ok {
					g = &RecipeGroup{Source: s.Source, Manual: s.Manual}
					byKey[s.Source] = g
					groups = append(groups, g)
				}
				g.Items = append(g.Items, ShareItem{ViewItem: it, Share: s})
			}
		}
	}
	rank := map[int64]int{}
	for i, e := range v.Entries {
		rank[e.RecipeID] = i
	}
	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		if a.Manual != b.Manual {
			return b.Manual
		}
		return rank[a.Source.RecipeID] < rank[b.Source.RecipeID]
	})
	out := make([]RecipeGroup, len(groups))
	for i, g := range groups {
		out[i] = *g
	}
	return out
}
