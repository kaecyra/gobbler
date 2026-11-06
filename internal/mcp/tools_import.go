package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"

	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/ingest"
)

// maxDraftText bounds the text of one import or draft, the way the web form
// bounds a pasted recipe.
const maxDraftText = 1 << 20

type importIn struct {
	URL  string `json:"url,omitempty" jsonschema:"the web address of a page with a recipe on it; give this or text"`
	Text string `json:"text,omitempty" jsonschema:"a recipe pasted as text; give this or url"`
}

type createDraftIn struct {
	Name        string   `json:"name" jsonschema:"the recipe's title"`
	Ingredients []string `json:"ingredients" jsonschema:"one ingredient line per item, as written in a recipe, such as '2 cups flour'"`
	Steps       []string `json:"steps" jsonschema:"the instructions, one step per item, in order"`
}

type draftOut struct {
	// ReviewPath is the page where a person checks and confirms the draft.
	ReviewPath string `json:"review_path"`
	Name       string `json:"name"`
	// LowConfidence counts the ingredient lines the rules could not read with
	// confidence, which the reviewer will see highlighted.
	LowConfidence int    `json:"low_confidence_lines"`
	Note          string `json:"note"`
}

const draftNote = "This is a draft. Nothing is saved until a person opens the review path in gobbler and confirms it."

func (t *tools) registerImport(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "import_recipe",
		Description: "Read a recipe from a web page address or from pasted text, and leave a draft for a person to review. Nothing is saved until they confirm it.",
		Annotations: drafts,
	}, t.importRecipe)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_recipe_draft",
		Description: "Write a new recipe from a title, ingredient lines and steps, and leave a draft for a person to review. Nothing is saved until they confirm it.",
		Annotations: drafts,
	}, t.createRecipeDraft)
}

func (t *tools) importRecipe(ctx context.Context, _ *mcp.CallToolRequest, in importIn) (*mcp.CallToolResult, draftOut, error) {
	const tool = "import_recipe"
	c, h, err := scoped(ctx)
	if err != nil {
		return nil, draftOut{}, t.toolError(ctx, tool, err)
	}
	rawURL, text := strings.TrimSpace(in.URL), strings.TrimSpace(in.Text)
	var d ingest.Draft
	switch {
	case (rawURL == "") == (text == ""):
		return nil, draftOut{}, refuse("give either url or text, not both and not neither")
	case rawURL != "":
		if u, perr := url.Parse(rawURL); perr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, draftOut{}, refuse("url must be an http or https address")
		}
		if err := t.checkRoom(ctx, callerOf(c, h), tool); err != nil {
			return nil, draftOut{}, err
		}
		d, err = t.Drafter.FromURL(ctx, rawURL)
	default:
		if len(text) > maxDraftText {
			return nil, draftOut{}, refuse("the text is too long: at most %d bytes", maxDraftText)
		}
		if err := t.checkRoom(ctx, callerOf(c, h), tool); err != nil {
			return nil, draftOut{}, err
		}
		d, err = t.Drafter.FromText(ctx, text)
	}
	if err != nil {
		return nil, draftOut{}, t.toolError(ctx, tool, err)
	}
	return t.hold(ctx, tool, c, h, d)
}

func (t *tools) createRecipeDraft(ctx context.Context, _ *mcp.CallToolRequest, in createDraftIn) (*mcp.CallToolResult, draftOut, error) {
	const tool = "create_recipe_draft"
	c, h, err := scoped(ctx)
	if err != nil {
		return nil, draftOut{}, t.toolError(ctx, tool, err)
	}
	name := strings.TrimSpace(in.Name)
	name = oneLine(name)
	if name == "" || len(in.Ingredients) == 0 || len(in.Steps) == 0 {
		return nil, draftOut{}, refuse("a recipe draft needs a name, at least one ingredient and at least one step")
	}
	// The same rules read an agent's recipe as read a pasted one: the draft is
	// composed as text and goes through the rule-first pipeline (ADR-00006).
	var b strings.Builder
	b.WriteString(name + "\n\nIngredients\n")
	for _, l := range in.Ingredients {
		b.WriteString("- " + oneLine(l) + "\n")
	}
	b.WriteString("\nInstructions\n")
	for i, s := range in.Steps {
		fmt.Fprintf(&b, "%d. %s\n", i+1, oneLine(s))
	}
	if b.Len() > maxDraftText {
		return nil, draftOut{}, refuse("the recipe is too long: at most %d bytes", maxDraftText)
	}
	if err := t.checkRoom(ctx, callerOf(c, h), tool); err != nil {
		return nil, draftOut{}, err
	}
	d, err := t.Drafter.FromText(ctx, b.String())
	if err != nil {
		return nil, draftOut{}, t.toolError(ctx, tool, err)
	}
	d.Name = ingest.Field[string]{Value: name, Confidence: 1}
	return t.hold(ctx, tool, c, h, d)
}

// checkRoom refuses, with the message hold gives, a household that already holds
// the most drafts it may, before anything is fetched or read for a draft that
// would be turned away. hold's check where the draft is written stays the
// authority.
func (t *tools) checkRoom(ctx context.Context, rc apirecipes.Caller, tool string) error {
	full, err := t.Recipes.DraftsFull(ctx, rc)
	if err != nil {
		return t.toolError(ctx, tool, err)
	}
	if full {
		return refuse("%s", apirecipes.ErrDraftLimit)
	}
	return nil
}

// hold puts the draft where the review screen finds it and records the write.
func (t *tools) hold(ctx context.Context, tool string, c Credential, h household.Household, d ingest.Draft) (*mcp.CallToolResult, draftOut, error) {
	token, err := t.Recipes.AddDraft(ctx, callerOf(c, h), d, provenance(c, h.ID, tool))
	if errors.Is(err, apirecipes.ErrDraftLimit) {
		// Say so as it is: the agent can tell its user to review the drafts.
		return nil, draftOut{}, refuse("%s", apirecipes.ErrDraftLimit)
	}
	if err != nil {
		return nil, draftOut{}, t.toolError(ctx, tool, err)
	}
	if err := t.recordWrite(ctx, c, h.ID, tool, "draft "+token[:8]); err != nil {
		return nil, draftOut{}, t.toolError(ctx, tool, err)
	}
	low := 0
	for _, comp := range d.Components {
		for _, l := range comp.Lines {
			if l.Confidence < 1 {
				low++
			}
		}
	}
	return nil, draftOut{ReviewPath: ReviewPath + token, Name: d.Name.Value, LowConfidence: low, Note: draftNote}, nil
}

// oneLine collapses every run of whitespace, newlines included, to one space,
// so an item the agent supplied stays one line of the text it is composed into:
// it cannot become several lines, nor a section heading of its own.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
