package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	apiingest "github.com/kaecyra/gobbler/internal/api/ingest"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// The held-draft tools (ADR-00048): an agent reads a pending ingestion draft
// as the review screen receives it, lists the household's pending drafts, and
// replaces a draft's recipe with its own revision through the replace the
// review screen uses. A revision never saves the draft; the person still
// confirms it on the review screen.

// draftText is a text field of a draft: its value, how sure the rules were of
// it (0 to 1), and whether an LLM or an agent supplied it. A revision reads
// only the value.
type draftText struct {
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence,omitempty" jsonschema:"0 to 1; ignored by revise_recipe_draft"`
	FromLLM    bool    `json:"from_llm,omitempty" jsonschema:"an LLM or an agent supplied the value; ignored by revise_recipe_draft"`
}

// draftNumber is a whole-number field of a draft, as draftText.
type draftNumber struct {
	Value      int     `json:"value"`
	Confidence float64 `json:"confidence,omitempty" jsonschema:"0 to 1; ignored by revise_recipe_draft"`
	FromLLM    bool    `json:"from_llm,omitempty" jsonschema:"an LLM or an agent supplied the value; ignored by revise_recipe_draft"`
}

type draftLine struct {
	Text         string `json:"text" jsonschema:"the line as written in a recipe, such as '2 cups flour, sifted'"`
	IngredientID int64  `json:"ingredient_id,omitempty" jsonschema:"the catalog ingredient, when the text does not name one the catalog knows"`
	Optional     bool   `json:"optional,omitempty"`
	// The rest is how the line parser read Text; a revision ignores it and
	// reads a changed line afresh.
	Name       string   `json:"name,omitempty" jsonschema:"the ingredient name as read; ignored by revise_recipe_draft"`
	Quantity   string   `json:"quantity,omitempty" jsonschema:"ignored by revise_recipe_draft"`
	Unit       string   `json:"unit,omitempty" jsonschema:"ignored by revise_recipe_draft"`
	ToTaste    bool     `json:"to_taste,omitempty" jsonschema:"ignored by revise_recipe_draft"`
	Qualifiers []string `json:"qualifiers,omitempty" jsonschema:"ignored by revise_recipe_draft"`
	Confidence float64  `json:"confidence,omitempty" jsonschema:"ignored by revise_recipe_draft"`
	Issues     []string `json:"issues,omitempty" jsonschema:"why the confidence is below 1; ignored by revise_recipe_draft"`
	FromLLM    bool     `json:"from_llm,omitempty" jsonschema:"ignored by revise_recipe_draft"`
}

type draftStep struct {
	Text    string `json:"text"`
	Title   string `json:"title,omitempty" jsonschema:"a short heading for the step, if it has one"`
	Kind    string `json:"kind,omitempty" jsonschema:"what the step is doing, for the time ribbon, such as prep or cook; empty when unknown"`
	Minutes int    `json:"minutes,omitempty"`
	Passive bool   `json:"passive,omitempty" jsonschema:"true for a step that needs no attention while it runs"`
	// DependsOn uses step numbers: from 1, in reading order across every
	// component.
	DependsOn    []int   `json:"depends_on,omitempty" jsonschema:"the numbers of the steps that must finish first; steps are numbered from 1 in reading order across every component"`
	Confidence   float64 `json:"confidence,omitempty" jsonschema:"ignored by revise_recipe_draft"`
	FromLLM      bool    `json:"from_llm,omitempty" jsonschema:"ignored by revise_recipe_draft"`
	GraphFromLLM bool    `json:"graph_from_llm,omitempty" jsonschema:"an LLM or an agent set passive and depends_on; ignored by revise_recipe_draft"`
}

type draftComponent struct {
	Name  string      `json:"name,omitempty"`
	Lines []draftLine `json:"lines"`
	Steps []draftStep `json:"steps"`
}

type draftAuthorNote struct {
	Title      string   `json:"title,omitempty"`
	Text       string   `json:"text"`
	Confidence float64  `json:"confidence,omitempty" jsonschema:"ignored by revise_recipe_draft"`
	Issues     []string `json:"issues,omitempty" jsonschema:"ignored by revise_recipe_draft"`
}

