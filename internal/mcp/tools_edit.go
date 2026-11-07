package mcp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"

	apicatalog "github.com/kaecyra/gobbler/internal/api/catalog"
	"github.com/kaecyra/gobbler/internal/parse"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipe/display"
)

// maxReason bounds the reason an agent gives, which the banner on the recipe
// page and the history page show.
const maxReason = 500

// editLine is one ingredient line of a proposed recipe. It has the shape
// get_recipe gives a line, so an agent can send back what it read.
type editLine struct {
	Key          string `json:"key,omitempty" jsonschema:"the line's key from get_recipe; leave it out for a new line"`
	Text         string `json:"text" jsonschema:"the line as written in a recipe, such as '2 cups flour'"`
	IngredientID int64  `json:"ingredient_id,omitempty" jsonschema:"the catalog ingredient, when the text does not name one the catalog knows"`
	Optional     bool   `json:"optional,omitempty"`
}

type editStep struct {
	ID        string   `json:"id,omitempty" jsonschema:"the step's id from get_recipe; for a new step leave it out, or give a label of your own that depends_on can name"`
	Text      string   `json:"text"`
	Minutes   int      `json:"minutes,omitempty"`
	Passive   bool     `json:"passive,omitempty" jsonschema:"true for a step that needs no attention while it runs"`
	DependsOn []string `json:"depends_on,omitempty" jsonschema:"ids or labels of the steps that must finish first"`
}

type editComponent struct {
	Name        string     `json:"name,omitempty"`
	Ingredients []editLine `json:"ingredients"`
	Steps       []editStep `json:"steps"`
}

type editRecipe struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	SourceURL   string          `json:"source_url,omitempty"`
	Author      string          `json:"author,omitempty"`
	Tags        []string        `json:"tags,omitempty"`
	Yield       string          `json:"yield,omitempty" jsonschema:"such as '2 loaves'"`
	Servings    string          `json:"servings,omitempty"`
	Components  []editComponent `json:"components"`
}

type proposeEditIn struct {
	ID      int64      `json:"id" jsonschema:"the recipe to change"`
	Version int64      `json:"version" jsonschema:"the recipe's version from get_recipe, which the edit was made against"`
	Reason  string     `json:"reason" jsonschema:"why the change is proposed, shown to the person who reviews it"`
	Recipe  editRecipe `json:"recipe" jsonschema:"the whole recipe as it should be after the change"`
}

type proposalOut struct {
	Token string `json:"token" jsonschema:"names the proposal for get_recipe_proposal"`
	// ReviewPath is the page where a person checks and saves the proposal.
	ReviewPath string `json:"review_path"`
	Note       string `json:"note"`
}

type getProposalIn struct {
	Token string `json:"token" jsonschema:"the token propose_recipe_edit returned"`
}

type proposalStateOut struct {
	Token string `json:"token"`
	Kind  string `json:"kind"`
	// State is pending, accepted, rejected, superseded or expired.
	State string `json:"state"`
	// Stale says the recipe has changed since the proposal was made, so it
	// cannot be accepted: propose again against CurrentVersion.
	Stale          bool   `json:"stale"`
	RecipeID       int64  `json:"recipe_id,omitempty"`
	BaseVersion    int64  `json:"base_version,omitempty"`
	CurrentVersion int64  `json:"current_version,omitempty"`
	Reason         string `json:"reason,omitempty"`
	ReviewPath     string `json:"review_path,omitempty"`
	Note           string `json:"note"`
}

const editNote = "This is a proposal. The recipe is unchanged until a person opens the review path in gobbler and saves it."

func (t *tools) registerEdit(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "propose_recipe_edit",
		Description: "Propose a change to an existing recipe: send the whole recipe as it should be, the version you read it at, and a reason. " +
			"Leave the keys and ids from get_recipe on the lines and steps you keep. It leaves a proposal for a person to review; the recipe is unchanged until they save it. " +
			"A newer proposal for the same recipe replaces a pending one.",
		Annotations: drafts,
	}, t.proposeEdit)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_recipe_proposal",
		Description: "Read the state of a proposal: pending, accepted, rejected, superseded or expired. A pending one is stale when the recipe changed after it was made; the result says so and gives the recipe's current version.",
		Annotations: readOnly,
	}, t.getProposal)
}

