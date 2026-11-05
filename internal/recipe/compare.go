package recipe

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ChangeKind says what a comparison found for one element.
type ChangeKind string

// Change kinds. A kept element that differs, or has moved, is changed.
const (
	Added   ChangeKind = "added"
	Removed ChangeKind = "removed"
	Changed ChangeKind = "changed"
)

// FieldChange is one recipe-level field that differs. Field is a stable name
// (see the constants below); Old and New are plain text, empty for unset.
type FieldChange struct {
	Field, Label string
	Old, New     string
}

// Field names of a FieldChange.
const (
	FieldName        = "name"
	FieldDescription = "description"
	FieldSourceURL   = "source_url"
	FieldAuthor      = "author"
	FieldVideo       = "video"
	FieldMealType    = "meal_type"
	FieldCuisine     = "cuisine"
	FieldSpice       = "spice"
	FieldYield       = "yield"
	FieldServings    = "servings"
	FieldCalories    = "calories"
	FieldTags        = "tags"
	FieldPrep        = "prep"
	FieldCook        = "cook"
	FieldTotal       = "total"
	FieldSpecial     = "special_times"
)

// ComponentChange is a part that was added, removed or renamed. Indexes are
// positions in the old and new recipe, -1 where the part is absent.
type ComponentChange struct {
	Kind               ChangeKind
	OldIndex, NewIndex int
	OldName, NewName   string
}

// LineChange is one ingredient line that differs between two versions. Old is
// nil for an added line and New for a removed one. Edited says its content
// differs; Moved that it sits in another part or place than before. Component
// and Index locate the line in the old version, or in the new for an added
// line.
type LineChange struct {
	Kind          ChangeKind
	Old, New      *IngredientLine
	Edited, Moved bool
	// OldComponent and NewComponent are -1 where the line is absent.
	OldComponent, NewComponent int
}

// Key is the line's key in whichever version holds one; empty for a line from
// before keys.
func (c LineChange) Key() string {
	switch {
	case c.Old != nil && c.Old.Key != "":
		return c.Old.Key
	case c.New != nil:
		return c.New.Key
	}
	return ""
}

// StepChange is one step that differs between two versions; see LineChange.
// OldNumber and NewNumber are the steps' numbers in reading order, counted from
// 1, and 0 where the step is absent.
type StepChange struct {
	Kind                       ChangeKind
	Old, New                   *Step
	Edited, Moved              bool
	OldComponent, NewComponent int
	OldNumber, NewNumber       int
}

// EquipmentChange is a piece of equipment that was added, removed or whose
// quantity or steps changed. Equipment is paired by the catalog entry.
type EquipmentChange struct {
	Kind     ChangeKind
	Old, New *EquipmentLink
}

// Diff is everything that differs between two versions of a recipe.
type Diff struct {
	Fields     []FieldChange
	Components []ComponentChange
	Lines      []LineChange
	Steps      []StepChange
	Equipment  []EquipmentChange
	// NewToOld maps each part of the newer version to the part of the older it
	// pairs with, or -1 for a part that is new. The ComponentChange of a pair
	// that only moved is not listed, so this is how a caller finds where an
	// added line or step belongs in the older version.
	NewToOld []int
	// ByPosition says the older version carried no keys, so its steps and
	// lines were paired with the newer one's by their position.
	ByPosition bool
}

// Empty reports whether the two versions are the same recipe.
func (d Diff) Empty() bool {
	return len(d.Fields) == 0 && len(d.Components) == 0 && len(d.Lines) == 0 && len(d.Steps) == 0 && len(d.Equipment) == 0
}

// Count is the number of changes of every kind.
func (d Diff) Count() int {
	return len(d.Fields) + len(d.Components) + len(d.Lines) + len(d.Steps) + len(d.Equipment)
}

// LineByOldKey returns the change to the line that has key in the old version.
func (d Diff) LineByOldKey(key string) (LineChange, bool) {
	for _, c := range d.Lines {
		if c.Old != nil && c.Old.Key == key && key != "" {
			return c, true
		}
	}
	return LineChange{}, false
}

