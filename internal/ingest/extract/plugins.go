package extract

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// IngredientGroup is a named run of ingredient lines as a recipe plugin
// renders it ("For the dough"). Name is empty for lines under no heading.
type IngredientGroup struct {
	Name  string   `json:"name,omitempty"`
	Lines []string `json:"lines"`
}

// Recipe plugins render their card with the groups schema.org drops: JSON-LD
// recipeIngredient is a flat list. Plugins are detected by their markup, never
// by host, so every site running one benefits. Each reader returns groups in
// document order from the first card on the page; a page with none returns nil.
//
// The class names below, and the fixtures under testdata (plugin_*.html),
// were written from recollection of each plugin's markup and have not been
// checked against live pages. A plugin that differs, or renames them later,
// only fails to match: the recipe stays flat.
type pluginReader struct {
	name        string
	ingredients func(doc *html.Node) []IngredientGroup
	steps       func(doc *html.Node) []InstructionStep
}

// plugins are tried in order; the first that yields groups wins.
var plugins = []pluginReader{
	{"wprm", wprmIngredients, wprmSteps},
	{"tasty", tastyIngredients, tastySteps},
	{"create", createIngredients, createSteps},
}

// applyPlugins merges the groups a recipe plugin on the page carries into raw,
// the JSON-LD or microdata recipe. The structured fields stay authoritative:
// groups are adopted only when they hold exactly the same ingredients, in the
// same order, as raw.Ingredients (compared by words, since plugins and
// JSON-LD punctuate differently). Anything else keeps the flat list; a
// mismatched card is a page we do not understand, not an error.
func applyPlugins(doc *html.Node, raw *Raw) {
	for _, p := range plugins {
		groups := p.ingredients(doc)
		if named(groups) {
			if merged, ok := mergeGroups(groups, raw.Ingredients); ok {
				raw.IngredientGroups = merged
			}
		}
		if len(raw.Instructions) > 0 {
			if steps := p.steps(doc); sectioned(steps) {
				if merged, ok := mergeSteps(steps, raw.Instructions); ok {
					raw.Instructions = merged
				}
			}
		}
		if raw.IngredientGroups != nil {
			return
		}
	}
}

func named(groups []IngredientGroup) bool {
	for _, g := range groups {
		if g.Name != "" {
			return true
		}
	}
	return false
}

func sectioned(steps []InstructionStep) bool {
	for _, s := range steps {
		if s.Section != "" {
			return true
		}
	}
	return false
}

// mergeGroups keeps the structured field's own strings and takes only the
// grouping from the plugin.
func mergeGroups(groups []IngredientGroup, flat []string) ([]IngredientGroup, bool) {
	var n int
	for _, g := range groups {
		n += len(g.Lines)
	}
	if n != len(flat) {
		return nil, false
	}
	out := make([]IngredientGroup, 0, len(groups))
	i := 0
	for _, g := range groups {
		m := IngredientGroup{Name: g.Name}
		for _, l := range g.Lines {
			if wordKey(l) != wordKey(flat[i]) {
				return nil, false
			}
			m.Lines = append(m.Lines, flat[i])
			i++
		}
		out = append(out, m)
	}
	return out, true
}

// mergeSteps names the sections of steps the structured field gave flat. It
// does nothing when raw already carries sections of its own.
func mergeSteps(steps, flat []InstructionStep) ([]InstructionStep, bool) {
	if len(steps) != len(flat) || sectioned(flat) {
		return nil, false
	}
	out := make([]InstructionStep, len(flat))
	for i, s := range steps {
		if wordKey(s.Text) != wordKey(flat[i].Text) {
			return nil, false
		}
		out[i] = InstructionStep{Section: s.Section, Text: flat[i].Text}
	}
	return out, true
}

// wordKey is the lower-case words of s, sorted, so punctuation and the order
// a plugin prints amount, unit and note do not matter.
func wordKey(s string) string {
	w := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	sort.Strings(w)
	return strings.Join(w, " ")
}

func hasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

// firstWithClass returns the first element in document order under root
// carrying class, or nil.
func firstWithClass(root *html.Node, class string) *html.Node {
	var found *html.Node
	walk(root, func(n *html.Node) bool {
		if found != nil {
			return false
		}
		if hasClass(n, class) {
			found = n
			return false
		}
		return true
	})
	return found
}