func (t *tools) proposeEdit(ctx context.Context, _ *mcp.CallToolRequest, in proposeEditIn) (*mcp.CallToolResult, proposalOut, error) {
	const tool = "propose_recipe_edit"
	c, h, err := scoped(ctx)
	if err != nil {
		return nil, proposalOut{}, t.toolError(ctx, tool, err)
	}
	rc := callerOf(c, h)
	reason := oneLine(in.Reason)
	switch {
	case in.ID <= 0 || in.Version <= 0:
		return nil, proposalOut{}, refuse("give the recipe's id and the version you read it at")
	case reason == "":
		return nil, proposalOut{}, refuse("give a reason: the person reviewing the change is shown it")
	case utf8.RuneCountInString(reason) > maxReason:
		return nil, proposalOut{}, refuse("the reason is too long: at most %d characters", maxReason)
	case editSize(in.Recipe) > maxDraftText:
		return nil, proposalOut{}, refuse("the recipe is too long: at most %d bytes", maxDraftText)
	}
	// Info first, for the reason get_recipe reads it first: a recipe saved
	// between the reads makes the proposal stale, never wrong.
	info, err := t.Recipes.Info(ctx, rc, in.ID)
	if err != nil {
		return nil, proposalOut{}, t.toolError(ctx, tool, err)
	}
	if info.Version != in.Version {
		return nil, proposalOut{}, staleRefusal(in.ID, in.Version, info.Version)
	}
	base, err := t.Recipes.Get(ctx, rc, in.ID)
	if err != nil {
		return nil, proposalOut{}, t.toolError(ctx, tool, err)
	}
	next, err := t.editedRecipe(ctx, base, in.Recipe)
	if err != nil {
		return nil, proposalOut{}, t.toolError(ctx, tool, err)
	}
	if err := next.Validate(); err != nil {
		return nil, proposalOut{}, refuse("the recipe is not valid: %s", err)
	}
	if recipe.Compare(base, next).Empty() {
		return nil, proposalOut{}, refuse("the proposed recipe is the same as the recipe now; there is nothing to propose")
	}
	by := provenance(c, h.ID, tool)
	by.Reason = reason
	token, err := t.Recipes.ProposeEdit(ctx, rc, in.ID, in.Version, next, by)
	switch {
	case errors.Is(err, apirecipes.ErrStaleVersion):
		return nil, proposalOut{}, staleRefusal(in.ID, in.Version, t.currentVersion(ctx, rc, in.ID, in.Version))
	case err != nil:
		return nil, proposalOut{}, t.toolError(ctx, tool, err)
	}
	if err := t.recordWrite(ctx, c, h.ID, tool, "proposal "+token[:8]); err != nil {
		return nil, proposalOut{}, t.toolError(ctx, tool, err)
	}
	return nil, proposalOut{Token: token, ReviewPath: EditPath(in.ID, token), Note: editNote}, nil
}

func staleRefusal(id, asked, current int64) error {
	return refuse("recipe %d is at version %d, not %d: read it again with get_recipe and propose against that version", id, current, asked)
}

// currentVersion is the version to name in a stale refusal; when it cannot be
// read the refusal names the version the agent asked for, which is still true
// of what the agent was told.
func (t *tools) currentVersion(ctx context.Context, rc apirecipes.Caller, id, fallback int64) int64 {
	info, err := t.Recipes.Info(ctx, rc, id)
	if err != nil {
		return fallback
	}
	return info.Version
}

