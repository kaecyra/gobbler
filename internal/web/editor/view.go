package editor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// view is everything the editor templates show.
type view struct {
	ID    int64
	D     draft
	Names lookups
}

func (v view) action() string { return editPath(v.ID) }

func (v view) back() string { return recipePath(v.ID) }

func (v view) version() string { return strconv.FormatInt(v.D.Version, 10) }

// Field names. Each helper builds the name of a field from positions, matching
// the groups parseForm reads.
func cField(ci int, f string) string { return fmt.Sprintf("c.%d.%s", ci, f) }

func lField(ci, li int, f string) string { return fmt.Sprintf("c.%d.l.%d.%s", ci, li, f) }

func qField(ci, li, qi int, f string) string { return fmt.Sprintf("c.%d.l.%d.q.%d.%s", ci, li, qi, f) }

func sField(ci, si int, f string) string { return fmt.Sprintf("c.%d.s.%d.%s", ci, si, f) }

func eField(ei int, f string) string { return fmt.Sprintf("e.%d.%s", ei, f) }

// Action values for the structural buttons.
func act(verb string, pos ...int) string {
	parts := make([]string, len(pos))
	for i, p := range pos {
		parts[i] = strconv.Itoa(p)
	}
	if len(parts) == 0 {
		return verb
	}
	return verb + ":" + strings.Join(parts, ".")
}

// Labels for people: positions are shown from one.
func n1(i int) string { return strconv.Itoa(i + 1) }

func componentLabel(ci int, c compDraft) string {
	if strings.TrimSpace(c.Name) == "" {
		return "Component " + n1(ci)
	}
	return "Component " + n1(ci) + ": " + c.Name
}

// stepLabel names a step in the list of dependencies: where it is and how it
// starts.
func stepLabel(si int, c compDraft, s stepDraft) string {
	text := strings.Join(strings.Fields(s.Text), " ")
	if r := []rune(text); len(r) > 48 {
		text = string(r[:48]) + "..."
	}
	where := "Step " + n1(si)
	if strings.TrimSpace(c.Name) != "" {
		where = c.Name + " step " + n1(si)
	}
	if text == "" {
		return where
	}
	return where + ": " + text
}

type stepOption struct {
	ID, Label string
}

// stepOptions lists every step of the recipe in reading order, for the
// dependency and equipment choosers.
func (v view) stepOptions() []stepOption {
	var out []stepOption
	for _, c := range v.D.Components {
		for si, s := range c.Steps {
			out = append(out, stepOption{ID: s.ID, Label: stepLabel(si, c, s)})
		}
	}
	return out
}

func hasID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func ruleOptions() []string {
	return []string{string(quantity.RuleLinear), string(quantity.RuleFixed), string(quantity.RuleToTaste)}
}

func qualKinds() []string {
	return []string{
		string(recipe.QualPreparation), string(recipe.QualState), string(recipe.QualForm), string(recipe.QualNote),
	}
}

func (l lineDraft) ruleIs(r string) bool {
	if l.Rule == "" {
		return r == string(quantity.RuleLinear)
	}
	return l.Rule == r
}

func (l lineDraft) hasPackage() bool { return l.PkgQty != "" || l.PkgUnit != "" }
func n0(i int) string                { return strconv.Itoa(i) }