// allWithClass lists the elements under root carrying class, outermost only.
func allWithClass(root *html.Node, class string) []*html.Node {
	var out []*html.Node
	walk(root, func(n *html.Node) bool {
		if hasClass(n, class) {
			out = append(out, n)
			return false
		}
		return true
	})
	return out
}

// itemsWithClass lists the li elements under root carrying class.
func itemsWithClass(root *html.Node, class string) []string {
	var out []string
	walk(root, func(n *html.Node) bool {
		if isElem(n, "li") && hasClass(n, class) {
			if t := textOf(n); t != "" {
				out = append(out, t)
			}
			return false
		}
		return true
	})
	return out
}

// --- WP Recipe Maker: every group is a container with its own name element.

// wprmCard is the first recipe card, so a page that repeats the card (for a
// print view) is read once.
func wprmCard(doc *html.Node) *html.Node {
	if c := firstWithClass(doc, "wprm-recipe-container"); c != nil {
		return c
	}
	return doc
}

func wprmIngredients(doc *html.Node) []IngredientGroup {
	var out []IngredientGroup
	for _, g := range allWithClass(wprmCard(doc), "wprm-recipe-ingredient-group") {
		lines := itemsWithClass(g, "wprm-recipe-ingredient")
		if len(lines) == 0 {
			continue
		}
		out = append(out, IngredientGroup{Name: wprmGroupName(g), Lines: lines})
	}
	return out
}

func wprmSteps(doc *html.Node) []InstructionStep {
	var out []InstructionStep
	for _, g := range allWithClass(wprmCard(doc), "wprm-recipe-instruction-group") {
		name := wprmGroupName(g)
		for _, t := range itemsWithClass(g, "wprm-recipe-instruction") {
			out = append(out, InstructionStep{Section: name, Text: t})
		}
	}
	return out
}

func wprmGroupName(g *html.Node) string {
	if n := firstWithClass(g, "wprm-recipe-group-name"); n != nil {
		return textOf(n)
	}
	return ""
}

// --- Tasty Recipes and Create: a flat container of headings and lists. A
// heading names the items after it.

func tastyIngredients(doc *html.Node) []IngredientGroup {
	return headedGroups(firstWithClass(doc, "tasty-recipes-ingredients"))
}

func tastySteps(doc *html.Node) []InstructionStep {
	return headedSteps(firstWithClass(doc, "tasty-recipes-instructions"))
}

func createIngredients(doc *html.Node) []IngredientGroup {
	return headedGroups(firstWithClass(doc, "mv-create-ingredients"))
}

func createSteps(doc *html.Node) []InstructionStep {
	return headedSteps(firstWithClass(doc, "mv-create-instructions"))
}

// sectionTitles are the headings that label the whole list rather than name a
// group in it.
var sectionTitles = map[string]bool{
	"ingredients": true, "instructions": true, "directions": true, "method": true,
}

// headedItems reads container as a run of headings and list items. An item
// before any heading belongs to the unnamed group.
func headedItems(container *html.Node, f func(name, text string)) {
	if container == nil {
		return
	}
	name := ""
	walk(container, func(n *html.Node) bool {
		switch {
		case len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6':
			if t := textOf(n); !sectionTitles[strings.ToLower(strings.TrimRight(t, ":"))] {
				name = strings.TrimRight(t, ": ")
			}
			return false
		case n.Data == "li":
			if t := textOf(n); t != "" {
				f(name, t)
			}
			return false
		}
		return true
	})
}

func headedGroups(container *html.Node) []IngredientGroup {
	var out []IngredientGroup
	headedItems(container, func(name, text string) {
		if len(out) == 0 || out[len(out)-1].Name != name {
			out = append(out, IngredientGroup{Name: name})
		}
		g := &out[len(out)-1]
		g.Lines = append(g.Lines, text)
	})
	return out
}

func headedSteps(container *html.Node) []InstructionStep {
	var out []InstructionStep
	headedItems(container, func(name, text string) {
		out = append(out, InstructionStep{Section: name, Text: text})
	})
	return out
}