func (t *tools) getProposal(ctx context.Context, _ *mcp.CallToolRequest, in getProposalIn) (*mcp.CallToolResult, proposalStateOut, error) {
	const tool = "get_recipe_proposal"
	rc, err := recipeCaller(ctx)
	if err != nil {
		return nil, proposalStateOut{}, t.toolError(ctx, tool, err)
	}
	p, err := t.Recipes.GetProposal(ctx, rc, strings.TrimSpace(in.Token))
	if err != nil {
		return nil, proposalStateOut{}, t.toolError(ctx, tool, err)
	}
	out := proposalStateOut{Token: p.Token, Kind: string(p.Kind), State: string(p.State), Reason: p.Reason, Note: stateNote(p.State)}
	if p.Kind != apirecipes.ProposalEdit {
		return nil, out, nil
	}
	out.RecipeID, out.BaseVersion = p.RecipeID, p.BaseVersion
	info, err := t.Recipes.Info(ctx, rc, p.RecipeID)
	if err != nil {
		return nil, proposalStateOut{}, t.toolError(ctx, tool, err)
	}
	out.CurrentVersion = info.Version
	if p.State == apirecipes.ProposalPending {
		if out.Stale = p.StaleAt(info.Version); out.Stale {
			out.Note = fmt.Sprintf("The recipe was changed after this proposal was made, so it cannot be accepted. Read the recipe again and propose against version %d; the new proposal replaces this one.", info.Version)
		} else {
			out.ReviewPath = EditPath(p.RecipeID, p.Token)
		}
	}
	return nil, out, nil
}

func stateNote(s apirecipes.ProposalState) string {
	switch s {
	case apirecipes.ProposalPending:
		return editNote
	case apirecipes.ProposalAccepted:
		return "A person saved this proposal."
	case apirecipes.ProposalRejected:
		return "A person turned this proposal down."
	case apirecipes.ProposalSuperseded:
		return "A newer proposal for the same recipe replaced this one."
	case apirecipes.ProposalExpired:
		return "Nobody reviewed this proposal in time, so it expired."
	}
	return ""
}

func editSize(r editRecipe) int {
	n := len(r.Name) + len(r.Description) + len(r.SourceURL) + len(r.Author) + len(r.Yield) + len(r.Servings)
	for _, tag := range r.Tags {
		n += len(tag)
	}
	for _, c := range r.Components {
		n += len(c.Name)
		for _, l := range c.Ingredients {
			n += len(l.Text) + len(l.Key)
		}
		for _, s := range c.Steps {
			n += len(s.Text) + len(s.ID)
			for _, d := range s.DependsOn {
				n += len(d)
			}
		}
	}
	return n
}

