package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

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

type getDraftIn struct {
	Token string `json:"token" jsonschema:"the draft's token from list_recipe_drafts, import_recipe or reimport_recipe"`
}

// getDraftOut is a held draft: the draft in preview_import's shape (previewOf),
// with what names and guards it.
type getDraftOut struct {
	Token string `json:"token"`
	// Revision names the draft's content; revise_recipe_draft takes it back
	// and is refused once the draft has changed.
	Revision   string `json:"revision"`
	ReviewPath string `json:"review_path"`
	// ReimportOf is the recipe a re-import draft replaces when it is saved.
	ReimportOf int64 `json:"reimport_of,omitempty"`
	// Reason is what an agent said about the draft: when it made it, or when it
	// last revised it.
	Reason string     `json:"reason,omitempty"`
	Draft  previewOut `json:"draft"`
}

type draftSummaryOut struct {
	Token      string `json:"token"`
	Name       string `json:"name"`
	SourceURL  string `json:"source_url,omitempty"`
	CreatedAt  string `json:"created_at"`
	Reimport   bool   `json:"reimport,omitempty"`
	ReviewPath string `json:"review_path"`
}

type reviseDraftIn struct {
	Token    string       `json:"token" jsonschema:"the draft's token"`
	Revision string       `json:"revision" jsonschema:"the revision get_recipe_draft gave when you read the draft"`
	Draft    revisedDraft `json:"draft" jsonschema:"the whole draft as it should be: the draft get_recipe_draft gave, with your changes. Confidences, marks, step numbers and the parse details may be left out and are ignored; video_id must be sent back unchanged"`
	Reason   string       `json:"reason" jsonschema:"what you changed and why, kept with the draft and returned by get_recipe_draft"`
}

// revisedDraft is the draft revise_recipe_draft takes back: previewOut's
// shape, key for key, read in the other direction. What only the encoder
// fills in (confidences, marks, step numbers, the parse details and the
// note) is optional and ignored, so an agent adding a line or a step need not
// invent it. TestRevisedDraftAcceptsEveryPreviewKey keeps the two in step.
type revisedDraft struct {
	SourceURL    string               `json:"source_url,omitempty" jsonschema:"ignored"`
	Name         revisedField[string] `json:"name"`
	Author       revisedField[string] `json:"author,omitempty"`
	Description  revisedField[string] `json:"description,omitempty"`
	Yield        revisedField[string] `json:"yield,omitempty" jsonschema:"such as '2 loaves'"`
	Servings     revisedField[string] `json:"servings,omitempty" jsonschema:"a number, a fraction or a range such as 4-6"`
	Calories     revisedField[int]    `json:"calories,omitempty" jsonschema:"per serving"`
	PrepMinutes  revisedField[int]    `json:"prep_minutes,omitempty"`
	CookMinutes  revisedField[int]    `json:"cook_minutes,omitempty"`
	TotalMinutes revisedField[int]    `json:"total_minutes,omitempty"`
	MealType     string               `json:"meal_type,omitempty"`
	MealTypeLLM  bool                 `json:"meal_type_from_llm,omitempty" jsonschema:"ignored"`
	Cuisine      string               `json:"cuisine,omitempty"`
	CuisineLLM   bool                 `json:"cuisine_from_llm,omitempty" jsonschema:"ignored"`
	VideoID      string               `json:"video_id,omitempty" jsonschema:"must be the held draft's: a revision that changes it is refused"`
	Components   []revisedComponent   `json:"components"`
	AuthorNotes  []revisedAuthorNote  `json:"author_notes,omitempty"`
	Equipment    []previewEquipment   `json:"equipment,omitempty" jsonschema:"give equipment_id back for equipment you keep, and leave it out for new equipment"`
	Method       string               `json:"parse_method,omitempty" jsonschema:"ignored"`
	ParserVer    string               `json:"parser_version,omitempty" jsonschema:"ignored"`
	SnapshotVer  string               `json:"snapshot_parser_version,omitempty" jsonschema:"ignored"`
	Overall      float64              `json:"overall_confidence,omitempty" jsonschema:"ignored"`
	Threshold    float64              `json:"threshold,omitempty" jsonschema:"ignored"`
	BelowThresh  bool                 `json:"below_threshold,omitempty" jsonschema:"ignored"`
	LLMAsked     bool                 `json:"llm_asked,omitempty" jsonschema:"ignored"`
	LLMError     string               `json:"llm_error,omitempty" jsonschema:"ignored"`
	Issues       []string             `json:"issues,omitempty" jsonschema:"ignored"`
	Note         string               `json:"note,omitempty" jsonschema:"ignored"`
}

// revisedField is previewField read back: only the value counts.
type revisedField[T any] struct {
	Value      T       `json:"value"`
	Confidence float64 `json:"confidence,omitempty" jsonschema:"ignored"`
	FromLLM    bool    `json:"from_llm,omitempty" jsonschema:"ignored"`
}

