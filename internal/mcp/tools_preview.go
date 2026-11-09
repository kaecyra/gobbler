package mcp

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	apiingest "github.com/kaecyra/gobbler/internal/api/ingest"
)

// previewIn names what a preview reads: a page, or a saved recipe's original
// page (ADR-00048).
type previewIn struct {
	URL      string `json:"url,omitempty" jsonschema:"the web address of a page with a recipe on it; give this or recipe_id"`
	RecipeID int64  `json:"recipe_id,omitempty" jsonschema:"a saved recipe's id; its original imported page is read again, with no fetch. Give this or url"`
}

// previewField is one value of the draft with the confidence the rules read it
// with, 0 to 1, and whether the LLM supplied it.
type previewField[T any] struct {
	Value      T       `json:"value"`
	Confidence float64 `json:"confidence,omitempty" jsonschema:"0 to 1; ignored by revise_recipe_draft"`
	FromLLM    bool    `json:"from_llm,omitempty" jsonschema:"an LLM or an agent supplied the value; ignored by revise_recipe_draft"`
}

type previewQualifier struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type previewLine struct {
	Raw          string             `json:"raw" jsonschema:"the line as written, such as '2 cups flour, sifted'; revise_recipe_draft reads a changed line afresh from it"`
	Name         string             `json:"name,omitempty"`
	IngredientID int64              `json:"ingredient_id,omitempty" jsonschema:"the catalog ingredient, when the text names none the catalog knows"`
	Amount       string             `json:"amount,omitempty"`
	ToTaste      bool               `json:"to_taste,omitempty"`
	Optional     bool               `json:"optional,omitempty"`
	Qualifiers   []previewQualifier `json:"qualifiers,omitempty"`
	Confidence   float64            `json:"confidence,omitempty"`
	Issues       []string           `json:"issues,omitempty"`
	FromLLM      bool               `json:"from_llm,omitempty"`
}

type previewStep struct {
	// Number counts steps from 1 in reading order through every component;
	// DependsOn names steps by it.
	Number       int     `json:"number,omitempty" jsonschema:"the step's number, from 1 in reading order across every component; depends_on names steps by it"`
	Title        string  `json:"title,omitempty"`
	Text         string  `json:"text"`
	Minutes      int     `json:"minutes,omitempty"`
	Passive      bool    `json:"passive,omitempty"`
	DependsOn    []int   `json:"depends_on,omitempty"`
	Kind         string  `json:"kind,omitempty"`
	Confidence   float64 `json:"confidence,omitempty"`
	FromLLM      bool    `json:"from_llm,omitempty"`
	GraphFromLLM bool    `json:"graph_from_llm,omitempty"`
}

type previewComponent struct {
	Name    string        `json:"name,omitempty"`
	Lines   []previewLine `json:"lines"`
	Steps   []previewStep `json:"steps"`
	FromLLM bool          `json:"from_llm,omitempty"`
}

type previewAuthorNote struct {
	Title      string   `json:"title,omitempty"`
	Text       string   `json:"text"`
	Confidence float64  `json:"confidence,omitempty"`
	Issues     []string `json:"issues,omitempty"`
	FromLLM    bool     `json:"from_llm,omitempty"`
}

type previewEquipment struct {
	Name        string `json:"name"`
	EquipmentID int64  `json:"equipment_id,omitempty"`
	FromLLM     bool   `json:"from_llm,omitempty"`
}

// previewOut is a draft as the review screen would receive it: what
// preview_import returns, and the draft get_recipe_draft returns and
// revise_recipe_draft takes back (ADR-00048).
type previewOut struct {
	SourceURL   string               `json:"source_url,omitempty"`
	Name        previewField[string] `json:"name"`
	Author      previewField[string] `json:"author"`
	Description previewField[string] `json:"description"`
	Yield       previewField[string] `json:"yield"`
	Servings    previewField[string] `json:"servings"`
	Calories    previewField[int]    `json:"calories"`
	PrepMinutes previewField[int]    `json:"prep_minutes"`
	CookMinutes previewField[int]    `json:"cook_minutes"`
	// TotalMinutes is the time the source states; zero when it states none.
	TotalMinutes previewField[int]   `json:"total_minutes"`
	MealType     string              `json:"meal_type,omitempty"`
	MealTypeLLM  bool                `json:"meal_type_from_llm,omitempty" jsonschema:"an LLM or an agent supplied the meal type"`
	Cuisine      string              `json:"cuisine,omitempty"`
	CuisineLLM   bool                `json:"cuisine_from_llm,omitempty" jsonschema:"an LLM or an agent supplied the cuisine"`
	VideoID      string              `json:"video_id,omitempty" jsonschema:"ignored by revise_recipe_draft"`
	Components   []previewComponent  `json:"components"`
	AuthorNotes  []previewAuthorNote `json:"author_notes,omitempty"`
	Equipment    []previewEquipment  `json:"equipment,omitempty"`
	// Method is how the recipe was found on the page: jsonld, microdata, site
	// (a site extractor), text (sectioning) or none.
	Method string `json:"parse_method"`
	// ParserVersion is the parser that made this draft, the current one.
	ParserVersion string `json:"parser_version"`
	// SnapshotParserVersion is the parser version stored with the saved
	// recipe's original page, for a recipe_id preview.
	SnapshotParserVersion string   `json:"snapshot_parser_version,omitempty"`
	Overall               float64  `json:"overall_confidence"`
	Threshold             float64  `json:"threshold"`
	BelowThreshold        bool     `json:"below_threshold,omitempty"`
	LLMAsked              bool     `json:"llm_asked"`
	LLMError              string   `json:"llm_error,omitempty"`
	Issues                []string `json:"issues,omitempty"`
	Note                  string   `json:"note"`
}