// StepByOldID returns the change to the step that has id in the old version.
func (d Diff) StepByOldID(id StepID) (StepChange, bool) {
	for _, c := range d.Steps {
		if c.Old != nil && c.Old.ID == id && id != "" {
			return c, true
		}
	}
	return StepChange{}, false
}

// Compare finds what changed from old to next (ADR-00035). Steps and ingredient
// lines are paired by their keys: an element of next whose key is in old is the
// same element, and a key in only one version is an addition or a removal.
// Against an old version that holds no keys at all (a revision written before
// keys existed) they are paired by position, counted in reading order. Parts are
// paired by name, then by what remains in order; equipment by catalog entry.
// Compare changes neither argument.
func Compare(old, next Recipe) Diff {
	d := Diff{Fields: compareFields(old, next)}
	oldComp, newComp := pairComponents(old.Components, next.Components)
	d.NewToOld = newComp
	for i, c := range old.Components {
		if oldComp[i] < 0 {
			d.Components = append(d.Components, ComponentChange{Kind: Removed, OldIndex: i, NewIndex: -1, OldName: c.Name})
		}
	}
	for j, c := range next.Components {
		switch o := newComp[j]; {
		case o < 0:
			d.Components = append(d.Components, ComponentChange{Kind: Added, OldIndex: -1, NewIndex: j, NewName: c.Name})
		case old.Components[o].Name != c.Name:
			d.Components = append(d.Components, ComponentChange{Kind: Changed, OldIndex: o, NewIndex: j, OldName: old.Components[o].Name, NewName: c.Name})
		}
	}
	d.ByPosition = !anyKeyed(old)
	d.Lines = compareLines(old, next, d.ByPosition, oldComp)
	d.Steps = compareSteps(old, next, d.ByPosition, oldComp)
	d.Equipment = compareEquipment(old.Equipment, next.Equipment)
	return d
}

func anyKeyed(r Recipe) bool {
	for _, c := range r.Components {
		for _, l := range c.Lines {
			if IsLineKey(l.Key) {
				return true
			}
		}
		for _, s := range c.Steps {
			if IsStepKey(s.ID) {
				return true
			}
		}
	}
	return false
}

// pairComponents pairs parts by name and then, in order, those left over. It
// returns, for each old part, the new part it pairs with, and the reverse; -1
// is unpaired.
func pairComponents(old, next []Component) (oldTo, newTo []int) {
	oldTo, newTo = make([]int, len(old)), make([]int, len(next))
	for i := range oldTo {
		oldTo[i] = -1
	}
	for j := range newTo {
		newTo[j] = -1
	}
	for i, o := range old {
		if o.Name == "" {
			continue
		}
		for j, n := range next {
			if newTo[j] < 0 && n.Name == o.Name {
				oldTo[i], newTo[j] = j, i
				break
			}
		}
	}
	j := 0
	for i := range old {
		if oldTo[i] >= 0 {
			continue
		}
		for j < len(next) && newTo[j] >= 0 {
			j++
		}
		if j < len(next) {
			oldTo[i], newTo[j] = j, i
		}
	}
	return oldTo, newTo
}

type located[T any] struct {
	v         T
	comp, pos int
	key       string
}

func flatLines(r Recipe) []located[IngredientLine] {
	var out []located[IngredientLine]
	for ci, c := range r.Components {
		for li, l := range c.Lines {
			out = append(out, located[IngredientLine]{l, ci, li, l.Key})
		}
	}
	return out
}

func flatSteps(r Recipe) []located[Step] {
	var out []located[Step]
	for ci, c := range r.Components {
		for si, s := range c.Steps {
			out = append(out, located[Step]{s, ci, si, string(s.ID)})
		}
	}
	return out
}

// pairing pairs the elements of two flat lists. It returns, for each old
// element, the index of its partner in next, or -1.
func pairing[T any](old, next []located[T], byPosition bool, shaped func(string) bool) []int {
	to := make([]int, len(old))
	for i := range to {
		to[i] = -1
	}
	if byPosition {
		for i := range old {
			if i < len(next) {
				to[i] = i
			}
		}
		return to
	}
	at := map[string]int{}
	for j, n := range next {
		if shaped(n.key) {
			at[n.key] = j
		}
	}
	for i, o := range old {
		if !shaped(o.key) {
			continue
		}
		if j, ok := at[o.key]; ok {
			to[i] = j
		}
	}
	return to
}