type draftEquipment struct {
	Name        string `json:"name"`
	EquipmentID int64  `json:"equipment_id,omitempty" jsonschema:"the catalog entry; give it back for equipment you keep, and leave it out for new equipment"`
	FromLLM     bool   `json:"from_llm,omitempty" jsonschema:"ignored by revise_recipe_draft"`
}

// draftRecipe is the recipe of a draft, in the shape get_recipe_draft gives
// and revise_recipe_draft takes back.
type draftRecipe struct {
	Name         draftText         `json:"name"`
	Author       draftText         `json:"author,omitempty"`
	Description  draftText         `json:"description,omitempty"`
	Yield        draftText         `json:"yield,omitempty" jsonschema:"such as '2 loaves'"`
	Servings     draftText         `json:"servings,omitempty" jsonschema:"a number, a fraction or a range such as 4-6"`
	Calories     draftNumber       `json:"calories,omitempty" jsonschema:"per serving"`
	PrepMinutes  draftNumber       `json:"prep_minutes,omitempty"`
	CookMinutes  draftNumber       `json:"cook_minutes,omitempty"`
	TotalMinutes draftNumber       `json:"total_minutes,omitempty"`
	MealType     string            `json:"meal_type,omitempty"`
	Cuisine      string            `json:"cuisine,omitempty"`
	Components   []draftComponent  `json:"components"`
	AuthorNotes  []draftAuthorNote `json:"author_notes,omitempty"`
	Equipment    []draftEquipment  `json:"equipment,omitempty"`
}

type getDraftIn struct {
	Token string `json:"token" jsonschema:"the draft's token from list_recipe_drafts, import_recipe or reimport_recipe"`
}

type getDraftOut struct {
	Token string `json:"token"`
	// Revision names the draft's content; revise_recipe_draft takes it back
	// and is refused once the draft has changed.
	Revision   string `json:"revision"`
	ReviewPath string `json:"review_path"`
	SourceURL  string `json:"source_url,omitempty"`
	// ParserVersion and Method say what read the source and how.
	ParserVersion string `json:"parser_version"`
	Method        string `json:"method,omitempty"`
	UsedLLM       bool   `json:"used_llm"`
	// ReimportOf is the recipe a re-import draft replaces when it is saved.
	ReimportOf int64 `json:"reimport_of,omitempty"`
	// Reason is what the agent that last revised the draft said.
	Reason string      `json:"reason,omitempty"`
	Recipe draftRecipe `json:"recipe"`
	Note   string      `json:"note"`
}

type draftSummaryOut struct {
	Token      string `json:"token"`
	Name       string `json:"name"`
	SourceURL  string `json:"source_url,omitempty"`
	CreatedAt  string `json:"created_at"`
	Reimport   bool   `json:"reimport,omitempty"`
	Revised    bool   `json:"revised_by_agent"`
	ReviewPath string `json:"review_path"`
}

type reviseDraftIn struct {
	Token    string      `json:"token" jsonschema:"the draft's token"`
	Revision string      `json:"revision" jsonschema:"the revision get_recipe_draft gave when you read the draft"`
	Recipe   draftRecipe `json:"recipe" jsonschema:"the whole recipe as it should be, in the shape get_recipe_draft gives"`
	Reason   string      `json:"reason" jsonschema:"what you changed and why, shown to the person who reviews the draft"`
}

type reviseDraftOut struct {
	Token string `json:"token"`
	// Revision is the draft's new revision, for a further revision.
	Revision   string `json:"revision"`
	ReviewPath string `json:"review_path"`
	Note       string `json:"note"`
}

const reviseNote = "The draft is revised and still waiting for review. What you changed is marked on the review screen; nothing is saved until a person confirms it there."

