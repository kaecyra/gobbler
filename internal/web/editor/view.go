package editor

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	apiingest "github.com/kaecyra/gobbler/internal/api/ingest"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipepath"
)

// view is everything the editor templates show.
type view struct {
	ID    int64
	D     draft
	Names lookups
	// Review is set when the form edits an unsaved ingest draft (ADR-00020
	// decision 3) rather than a stored recipe; nil for the recipe editor.
	Review *ReviewOptions
	// Proposal is the summary of the proposed edit under review, shown above the
	// form; nil for an ordinary edit.
	Proposal *proposalView
}

func (v view) reviewing() bool { return v.Review != nil }

// carries reports whether the control group that edits a recipe field is shown.
// A stored recipe carries everything. A draft under review carries only what
// ingest.Draft and Saver.Save keep; a control for anything else is left out
// rather than shown and silently dropped on confirm. This is the one place
// that filters the hidden controls: when the draft learns to carry
// tags or equipment links, switch the group on here.
func (v view) carries(g controlGroup) bool {
	if !v.reviewing() {
		return true
	}
	switch g {
	case groupPlacement, groupStepGraph, groupVideo, groupRecipeFields:
		return true
	}
	return false
}

// controlGroup names a set of editor controls that exist only if the recipe
// being edited can store what they edit.
type controlGroup int

const (
	// groupPlacement: the component a step belongs to.
	groupPlacement controlGroup = iota
	// groupVersion: the stale-save check of a stored recipe.
	groupVersion
	// groupSource: the source URL and tags of a stored recipe.
	groupSource
	// groupVideo: the YouTube video of a recipe; a draft carries it too
	// (ADR-00021 decision 5).
	groupVideo
	// groupScaling: a line's scaling rule.
	groupScaling
	// groupStepGraph: a step's dependencies and passive flag.
	groupStepGraph
	// groupEquipmentLinks: an equipment quantity and the steps that use it.
	groupEquipmentLinks
	// groupRecipeFields: the meal type, cuisine and spice level of a recipe, and the
	// stage kind and title of a step (ADR-00022); a draft carries them too.
	groupRecipeFields
)

func (v view) action() string {
	if v.reviewing() {
		return v.Review.Action
	}
	return editPath(v.ID)
}

func (v view) saveLabel() string {
	if v.reviewing() {
		return "Confirm and save"
	}
	return "Save recipe"
}

// back is the recipe page the editor returns to. The draft name may hold unsaved
// edits, so the slug can be stale; the view page redirects to the canonical URL.
func (v view) back() string { return recipepath.View(v.ID, v.D.Name) }

func (v view) version() string { return strconv.FormatInt(v.D.Version, 10) }

// Field names. Each helper builds the name of a field from positions, matching
// the groups parseForm reads.
func cField(ci int, f string) string { return fmt.Sprintf("c.%d.%s", ci, f) }

func lField(ci, li int, f string) string { return fmt.Sprintf("c.%d.l.%d.%s", ci, li, f) }

func qField(ci, li, qi int, f string) string { return fmt.Sprintf("c.%d.l.%d.q.%d.%s", ci, li, qi, f) }

func sField(ci, si int, f string) string { return fmt.Sprintf("c.%d.s.%d.%s", ci, si, f) }

func eField(ei int, f string) string { return fmt.Sprintf("e.%d.%s", ei, f) }

func nField(ni int, f string) string { return fmt.Sprintf("n.%d.%s", ni, f) }

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

// stepTitle names a step in a list of steps: its title, or the start of its
// instruction when it has none.
func stepTitle(s stepDraft) string {
	if t := strings.TrimSpace(s.Title); t != "" {
		return t
	}
	text := strings.Join(strings.Fields(s.Text), " ")
	if r := []rune(text); len(r) > 48 {
		text = string(r[:48]) + "..."
	}
	return text
}

// stepOption is one step offered in the dependency and equipment choosers.
// Num is the step's place in the whole recipe, counted from one, the way the
// recipe page and cook mode number it.
type stepOption struct {
	ID, Title string
	Num       int
	Passive   bool
}

// stepGroup is the steps of one component, for a chooser grouped by component.
type stepGroup struct {
	Name    string
	Options []stepOption
}