type revisedLine struct {
	Raw          string             `json:"raw" jsonschema:"the line as written, such as '2 cups flour, sifted'; a changed line is read afresh from it"`
	Name         string             `json:"name,omitempty" jsonschema:"ignored"`
	IngredientID int64              `json:"ingredient_id,omitempty" jsonschema:"the catalog ingredient, when the text names none the catalog knows"`
	Amount       string             `json:"amount,omitempty" jsonschema:"ignored"`
	ToTaste      bool               `json:"to_taste,omitempty" jsonschema:"ignored"`
	Optional     bool               `json:"optional,omitempty"`
	Qualifiers   []previewQualifier `json:"qualifiers,omitempty" jsonschema:"ignored"`
	Confidence   float64            `json:"confidence,omitempty" jsonschema:"ignored"`
	Issues       []string           `json:"issues,omitempty" jsonschema:"ignored"`
	FromLLM      bool               `json:"from_llm,omitempty" jsonschema:"ignored"`
}

type revisedStep struct {
	Number       int     `json:"number,omitempty" jsonschema:"ignored: steps are numbered by their place"`
	Title        string  `json:"title,omitempty"`
	Text         string  `json:"text"`
	Minutes      int     `json:"minutes,omitempty"`
	Passive      bool    `json:"passive,omitempty" jsonschema:"true for a step that needs no attention while it runs"`
	DependsOn    []int   `json:"depends_on,omitempty" jsonschema:"the numbers of the steps that must finish first, counting from 1 in reading order across every component"`
	Kind         string  `json:"kind,omitempty"`
	Confidence   float64 `json:"confidence,omitempty" jsonschema:"ignored"`
	FromLLM      bool    `json:"from_llm,omitempty" jsonschema:"ignored"`
	GraphFromLLM bool    `json:"graph_from_llm,omitempty" jsonschema:"ignored"`
}

type revisedComponent struct {
	Name    string        `json:"name,omitempty"`
	Lines   []revisedLine `json:"lines"`
	Steps   []revisedStep `json:"steps"`
	FromLLM bool          `json:"from_llm,omitempty" jsonschema:"ignored"`
}

type revisedAuthorNote struct {
	Title      string   `json:"title,omitempty"`
	Text       string   `json:"text"`
	Confidence float64  `json:"confidence,omitempty" jsonschema:"ignored"`
	Issues     []string `json:"issues,omitempty" jsonschema:"ignored"`
	FromLLM    bool     `json:"from_llm,omitempty" jsonschema:"ignored"`
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
			"It is the shape preview_import gives, with the draft's revision, which revise_recipe_draft takes back with the draft.",
		Annotations: readOnly,
	}, t.getRecipeDraft)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_recipe_drafts",
		Description: "List the household's recipe drafts waiting for review, re-imports included, oldest first. Returns one page and a cursor for the next.",
		Annotations: readOnly,
	}, t.listRecipeDrafts)
	mcp.AddTool(s, &mcp.Tool{
		Name: "revise_recipe_draft",
		Description: "Replace a pending draft's recipe with your revision of it: send back the draft get_recipe_draft gave, with your changes, the revision you read it at, and a reason. " +
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
	out := getDraftOut{Token: token, Revision: d.Revision, ReviewPath: ReviewPath + token, Reason: d.Reason, Draft: previewOf(d)}
	out.Draft.Note = draftNote
	if d.Reimport != nil {
		out.ReimportOf = d.Reimport.RecipeID
	}
	return out
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
			Reimport: f.Reimport, ReviewPath: ReviewPath + f.Token,
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
	token, revision := strings.TrimSpace(in.Token), strings.TrimSpace(in.Revision)
	reason, err := reasonOf(in.Reason)
	if err != nil {
		return nil, reviseDraftOut{}, err
	}
	switch {
	case token == "" || revision == "":
		return nil, reviseDraftOut{}, refuse("give the draft's token and the revision get_recipe_draft gave")
	case reason == "":
		return nil, reviseDraftOut{}, refuse("give a reason: it is kept with the draft and returned by get_recipe_draft")
	}
	if size, err := json.Marshal(in.Draft); err != nil || len(size) > maxDraftText {
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
	rev, err := revisionOf(held, in.Draft)
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

// revisionOf reads the agent's draft into a revision of held. Minutes equal
// to the whole minutes held shows keep held's duration, which may have seconds
// in it. A revision cannot change the video, so a changed video_id is refused
// rather than dropped.
func revisionOf(held apiingest.Draft, in revisedDraft) (apirecipes.DraftRevision, error) {
	if video := strings.TrimSpace(in.VideoID); video != held.VideoID {
		return apirecipes.DraftRevision{}, refuse("a revision cannot change video_id: send back %q, the held draft's, and a person can change the video on the review screen", held.VideoID)
	}
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
			rc.Lines = append(rc.Lines, apirecipes.RevisedLine{Text: l.Raw, Ingredient: recipe.IngredientID(l.IngredientID), Optional: l.Optional})
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