// editedRecipe builds the recipe an edit proposes: base, with the fields,
// parts, lines and steps the agent sent in place of the ones it has. A line or
// step the agent sends back unchanged is base's own, so nothing about it that
// the tool does not expose (a step's title, a line's exact quantity and unit)
// is lost; one it changed is read afresh, with the key it had. What the tool
// does not expose at all (equipment, times, video, meal type, cuisine, spice
// level, calories, rating) stays as it is.
func (t *tools) editedRecipe(ctx context.Context, base recipe.Recipe, in editRecipe) (recipe.Recipe, error) {
	next := base
	next.Name = strings.TrimSpace(in.Name)
	if next.Name == "" {
		return recipe.Recipe{}, refuse("a recipe needs a name")
	}
	next.Description, next.SourceURL, next.Author = in.Description, strings.TrimSpace(in.SourceURL), strings.TrimSpace(in.Author)
	next.Tags = nil
	for _, tag := range in.Tags {
		if tag = oneLine(tag); tag != "" {
			next.Tags = append(next.Tags, tag)
		}
	}
	var err error
	if next.Yield, err = editYield(base.Yield, in.Yield); err != nil {
		return recipe.Recipe{}, err
	}
	if next.Servings, err = editServings(base.Servings, in.Servings); err != nil {
		return recipe.Recipe{}, err
	}
	if len(in.Components) == 0 {
		return recipe.Recipe{}, refuse("a recipe needs at least one component")
	}

	oldLines, oldSteps := map[string]recipe.IngredientLine{}, map[recipe.StepID]recipe.Step{}
	for _, c := range base.Components {
		for _, l := range c.Lines {
			oldLines[l.Key] = l
		}
		for _, s := range c.Steps {
			oldSteps[s.ID] = s
		}
	}
	lines := display.NewLineBuilder(t.Catalog, display.Default())
	m := &lineMatcher{ctx: ctx, cat: t.Catalog}
	labels := 0
	// Ids the agent wrote, so a label made for an id-less step is never one of them.
	written := map[recipe.StepID]bool{}
	for _, c := range in.Components {
		for _, s := range c.Steps {
			if id := strings.TrimSpace(s.ID); id != "" {
				written[recipe.StepID(id)] = true
			}
		}
	}
	used := map[recipe.StepID]bool{}
	next.Components = nil
	for ci, c := range in.Components {
		comp := recipe.Component{Name: strings.TrimSpace(c.Name)}
		for li, l := range c.Ingredients {
			where := fmt.Sprintf("component %d, ingredient %d", ci+1, li+1)
			line, err := t.editedLine(ctx, lines, m, oldLines, l, where)
			if err != nil {
				return recipe.Recipe{}, err
			}
			comp.Lines = append(comp.Lines, line)
		}
		for si, s := range c.Steps {
			where := fmt.Sprintf("component %d, step %d", ci+1, si+1)
			step, err := editedStep(oldSteps, s, where, &labels, written)
			if err != nil {
				return recipe.Recipe{}, err
			}
			used[step.ID] = true
			comp.Steps = append(comp.Steps, step)
		}
		next.Components = append(next.Components, comp)
	}
	// Equipment names the steps that use it; a step the agent removed takes its
	// links with it rather than leaving the recipe invalid.
	next.Equipment = nil
	for _, e := range base.Equipment {
		link := e
		link.Steps = nil
		for _, id := range e.Steps {
			if used[id] {
				link.Steps = append(link.Steps, id)
			}
		}
		next.Equipment = append(next.Equipment, link)
	}
	return next, nil
}

func (t *tools) editedLine(ctx context.Context, lines *display.LineBuilder, m *lineMatcher, old map[string]recipe.IngredientLine, in editLine, where string) (recipe.IngredientLine, error) {
	text := oneLine(in.Text)
	if text == "" {
		return recipe.IngredientLine{}, refuse("%s: the line has no text", where)
	}
	key := strings.TrimSpace(in.Key)
	var had recipe.IngredientLine
	if key != "" {
		var ok bool
		if had, ok = old[key]; !ok {
			return recipe.IngredientLine{}, refuse("%s: the key %q is not a line of this recipe; leave the key out for a new line", where, key)
		}
		built, err := lines.Build(ctx, had)
		if err != nil {
			return recipe.IngredientLine{}, err
		}
		if built.Text == text && in.Optional == had.Optional && (in.IngredientID == 0 || in.IngredientID == int64(had.Ingredient)) {
			return had, nil
		}
	}
	r := parse.IngredientLine(text, m, m)
	if m.err != nil {
		return recipe.IngredientLine{}, m.err
	}
	id := in.IngredientID
	switch {
	case id != 0:
		// Reading the ingredient is what checks it exists and is the household's.
		if _, err := t.Catalog.GetIngredient(ctx, id); err != nil {
			if errors.Is(err, apicatalog.ErrNotFound) {
				return recipe.IngredientLine{}, refuse("%s: there is no ingredient %d in the catalog", where, id)
			}
			return recipe.IngredientLine{}, err
		}
	case r.Matched:
		id = r.Match.ID
	default:
		return recipe.IngredientLine{}, refuse("%s: %q does not name an ingredient the catalog has. Reword the line with the name of one it has, or give its ingredient_id; get_recipe shows the ids of the ingredients a recipe uses", where, text)
	}
	line := recipe.IngredientLine{
		Key: key, Amount: r.Amount, Ingredient: recipe.IngredientID(id), Optional: r.Optional || in.Optional,
		Rule: quantity.RuleLinear, Raw: text,
	}
	if r.ToTaste {
		line.Rule = quantity.RuleToTaste
	}
	for _, q := range r.Qualifiers {
		kind := recipe.QualifierKind(q.Kind)
		if !kind.Valid() {
			kind = recipe.QualNote
		}
		line.Qualifiers = append(line.Qualifiers, recipe.Qualifier{Kind: kind, Text: q.Text})
	}
	return line, nil
}

