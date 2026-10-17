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
- A rule-based reading of the source may be supplied. It can be wrong or incomplete; the source is authoritative.`

// userBlocks builds the user turn: the image when there is one, then the source
// text and the rule-based draft as context.
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
	text.WriteString(describeDraft(req.RuleDraft))
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
		},
		Required:    []string{"name", "components"},
		ExtraFields: map[string]any{"additionalProperties": false},
	}
}