// outOfOrder marks the kept elements that are not in the longest run that keeps
// its relative order: they are the ones that moved. to maps each old element to
// its partner in the new list, -1 for none.
func outOfOrder(to []int) map[int]bool {
	var idx []int
	for i, j := range to {
		if j >= 0 {
			idx = append(idx, i)
		}
	}
	n := len(idx)
	best, prev := make([]int, n), make([]int, n)
	top := -1
	for a := 0; a < n; a++ {
		best[a], prev[a] = 1, -1
		for b := 0; b < a; b++ {
			if to[idx[b]] < to[idx[a]] && best[b]+1 > best[a] {
				best[a], prev[a] = best[b]+1, b
			}
		}
		if top < 0 || best[a] > best[top] {
			top = a
		}
	}
	moved := map[int]bool{}
	for _, i := range idx {
		moved[i] = true
	}
	for a := top; a >= 0; a = prev[a] {
		delete(moved, idx[a])
	}
	return moved
}

func compareLines(old, next Recipe, byPosition bool, oldComp []int) []LineChange {
	o, n := flatLines(old), flatLines(next)
	to := pairing(o, n, byPosition, IsLineKey)
	moved := outOfOrder(to)
	paired := map[int]bool{}
	var out []LineChange
	for i, x := range o {
		j := to[i]
		if j < 0 {
			xv := x.v
			out = append(out, LineChange{Kind: Removed, Old: &xv, OldComponent: x.comp, NewComponent: -1})
			continue
		}
		paired[j] = true
		y := n[j]
		edited := !sameLine(x.v, y.v)
		mv := moved[i] || oldComp[x.comp] != y.comp
		if !edited && !mv {
			continue
		}
		xv, yv := x.v, y.v
		out = append(out, LineChange{Kind: Changed, Old: &xv, New: &yv, Edited: edited, Moved: mv, OldComponent: x.comp, NewComponent: y.comp})
	}
	for j, y := range n {
		if !paired[j] {
			yv := y.v
			out = append(out, LineChange{Kind: Added, New: &yv, OldComponent: -1, NewComponent: y.comp})
		}
	}
	return out
}

func compareSteps(old, next Recipe, byPosition bool, oldComp []int) []StepChange {
	o, n := flatSteps(old), flatSteps(next)
	to := pairing(o, n, byPosition, func(k string) bool { return IsStepKey(StepID(k)) })
	moved := outOfOrder(to)
	paired := map[int]bool{}
	var out []StepChange
	for i, x := range o {
		j := to[i]
		if j < 0 {
			xv := x.v
			out = append(out, StepChange{Kind: Removed, Old: &xv, OldComponent: x.comp, NewComponent: -1, OldNumber: i + 1})
			continue
		}
		paired[j] = true
		y := n[j]
		edited := !sameStep(x.v, y.v)
		mv := moved[i] || oldComp[x.comp] != y.comp
		if !edited && !mv {
			continue
		}
		xv, yv := x.v, y.v
		out = append(out, StepChange{Kind: Changed, Old: &xv, New: &yv, Edited: edited, Moved: mv, OldComponent: x.comp, NewComponent: y.comp, OldNumber: i + 1, NewNumber: j + 1})
	}
	for j, y := range n {
		if !paired[j] {
			yv := y.v
			out = append(out, StepChange{Kind: Added, New: &yv, OldComponent: -1, NewComponent: y.comp, NewNumber: j + 1})
		}
	}
	return out
}

func sameLine(a, b IngredientLine) bool {
	if !a.Amount.Qty.Equal(b.Amount.Qty) || a.Amount.Unit.Name != b.Amount.Unit.Name || a.Amount.HasPackage != b.Amount.HasPackage {
		return false
	}
	if a.Amount.HasPackage && (!a.Amount.Package.Qty.Equal(b.Amount.Package.Qty) || a.Amount.Package.Unit.Name != b.Amount.Package.Unit.Name) {
		return false
	}
	return a.Ingredient == b.Ingredient && a.Optional == b.Optional && a.Rule == b.Rule &&
		slices.Equal(a.Qualifiers, b.Qualifiers)
}

