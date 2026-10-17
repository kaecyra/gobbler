package claude

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/kaecyra/gobbler/internal/ingest"
)

const systemPrompt = `You extract recipes from source material. Call the ` + toolName + ` tool exactly once with the recipe.

Rules:
- Copy ingredient lines exactly as written in the source, one per entry, with their quantities and qualifiers. Do not convert units or normalise wording.
- Copy times and yield as written (for example "1 hour 30 minutes", "Serves 4").
- Put each instruction in its own step, in order, without numbering.
- Split into named components only when the source does (for example "For the sauce"); otherwise use a single component with no name.
- List the equipment the recipe uses (for example "Dutch oven", "9-inch springform pan") in the equipment list, one per entry, only when the source names it. Name the equipment alone, without quantities or notes.
- Use only what the source states. Leave a field out rather than guessing.
- A step graph is asked for only when the list of gaps asks for it: then fill step_graph, numbering steps from 1 in reading order across the whole recipe. Otherwise leave step_graph out.
- A rule-based reading of the source may be supplied. It can be wrong or incomplete; the source is authoritative.
- Instead of a reading you may be given a list of gaps. Answer only for those gaps, exactly as the list asks; everything else is already settled and is ignored.`

// userBlocks builds the user turn: the image when there is one, then the source
// text and, as context, the rule-based draft or the gaps to fill.
func userBlocks(req ingest.Request) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion
	var text strings.Builder
	if len(req.Image) > 0 {
		blocks = append(blocks, anthropic.NewImageBlockBase64(req.ImageMIME, base64.StdEncoding.EncodeToString(req.Image)))
		text.WriteString("The recipe is in the attached image.\n\n")
	}
	if req.Text != "" {
		fmt.Fprintf(&text, "Source text:\n%s\n\n", req.Text)
	}
	if req.Gaps.Empty() {
		text.WriteString(describeDraft(req.RuleDraft))
	} else {
		text.WriteString(req.Gaps.Describe())
	}
	text.WriteString("Record the recipe with the " + toolName + " tool.")
	return append(blocks, anthropic.NewTextBlock(text.String()))
}

// describeDraft renders the rule-based reading as plain text. The source
// snapshot is left out: the source text already carries it.
func describeDraft(d ingest.Draft) string {
	if d.Name.Value == "" && len(d.Components) == 0 && len(d.Equipment) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Rule-based reading (may be wrong):\n")
	if d.Name.Value != "" {
		fmt.Fprintf(&b, "Name: %s\n", d.Name.Value)
	}
	for _, e := range d.Equipment {
		fmt.Fprintf(&b, "- equipment: %s\n", e.Name)
	}
	for _, c := range d.Components {
		if c.Name != "" {
			fmt.Fprintf(&b, "Component: %s\n", c.Name)
		}
		for _, l := range c.Lines {
			fmt.Fprintf(&b, "- ingredient: %s\n", l.Raw)
		}
		for _, s := range c.Steps {
			fmt.Fprintf(&b, "- step: %s\n", s.Text)
		}
	}
	for _, issue := range d.Issues {
		fmt.Fprintf(&b, "Known issue: %s\n", issue)
	}
	b.WriteString("\n")
	return b.String()
}

// recipeSchema is the JSON schema of ingest.Structured, as the tool's input.
func recipeSchema() anthropic.ToolInputSchemaParam {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	list := func(desc string) map[string]any {
		return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "string"}}
	}
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"name":        str("Recipe title."),
			"author":      str("Author, if stated."),
			"description": str("Short description, if stated."),
			"yield":       str("Yield or servings as written."),
			"prep_time":   str("Preparation time as written."),
			"cook_time":   str("Cooking time as written."),
			"total_time":  str("Total time as written."),
			"equipment":   list("Equipment the recipe uses, named alone; omit when the source names none."),
			"components": map[string]any{
				"type":        "array",
				"description": "Named parts of the recipe; one unnamed component when the source has no parts.",
				"minItems":    1,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":        str("Component name; omit for the unnamed component."),
						"ingredients": list("Ingredient lines as written."),
						"steps":       list("Instructions in order."),
					},
					"required":             []string{"ingredients"},
					"additionalProperties": false,
				},
			},
			"step_graph": map[string]any{
				"type":        "array",
				"description": "Passive flag and dependencies of the steps the gaps list names; omit otherwise.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"step":       map[string]any{"type": "integer", "minimum": 1, "description": "Step number, from 1 in reading order across the whole recipe."},
						"passive":    map[string]any{"type": "boolean", "description": "True when the cook can leave the step unattended, such as baking or resting."},
						"depends_on": map[string]any{"type": "array", "description": "Numbers of earlier steps that must finish first; empty when the step can start at once.", "items": map[string]any{"type": "integer", "minimum": 1}},
					},
					"required":             []string{"step", "passive", "depends_on"},
					"additionalProperties": false,
				},
			},
		},
		Required:    []string{"name", "components"},
		ExtraFields: map[string]any{"additionalProperties": false},
	}
}