func (t *tools) registerDrafts(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_recipe_draft",
		Description: "Read one pending recipe draft as the review screen shows it: every field with its confidence and whether an LLM or agent supplied it, " +
			"components with ingredient lines and steps, author notes, times, equipment, meal type and cuisine, the parse method and parser version. " +
			"It gives the draft's revision, which revise_recipe_draft takes back.",
		Annotations: readOnly,
	}, t.getRecipeDraft)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_recipe_drafts",
		Description: "List the household's recipe drafts waiting for review, re-imports included, oldest first. Returns one page and a cursor for the next.",
		Annotations: readOnly,
	}, t.listRecipeDrafts)
	mcp.AddTool(s, &mcp.Tool{
		Name: "revise_recipe_draft",
		Description: "Replace a pending draft's recipe with your revision of it: send the whole recipe in the shape get_recipe_draft gives, the revision you read it at, and a reason. " +
			"What you change is marked for the person reviewing it. It is refused if the draft changed since you read it. The draft stays a draft: nothing is saved until a person confirms it.",
		Annotations: drafts,
	}, t.reviseRecipeDraft)
}

// draftRefusal is the refusal for a draft error the agent can act on, or nil.
func draftRefusal(err error) error {
	switch {
	case errors.Is(err, apirecipes.ErrDraftGone):
		return refuse("no pending draft has that token: it was saved, discarded or has expired, or it is an edit proposal, which get_recipe_proposal reads")
	case errors.Is(err, apirecipes.ErrDraftChanged):
		return refuse("the draft changed since you read it, perhaps edited by a person on the review screen; read it again with get_recipe_draft and revise what you get")
	case errors.Is(err, apirecipes.ErrInvalidRevision):
		return refuse("%s", err)
	}
	return nil
}

func (t *tools) getRecipeDraft(ctx context.Context, _ *mcp.CallToolRequest, in getDraftIn) (*mcp.CallToolResult, getDraftOut, error) {
	const tool = "get_recipe_draft"
	rc, err := recipeCaller(ctx)
	if err != nil {
		return nil, getDraftOut{}, t.toolError(ctx, tool, err)
	}
	token := strings.TrimSpace(in.Token)
	if token == "" {
		return nil, getDraftOut{}, refuse("give the draft's token")
	}
	d, err := t.Recipes.GetDraft(ctx, rc, token)
	if err != nil {
		if r := draftRefusal(err); r != nil {
			return nil, getDraftOut{}, r
		}
		return nil, getDraftOut{}, t.toolError(ctx, tool, err)
	}
	return nil, draftOutOf(token, d), nil
}

// draftOutOf is d as get_recipe_draft gives it.
func draftOutOf(token string, d apiingest.Draft) getDraftOut {
	out := getDraftOut{
		Token: token, Revision: d.Revision, ReviewPath: ReviewPath + token, SourceURL: d.Source.URL,
		ParserVersion: d.ParserVersion, Method: string(d.Method), UsedLLM: d.UsedLLM, Reason: d.Reason,
		Recipe: draftRecipeOf(d), Note: draftNote,
	}
	if d.Reimport != nil {
		out.ReimportOf = d.Reimport.RecipeID
	}
	return out
}

func textOf(f apiingest.Field[string]) draftText {
	return draftText{Value: f.Value, Confidence: f.Confidence, FromLLM: f.FromLLM}
}

func minutesOf(f apiingest.Field[time.Duration]) draftNumber {
	return draftNumber{Value: int(f.Value.Minutes()), Confidence: f.Confidence, FromLLM: f.FromLLM}
}