func sameStep(a, b Step) bool {
	return a.Text == b.Text && a.Duration == b.Duration && a.Passive == b.Passive && a.Title == b.Title &&
		a.Kind == b.Kind && slices.Equal(a.DependsOn, b.DependsOn)
}

func compareEquipment(old, next []EquipmentLink) []EquipmentChange {
	var out []EquipmentChange
	used := map[int]bool{}
	for _, o := range old {
		found := -1
		for j, n := range next {
			if !used[j] && n.Equipment == o.Equipment {
				found = j
				break
			}
		}
		ov := o
		if found < 0 {
			out = append(out, EquipmentChange{Kind: Removed, Old: &ov})
			continue
		}
		used[found] = true
		nv := next[found]
		if !o.Qty.Equal(nv.Qty) || !slices.Equal(o.Steps, nv.Steps) {
			out = append(out, EquipmentChange{Kind: Changed, Old: &ov, New: &nv})
		}
	}
	for j, n := range next {
		if !used[j] {
			nv := n
			out = append(out, EquipmentChange{Kind: Added, New: &nv})
		}
	}
	return out
}

func compareFields(old, next Recipe) []FieldChange {
	var out []FieldChange
	add := func(field, label, a, b string) {
		if a != b {
			out = append(out, FieldChange{Field: field, Label: label, Old: a, New: b})
		}
	}
	add(FieldName, "Name", old.Name, next.Name)
	add(FieldDescription, "Description", old.Description, next.Description)
	add(FieldSourceURL, "Source", old.SourceURL, next.SourceURL)
	add(FieldAuthor, "Author", old.Author, next.Author)
	add(FieldVideo, "Video", old.VideoID, next.VideoID)
	add(FieldMealType, "Meal type", string(old.MealType), string(next.MealType))
	add(FieldCuisine, "Cuisine", string(old.Cuisine), string(next.Cuisine))
	add(FieldSpice, "Spice level", intText(old.SpiceLevel), intText(next.SpiceLevel))
	add(FieldYield, "Makes", yieldText(old.Yield), yieldText(next.Yield))
	add(FieldServings, "Serves", old.Servings.String(), next.Servings.String())
	add(FieldCalories, "Calories per serving", intText(old.CaloriesPerServing), intText(next.CaloriesPerServing))
	add(FieldTags, "Tags", strings.Join(old.Tags, ", "), strings.Join(next.Tags, ", "))
	add(FieldPrep, "Prep time", minutesText(old.Times.Prep), minutesText(next.Times.Prep))
	add(FieldCook, "Cook time", minutesText(old.Times.Cook), minutesText(next.Times.Cook))
	add(FieldTotal, "Total time", minutesText(old.Times.Total), minutesText(next.Times.Total))
	if !reflect.DeepEqual(nonNil(old.Times.Special), nonNil(next.Times.Special)) {
		add(FieldSpecial, "Waiting times", specialText(old.Times.Special), specialText(next.Times.Special))
	}
	return out
}

func nonNil(s []SpecialTime) []SpecialTime {
	if s == nil {
		return []SpecialTime{}
	}
	return s
}

func yieldText(y Yield) string { return strings.TrimSpace(y.Qty.String() + " " + y.Unit) }

func intText(p *int) string {
	if p == nil {
		return ""
	}
	return strconv.Itoa(*p)
}

// minutesText writes a duration as whole minutes where it is one, and in
// seconds otherwise; zero (unknown) is empty.
func minutesText(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d%time.Minute == 0:
		return fmt.Sprintf("%d min", d/time.Minute)
	}
	return fmt.Sprintf("%d sec", (d+time.Second/2)/time.Second)
}

func specialText(s []SpecialTime) string {
	parts := make([]string, len(s))
	for i, t := range s {
		parts[i] = strings.TrimSpace(t.Label + " " + minutesText(t.Duration))
	}
	return strings.Join(parts, ", ")
}