const previewNote = "This is a preview. Nothing was saved or held, and no draft is waiting for review. Use import_recipe to leave a draft for a person."

func (t *tools) registerPreview(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "preview_import",
		Description: "Show what importing a recipe would produce, without saving or holding anything. Give a web page address, " +
			"or a saved recipe's id to read its original imported page again with no fetch, to compare with the saved recipe from get_recipe. " +
			"Every field carries the confidence it was read with and whether the LLM supplied it.",
		Annotations: readOnly,
	}, t.previewImport)
}

func (t *tools) previewImport(ctx context.Context, _ *mcp.CallToolRequest, in previewIn) (*mcp.CallToolResult, previewOut, error) {
	const tool = "preview_import"
	rawURL := strings.TrimSpace(in.URL)
	switch {
	case (rawURL == "") == (in.RecipeID == 0):
		return nil, previewOut{}, refuse("give either url or recipe_id, not both and not neither")
	case in.RecipeID < 0:
		return nil, previewOut{}, refuse("give the recipe's id")
	case rawURL != "":
		if err := checkPageURL(rawURL); err != nil {
			return nil, previewOut{}, err
		}
	}
	p, err := t.Previewer.Preview(ctx, apiingest.PreviewSource{URL: rawURL, RecipeID: in.RecipeID})
	if errors.Is(err, apiingest.ErrNoPageSnapshot) {
		return nil, previewOut{}, refuse("%s", apiingest.ErrNoPageSnapshot)
	}
	if err != nil {
		return nil, previewOut{}, t.toolError(ctx, tool, err)
	}
	out := previewOf(p.Draft)
	out.SnapshotParserVersion = p.SnapshotParserVersion
	return nil, out, nil
}

// previewOf is d in the preview's shape.
func previewOf(d apiingest.Draft) previewOut {
	out := previewOut{
		SourceURL:    d.Source.URL,
		Name:         field(d.Name, d.Name.Value),
		Author:       field(d.Author, d.Author.Value),
		Description:  field(d.Description, d.Description.Value),
		Yield:        field(d.Yield, yieldText(d.Yield.Value)),
		Servings:     field(d.Servings, d.Servings.Value.String()),
		Calories:     field(d.Calories, d.Calories.Value),
		PrepMinutes:  field(d.Prep, int(d.Prep.Value.Minutes())),
		CookMinutes:  field(d.Cook, int(d.Cook.Value.Minutes())),
		TotalMinutes: field(d.Total, int(d.Total.Value.Minutes())),
		MealType:     string(d.MealType), Cuisine: string(d.Cuisine), VideoID: d.VideoID,
		MealTypeLLM: d.MealTypeFromLLM, CuisineLLM: d.CuisineFromLLM,
		Components: []previewComponent{},
		Method:     string(d.Method), ParserVersion: d.ParserVersion,
		Overall: d.Overall, Threshold: d.Threshold, BelowThreshold: d.BelowThreshold,
		LLMAsked: d.AskedLLM(), LLMError: d.LLMError, Issues: d.Issues,
		Note: previewNote,
	}
	n := 0
	for _, c := range d.Components {
		co := previewComponent{Name: c.Name, Lines: []previewLine{}, Steps: []previewStep{}, FromLLM: c.FromLLM}
		for _, l := range c.Lines {
			pl := previewLine{
				Raw: l.Raw, Name: l.Name, IngredientID: int64(l.Ingredient), ToTaste: l.ToTaste, Optional: l.Optional,
				Confidence: l.Confidence, Issues: l.Issues, FromLLM: l.FromLLM,
			}
			if !l.ToTaste {
				pl.Amount = l.Amount.String()
			}
			for _, q := range l.Qualifiers {
				pl.Qualifiers = append(pl.Qualifiers, previewQualifier{Kind: string(q.Kind), Text: q.Text})
			}
			co.Lines = append(co.Lines, pl)
		}
		for _, st := range c.Steps {
			n++
			co.Steps = append(co.Steps, previewStep{
				Number: n, Title: st.Title, Text: st.Text, Minutes: int(st.Duration.Minutes()), Passive: st.Passive,
				DependsOn: st.DependsOn, Kind: string(st.Kind), Confidence: st.Confidence, FromLLM: st.FromLLM, GraphFromLLM: st.GraphFromLLM,
			})
		}
		out.Components = append(out.Components, co)
	}
	for _, an := range d.AuthorNotes {
		out.AuthorNotes = append(out.AuthorNotes, previewAuthorNote{Title: an.Title, Text: an.Text, Confidence: an.Confidence, Issues: an.Issues, FromLLM: an.FromLLM})
	}
	for _, e := range d.Equipment {
		out.Equipment = append(out.Equipment, previewEquipment{Name: e.Name, EquipmentID: int64(e.Equipment), FromLLM: e.FromLLM})
	}
	return out
}

// field is f's confidence and LLM mark with v, f's value in the preview's form.
func field[T, V any](f apiingest.Field[T], v V) previewField[V] {
	return previewField[V]{Value: v, Confidence: f.Confidence, FromLLM: f.FromLLM}
}