func editedStep(old map[recipe.StepID]recipe.Step, in editStep, where string, labels *int, written map[recipe.StepID]bool) (recipe.Step, error) {
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return recipe.Step{}, refuse("%s: the step has no text", where)
	}
	if in.Minutes < 0 {
		return recipe.Step{}, refuse("%s: minutes cannot be negative", where)
	}
	id := recipe.StepID(strings.TrimSpace(in.ID))
	var step recipe.Step
	switch {
	case recipe.IsStepKey(id):
		had, ok := old[id]
		if !ok {
			return recipe.Step{}, refuse("%s: the id %q is not a step of this recipe; leave the id out for a new step", where, id)
		}
		step = had
	case id == "":
		for {
			*labels++
			id = recipe.StepID("new-" + strconv.Itoa(*labels))
			if !written[id] {
				break
			}
		}
	}
	step.ID, step.Text, step.Passive = id, text, in.Passive
	// get_recipe shows whole minutes, so minutes sent back unchanged keep the
	// stored duration, which may have seconds in it.
	if in.Minutes != int(step.Duration.Minutes()) {
		step.Duration = time.Duration(in.Minutes) * time.Minute
	}
	step.DependsOn = nil
	for _, d := range in.DependsOn {
		if d = strings.TrimSpace(d); d != "" {
			step.DependsOn = append(step.DependsOn, recipe.StepID(d))
		}
	}
	return step, nil
}

// editYield reads a yield such as "2 loaves": a quantity and a unit. Text equal
// to base's own is base's.
func editYield(base recipe.Yield, text string) (recipe.Yield, error) {
	text = oneLine(text)
	if text == yieldText(base) {
		return base, nil
	}
	if text == "" {
		return recipe.Yield{}, nil
	}
	words := strings.Fields(text)
	for n := min(3, len(words)); n >= 1; n-- {
		if q, err := quantity.ParseQuantity(strings.Join(words[:n], " ")); err == nil {
			return recipe.Yield{Qty: q, Unit: strings.Join(words[n:], " ")}, nil
		}
	}
	return recipe.Yield{Unit: text}, nil
}

func editServings(base quantity.Quantity, text string) (quantity.Quantity, error) {
	text = oneLine(text)
	if text == base.String() {
		return base, nil
	}
	if text == "" {
		return quantity.Absent(), nil
	}
	q, err := quantity.ParseQuantity(text)
	if err != nil {
		return quantity.Quantity{}, refuse("servings %q is not a quantity: use a number, a fraction such as 1 1/2, or a range such as 4-6", text)
	}
	return q, nil
}

// lineMatcher answers the line parser's catalog questions, which have no
// context or error: it carries the request's context and keeps the first
// catalog failure for the caller, who checks err after parsing.
type lineMatcher struct {
	ctx context.Context
	cat Catalog
	err error
}

func (m *lineMatcher) MatchIngredient(name string) (parse.IngredientMatch, bool) {
	if m.err != nil {
		return parse.IngredientMatch{}, false
	}
	ing, ok, err := m.cat.MatchIngredient(m.ctx, name)
	if err != nil {
		m.err = fmt.Errorf("match ingredient %q: %w", name, err)
		return parse.IngredientMatch{}, false
	}
	return parse.IngredientMatch{ID: ing.ID, Name: ing.Name}, ok
}

func (m *lineMatcher) MatchUnit(name string) (quantity.Unit, bool) {
	if m.err != nil {
		return quantity.Unit{}, false
	}
	u, ok, err := m.cat.MatchUnit(m.ctx, name)
	if err != nil {
		m.err = fmt.Errorf("match unit %q: %w", name, err)
		return quantity.Unit{}, false
	}
	return u.Unit, ok
}