func draftRecipeOf(d apiingest.Draft) draftRecipe {
	r := draftRecipe{
		Name: textOf(d.Name), Author: textOf(d.Author), Description: textOf(d.Description),
		Yield:        draftText{Value: yieldText(d.Yield.Value), Confidence: d.Yield.Confidence, FromLLM: d.Yield.FromLLM},
		Servings:     draftText{Value: d.Servings.Value.String(), Confidence: d.Servings.Confidence, FromLLM: d.Servings.FromLLM},
		Calories:     draftNumber{Value: d.Calories.Value, Confidence: d.Calories.Confidence, FromLLM: d.Calories.FromLLM},
		PrepMinutes:  minutesOf(d.Prep),
		CookMinutes:  minutesOf(d.Cook),
		TotalMinutes: minutesOf(d.Total),
		MealType:     string(d.MealType), Cuisine: string(d.Cuisine),
		Components: []draftComponent{},
	}
	for _, c := range d.Components {
		dc := draftComponent{Name: c.Name, Lines: []draftLine{}, Steps: []draftStep{}}
		for _, l := range c.Lines {
			dl := draftLine{
				Text: l.Raw, IngredientID: int64(l.Ingredient), Optional: l.Optional, Name: l.Name,
				Quantity: l.Amount.Qty.String(), Unit: l.Amount.Unit.Name, ToTaste: l.ToTaste,
				Confidence: l.Confidence, Issues: l.Issues, FromLLM: l.FromLLM,
			}
			for _, q := range l.Qualifiers {
				dl.Qualifiers = append(dl.Qualifiers, q.Text)
			}
			dc.Lines = append(dc.Lines, dl)
		}
		for _, s := range c.Steps {
			dc.Steps = append(dc.Steps, draftStep{
				Text: s.Text, Title: s.Title, Kind: string(s.Kind), Minutes: int(s.Duration.Minutes()), Passive: s.Passive,
				DependsOn: s.DependsOn, Confidence: s.Confidence, FromLLM: s.FromLLM, GraphFromLLM: s.GraphFromLLM,
			})
		}
		r.Components = append(r.Components, dc)
	}
	for _, n := range d.AuthorNotes {
		r.AuthorNotes = append(r.AuthorNotes, draftAuthorNote{Title: n.Title, Text: n.Text, Confidence: n.Confidence, Issues: n.Issues})
	}
	for _, e := range d.Equipment {
		r.Equipment = append(r.Equipment, draftEquipment{Name: e.Name, EquipmentID: int64(e.Equipment), FromLLM: e.FromLLM})
	}
	return r
}

func (t *tools) listRecipeDrafts(ctx context.Context, _ *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, Page[draftSummaryOut], error) {
	const tool = "list_recipe_drafts"
	rc, err := recipeCaller(ctx)
	if err != nil {
		return nil, Page[draftSummaryOut]{}, t.toolError(ctx, tool, err)
	}
	found, err := t.Recipes.PendingDrafts(ctx, rc)
	if err != nil {
		return nil, Page[draftSummaryOut]{}, t.toolError(ctx, tool, err)
	}
	all := make([]draftSummaryOut, len(found))
	for i, f := range found {
		all[i] = draftSummaryOut{
			Token: f.Token, Name: f.Name, SourceURL: f.SourceURL, CreatedAt: f.CreatedAt.UTC().Format(time.RFC3339),
			Reimport: f.Reimport, Revised: f.Reason != "", ReviewPath: ReviewPath + f.Token,
		}
	}
	p, err := paginate(all, in.Cursor, in.Limit)
	if err != nil {
		return nil, Page[draftSummaryOut]{}, refuse("%s", err)
	}
	return nil, p, nil
}

func (t *tools) reviseRecipeDraft(ctx context.Context, _ *mcp.CallToolRequest, in reviseDraftIn) (*mcp.CallToolResult, reviseDraftOut, error) {
	const tool = "revise_recipe_draft"
	c, h, err := scoped(ctx)
	if err != nil {
		return nil, reviseDraftOut{}, t.toolError(ctx, tool, err)
	}
	rc := callerOf(c, h)
	token, revision, reason := strings.TrimSpace(in.Token), strings.TrimSpace(in.Revision), oneLine(in.Reason)
	switch {
	case token == "" || revision == "":
		return nil, reviseDraftOut{}, refuse("give the draft's token and the revision get_recipe_draft gave")
	case reason == "":
		return nil, reviseDraftOut{}, refuse("give a reason: the person reviewing the draft is shown it")
	case utf8.RuneCountInString(reason) > maxReason:
		return nil, reviseDraftOut{}, refuse("the reason is too long: at most %d characters", maxReason)
	}
	if size, err := json.Marshal(in.Recipe); err != nil || len(size) > maxDraftText {
		return nil, reviseDraftOut{}, refuse("the recipe is too long: at most %d bytes", maxDraftText)
	}
	// The held draft is read for the values the agent was shown, so a value
	// sent back as it was shown (minutes rounded down, a yield as text) is the
	// held value. The revision is checked again where the draft is written.
	held, err := t.Recipes.GetDraft(ctx, rc, token)
	if err == nil && held.Revision != revision {
		err = apirecipes.ErrDraftChanged
	}
	if err != nil {
		if r := draftRefusal(err); r != nil {
			return nil, reviseDraftOut{}, r
		}
		return nil, reviseDraftOut{}, t.toolError(ctx, tool, err)
	}
	rev, err := revisionOf(held, in.Recipe)
	if err != nil {
		return nil, reviseDraftOut{}, err
	}
	next, err := t.Recipes.ReviseDraft(ctx, rc, token, revision, rev, reason)
	if err != nil {
		if r := draftRefusal(err); r != nil {
			return nil, reviseDraftOut{}, r
		}
		return nil, reviseDraftOut{}, t.toolError(ctx, tool, err)
	}
	if err := t.recordWrite(ctx, c, h.ID, tool, "draft "+token[:min(8, len(token))]); err != nil {
		return nil, reviseDraftOut{}, t.toolError(ctx, tool, err)
	}
	return nil, reviseDraftOut{Token: token, Revision: next.Revision, ReviewPath: ReviewPath + token, Note: reviseNote}, nil
}