// stepGroups lists every step of the recipe in reading order, grouped by
// component, for the dependency and equipment choosers.
func (v view) stepGroups() []stepGroup {
	var out []stepGroup
	n := 0
	for ci, c := range v.D.Components {
		g := stepGroup{Name: componentName(ci, c)}
		for _, s := range c.Steps {
			n++
			g.Options = append(g.Options, stepOption{ID: s.ID, Title: stepTitle(s), Num: n, Passive: s.Passive})
		}
		if len(g.Options) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// componentName is a component's name, or its place when it has none.
func componentName(ci int, c compDraft) string {
	if n := strings.TrimSpace(c.Name); n != "" {
		return n
	}
	return "Part " + n1(ci)
}

// stepNum is the place of step si of component ci in the whole recipe,
// counted from one.
func (v view) stepNum(ci, si int) int {
	n := si + 1
	for _, c := range v.D.Components[:ci] {
		n += len(c.Steps)
	}
	return n
}

// after is the "after 1, 2" tag of a step's closed row, as the mockup writes
// it, empty when the step waits for none of the steps the form shows.
func (v view) after(s stepDraft) string {
	num := map[string]int{}
	for _, g := range v.stepGroups() {
		for _, o := range g.Options {
			num[o.ID] = o.Num
		}
	}
	var nums []int
	for _, id := range s.Deps {
		if n, ok := num[id]; ok {
			nums = append(nums, n)
		}
	}
	if len(nums) == 0 {
		return ""
	}
	slices.Sort(nums)
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = strconv.Itoa(n)
	}
	return "after " + strings.Join(parts, ", ")
}

// equipmentFor names the equipment the form links to step id.
func (v view) equipmentFor(id string) []string {
	var out []string
	for _, e := range v.D.Equipment {
		if n := strings.TrimSpace(e.Name); n != "" && slices.Contains(e.Steps, id) {
			out = append(out, n)
		}
	}
	return out
}

// lineRef names line li of component ci for a person, unique in the form:
// "line 2", or "Glaze line 2" when the recipe has more than one part.
func (v view) lineRef(ci, li int) string {
	if len(v.D.Components) > 1 {
		return componentName(ci, v.D.Components[ci]) + " line " + n1(li)
	}
	return "line " + n1(li)
}

// partHeaders reports whether the form draws a header row for each part. A
// recipe of one unnamed part is drawn as its lines alone, as the mockup draws
// it; naming it, or adding a part, brings the headers.
func (v view) partHeaders() bool {
	return len(v.D.Components) > 1 || (len(v.D.Components) == 1 && strings.TrimSpace(v.D.Components[0].Name) != "")
}

// ingredientsMeta is the count beside the Ingredients heading: "6 lines", or
// "3 parts · 8 lines".
func (v view) ingredientsMeta() string {
	lines := 0
	for _, c := range v.D.Components {
		lines += len(c.Lines)
	}
	out := plural(lines, "line", "lines")
	if len(v.D.Components) > 1 {
		out = plural(len(v.D.Components), "part", "parts") + " · " + out
	}
	return out
}

// stepMeta is the line beside an open step's title: its part when the recipe
// has headed parts, its duration and its attention, "Bread · 55 min ·
// hands-off".
func (v view) stepMeta(ci int, s stepDraft) string {
	var parts []string
	if v.partHeaders() {
		parts = append(parts, partTitle(v.D.Components[ci]))
	}
	if d := strings.TrimSpace(s.Duration); d != "" {
		parts = append(parts, d)
	}
	if s.Passive {
		parts = append(parts, "hands-off")
	} else {
		parts = append(parts, "hands-on")
	}
	return strings.Join(parts, " · ")
}

// equipRef names a piece of equipment for a button: by its name, or its place
// when it has none.
func equipRef(e equipDraft, ei int) string {
	if n := strings.TrimSpace(e.Name); n != "" {
		return n
	}
	return "equipment " + n1(ei)
}

// spiceLabel is an option of the spice select: 0 is a recipe that is not
// spicy, which is not the same as unset.
func spiceLabel(n string) string {
	if n == "0" {
		return "Not spicy"
	}
	return n + " of 3"
}

// partAnchor is the id of a part's group in the ingredients section.
func partAnchor(ci int) string { return "part-" + n1(ci) }

// partTitle is a part's name in the parts panel.
func partTitle(c compDraft) string {
	if n := strings.TrimSpace(c.Name); n != "" {
		return n
	}
	return "Unnamed part"
}

// partCounts is "4 ingredients · 3 steps" for a part.
func partCounts(c compDraft) string {
	return plural(len(c.Lines), "ingredient", "ingredients") + " · " + plural(len(c.Steps), "step", "steps")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// countWords spells the small counts the parts panel reads out.
var countWords = []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}

func countWord(n int) string {
	if n >= 0 && n < len(countWords) {
		return countWords[n]
	}
	return strconv.Itoa(n)
}

// partsText is the sentence at the top of the parts panel. On the review
// screen, while the draft still has one part for each heading the source
// listed its ingredients under, it says the parts came from those headings.
// Cook mode names the part of each step beside the step count
// (internal/web/cook), which is what the sentence claims.
func (v view) partsText() string {
	n := len(v.D.Components)
	if n <= 1 {
		return "This recipe is one part. If the page groups its ingredients, say for a dough and a glaze, split them into parts so each keeps its own steps."
	}
	rest := "Each part keeps its own ingredients and steps, and cook mode names the part of each step as you go."
	w := countWord(n)
	if v.Review != nil && v.Review.SourceParts == n {
		return "The page lists its ingredients under " + w + " headings, so the recipe is read as " + w + " parts. " + rest
	}
	return "This recipe has " + w + " parts. " + rest
}

// oobAttrs marks the copy of the parts panel an htmx answer swaps out of band.
func oobAttrs(oob bool) templ.Attributes {
	if oob {
		return templ.Attributes{"hx-swap-oob": "true"}
	}
	return templ.Attributes{}
}

// tipID is the id of a help popover, unique in the form.
func tipID(kind string, pos ...int) string {
	return "tip-" + strings.ReplaceAll(act(kind, pos...), ":", "-")
}

// Anchors of the items the save bar can point at.
func lineAnchor(ci, li int) string { return fmt.Sprintf("line-%d-%d", ci, li) }
func stepAnchor(ci, si int) string { return fmt.Sprintf("step-%d-%d", ci, si) }
func equipAnchor(ei int) string    { return fmt.Sprintf("equip-%d", ei) }
func noteAnchor(ni int) string     { return fmt.Sprintf("note-%d", ni) }

// metaOrder is the marked metadata fields in the order the form shows them,
// with the input each one is read from.
var metaOrder = []struct{ field, input string }{
	{"name", "f-name"}, {"description", "f-description"}, {"yield", "f-yield-qty"},
	{"prep", "f-prep"}, {"cook", "f-cook"}, {"total", "f-total"}, {"author", "f-author"},
}

// needsLook counts the items marked low confidence, and gives the anchor of
// the first in the order the form shows them, for the save bar.
func (v view) needsLook() (int, string) {
	n, first := 0, ""
	flag := func(low bool, anchor string) {
		if !low {
			return
		}
		if n == 0 {
			first = anchor
		}
		n++
	}
	for _, m := range metaOrder {
		flag(v.D.mark(m.field).Low, m.input)
	}
	for ci, c := range v.D.Components {
		for li, l := range c.Lines {
			flag(l.Mark.Low, lineAnchor(ci, li))
		}
	}
	for ci, c := range v.D.Components {
		for si, s := range c.Steps {
			flag(s.Mark.Low, stepAnchor(ci, si))
		}
	}
	for ni, nt := range v.D.Notes {
		flag(nt.Mark.Low, noteAnchor(ni))
	}
	for ei, e := range v.D.Equipment {
		flag(e.Mark.Low, equipAnchor(ei))
	}
	return n, first
}

// errFieldOrder is the metadata fields that can carry an error message, in the
// order the form shows them.
var errFieldOrder = []string{
	"name", "yield_qty", "servings", "calories", "prep", "cook", "total",
	"meal_type", "cuisine", "spice", "video",
}

// errAnchor is the id of the message under a metadata field.
func errAnchor(field string) string { return "err-" + field }

// errors counts the fields a refused save found fault with, and gives the
// anchor of the first in the order the form shows them, for the callout above
// the form and the save bar. A message about the recipe as a whole (Problems)
// is not counted: it is shown in its own alert and has no field.
func (v view) errors() (int, string) {
	n, first := 0, ""
	found := func(anchor string) {
		if n == 0 {
			first = anchor
		}
		n++
	}
	for _, f := range errFieldOrder {
		if v.D.Errs[f] != "" {
			found(errAnchor(f))
		}
	}
	for ci, c := range v.D.Components {
		for li, l := range c.Lines {
			if l.Err != "" {
				found(lineAnchor(ci, li))
			}
		}
	}
	for ci, c := range v.D.Components {
		for si, s := range c.Steps {
			if s.Err != "" {
				found(stepAnchor(ci, si))
			}
		}
	}
	for ni, nt := range v.D.Notes {
		if nt.Err != "" {
			found(noteAnchor(ni))
		}
	}
	for ei, e := range v.D.Equipment {
		if e.Err != "" {
			found(equipAnchor(ei))
		}
	}
	return n, first
}

func notSavedText(n int) string {
	if n == 1 {
		return "1 field needs fixing"
	}
	return strconv.Itoa(n) + " fields need fixing"
}

func cantSaveText(n int) string {
	if n == 1 {
		return "1 field stops this being saved"
	}
	return strconv.Itoa(n) + " fields stop this being saved"
}

func needsLookText(n int) string {
	if n == 1 {
		return "1 item still needs a look"
	}
	return strconv.Itoa(n) + " items still need a look"
}

// metaSummary names what the recipe's second disclosure holds, which is less
// for a draft under review: it cannot keep a source URL or tags.
func (v view) metaSummary() string {
	if v.carries(groupSource) {
		return "Source, author, video and tags"
	}
	return "Author and video"
}

// formClass is the form's class. A draft under review gets "reviewing", which
// draws a confidence dot on each line.
func (v view) formClass() string {
	if v.reviewing() {
		return "editor reviewing"
	}
	return "editor"
}

// open reports whether a line's details start open: when the line has
// something the reader must see (a flag, an error, a catalog question), or is
// still being filled in (a line or qualifier just added), so the fields an Add
// button made are in view when the form comes back.
func (l lineDraft) open() bool {
	if l.Mark.Low || l.Err != "" || l.OfferProposal || l.Propose {
		return true
	}
	if strings.TrimSpace(l.Ingredient) == "" && strings.TrimSpace(l.Raw) == "" {
		return true
	}
	return slices.ContainsFunc(l.Quals, func(q qualDraft) bool { return strings.TrimSpace(q.Text) == "" })
}

// qualText is the line's package size and qualifiers as its closed row shows
// them.
func (l lineDraft) qualText() string {
	var parts []string
	if l.hasPackage() {
		parts = append(parts, "("+strings.TrimSpace(l.PkgQty+" "+l.PkgUnit)+")")
	}
	for _, q := range l.Quals {
		if t := strings.TrimSpace(q.Text); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, ", ")
}

// open reports whether a step's details start open, on the same terms as a
// line's: something to check, or a step just added.
func (s stepDraft) open() bool {
	return s.Mark.Low || s.Err != "" || strings.TrimSpace(s.Text) == ""
}

// fromLLM reports whether the LLM supplied the step's reading or its order and
// passive flag. The step shows it as one LLM pill either way.
func (s stepDraft) fromLLM() bool { return s.Mark.LLM || s.Graph.LLM }

// open reports whether an equipment row's details start open, on the same
// terms as a line's.
func (e equipDraft) open() bool {
	return e.Mark.Low || e.Err != "" || e.OfferProposal || e.Propose || strings.TrimSpace(e.Name) == ""
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

// mealOptions is the closed set the meal type select offers; the option text
// is MealType.Label.
func mealOptions() []recipe.MealType { return recipe.MealTypes() }

// cuisineOption is one entry of the cuisine select.
type cuisineOption struct{ Key, Label string }

func cuisineOptions() []cuisineOption {
	var out []cuisineOption
	for _, c := range recipe.Cuisines() {
		out = append(out, cuisineOption{string(c), c.Label()})
	}
	return out
}

// stageOptions is the closed set the step kind select offers.
func stageOptions() []string {
	var out []string
	for _, k := range recipe.StageKinds() {
		out = append(out, string(k))
	}
	return out
}

// spiceOptions are the levels 0 to recipe.MaxSpiceLevel as the form posts them.
func spiceOptions() []string {
	var out []string
	for n := 0; n <= recipe.MaxSpiceLevel; n++ {
		out = append(out, strconv.Itoa(n))
	}
	return out
}

// noteIssueText words the issue codes ingest puts on a candidate note, for the
// hint under it.
func noteIssueText(codes []string) []string {
	words := map[string]string{
		apiingest.IssueNoteTooLong:     "it is longer than one short paragraph",
		apiingest.IssueNoteNutrition:   "it reads as nutrition facts",
		apiingest.IssueNotePublication: "it reads as the page's publication history",
		apiingest.IssueNoteMealPlan:    "it reads as a meal plan",
	}
	var out []string
	for _, c := range codes {
		if w, ok := words[c]; ok {
			out = append(out, w)
		}
	}
	return out
}

// noteHint is the sentence under a flagged note.
func noteHint(issues []string) string {
	if w := noteIssueText(issues); len(w) > 0 {
		return "this may not be a tip to keep: " + strings.Join(w, "; ") + ". Trim it or remove it."
	}
	return "the rules were not sure this is a short tip."
}

// noteRef names a note for a button: by its title, or its place when it has
// none.
func noteRef(n noteDraft, ni int) string {
	if t := strings.TrimSpace(n.Title); t != "" {
		return "note " + t
	}
	return "note " + n1(ni)
}

// blank reports whether a note row has no title and no text: an unused row.
func (n noteDraft) blank() bool {
	return strings.TrimSpace(n.Title) == "" && strings.TrimSpace(n.Text) == ""
}