// revisionOf reads the agent's recipe into a revision of held. Minutes equal
// to the whole minutes held shows keep held's duration, which may have seconds
// in it.
func revisionOf(held apiingest.Draft, in draftRecipe) (apirecipes.DraftRevision, error) {
	minutes := func(heldDur time.Duration, m int) time.Duration {
		if m == int(heldDur.Minutes()) {
			return heldDur
		}
		return time.Duration(m) * time.Minute
	}
	r := apirecipes.DraftRevision{
		Name: oneLine(in.Name.Value), Author: oneLine(in.Author.Value), Description: strings.TrimSpace(in.Description.Value),
		Calories: in.Calories.Value,
		MealType: recipe.MealType(strings.TrimSpace(in.MealType)), Cuisine: recipe.Cuisine(strings.TrimSpace(in.Cuisine)),
	}
	for _, m := range []int{in.PrepMinutes.Value, in.CookMinutes.Value, in.TotalMinutes.Value} {
		if m < 0 {
			return apirecipes.DraftRevision{}, refuse("times cannot be negative")
		}
	}
	r.Prep = minutes(held.Prep.Value, in.PrepMinutes.Value)
	r.Cook = minutes(held.Cook.Value, in.CookMinutes.Value)
	r.Total = minutes(held.Total.Value, in.TotalMinutes.Value)
	var err error
	if r.Yield, err = editYield(held.Yield.Value, in.Yield.Value); err != nil {
		return apirecipes.DraftRevision{}, err
	}
	if r.Servings, err = editServings(held.Servings.Value, in.Servings.Value); err != nil {
		return apirecipes.DraftRevision{}, err
	}
	heldSteps := map[string][]time.Duration{}
	for _, c := range held.Components {
		for _, s := range c.Steps {
			heldSteps[s.Text] = append(heldSteps[s.Text], s.Duration)
		}
	}
	for _, c := range in.Components {
		rc := apirecipes.RevisedComponent{Name: c.Name}
		for _, l := range c.Lines {
			rc.Lines = append(rc.Lines, apirecipes.RevisedLine{Text: l.Text, Ingredient: recipe.IngredientID(l.IngredientID), Optional: l.Optional})
		}
		for _, s := range c.Steps {
			if s.Minutes < 0 {
				return apirecipes.DraftRevision{}, refuse("a step's minutes cannot be negative")
			}
			d := time.Duration(s.Minutes) * time.Minute
			for _, hd := range heldSteps[strings.TrimSpace(s.Text)] {
				if int(hd.Minutes()) == s.Minutes {
					d = hd
					break
				}
			}
			rc.Steps = append(rc.Steps, apirecipes.RevisedStep{
				Text: s.Text, Title: s.Title, Kind: recipe.StageKind(strings.TrimSpace(s.Kind)), Duration: d, Passive: s.Passive, DependsOn: s.DependsOn,
			})
		}
		r.Components = append(r.Components, rc)
	}
	for _, n := range in.AuthorNotes {
		r.AuthorNotes = append(r.AuthorNotes, apirecipes.RevisedNote{Title: n.Title, Text: n.Text})
	}
	for _, e := range in.Equipment {
		r.Equipment = append(r.Equipment, apirecipes.RevisedEquipment{Name: e.Name, Equipment: recipe.EquipmentID(e.EquipmentID)})
	}
	return r, nil
}
