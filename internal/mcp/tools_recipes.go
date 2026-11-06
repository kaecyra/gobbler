package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipe/display"
)

type searchIn struct {
	Query  string `json:"query" jsonschema:"words to find in a recipe's name, description, ingredients, tags or notes; every word must match"`
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor of the previous page"`
	Limit  int    `json:"limit,omitempty" jsonschema:"page size, at most 50"`
}

type listIn struct {
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor of the previous page"`
	Limit  int    `json:"limit,omitempty" jsonschema:"page size, at most 50"`
}

type recipeSummary struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Rating       *int     `json:"rating,omitempty"`
	PrepMinutes  int      `json:"prep_minutes,omitempty"`
	CookMinutes  int      `json:"cook_minutes,omitempty"`
	TotalMinutes int      `json:"total_minutes,omitempty"`
}

type getRecipeIn struct {
	ID int64 `json:"id" jsonschema:"the recipe's id from search_recipes or list_recipes"`
}

type ingredientLine struct {
	// Key names the line across versions of the recipe; give it back, unchanged,
	// to propose a change to this line.
	Key          string `json:"key"`
	Text         string `json:"text"`
	IngredientID int64  `json:"ingredient_id,omitempty"`
	Optional     bool   `json:"optional,omitempty"`
}

type stepOut struct {
	ID        string   `json:"id"`
	Text      string   `json:"text"`
	Minutes   int      `json:"minutes,omitempty"`
	Passive   bool     `json:"passive,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
}

type componentOut struct {
	Name        string           `json:"name,omitempty"`
	Ingredients []ingredientLine `json:"ingredients"`
	Steps       []stepOut        `json:"steps"`
}

type recipeOut struct {
	ID int64 `json:"id"`
	// Version is the recipe's version; propose_recipe_edit takes it back, so a
	// proposal made against a recipe that has since changed is refused.
	Version     int64          `json:"version"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	SourceURL   string         `json:"source_url,omitempty"`
	Author      string         `json:"author,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Rating      *int           `json:"rating,omitempty"`
	Yield       string         `json:"yield,omitempty"`
	Servings    string         `json:"servings,omitempty"`
	Components  []componentOut `json:"components"`
}

func summaryOf(s apirecipes.Summary) recipeSummary {
	return recipeSummary{
		ID: s.ID, Name: s.Name, Description: s.Description, Tags: s.Tags, Rating: s.Rating,
		PrepMinutes: int(s.Prep.Minutes()), CookMinutes: int(s.Cook.Minutes()), TotalMinutes: int(s.Total.Minutes()),
	}
}

func (t *tools) registerRecipes(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_recipes",
		Description: "Search the household's recipes. Best match first. Returns one page and a cursor for the next.",
		Annotations: readOnly,
	}, t.searchRecipes)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_recipes",
		Description: "List the household's recipes by name. Returns one page and a cursor for the next.",
		Annotations: readOnly,
	}, t.listRecipes)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_recipe",
		Description: "Get one recipe as structured data: its version, and components with their ingredient lines and steps. Lines and steps carry keys, which propose_recipe_edit takes back.",
		Annotations: readOnly,
	}, t.getRecipe)
}

func (t *tools) page(ctx context.Context, tool, query, cursor string, limit int) (*mcp.CallToolResult, Page[recipeSummary], error) {
	rc, err := recipeCaller(ctx)
	if err != nil {
		return nil, Page[recipeSummary]{}, t.toolError(ctx, tool, err)
	}
	found, err := t.Recipes.Search(ctx, rc, apirecipes.SearchQuery{Text: query})
	if err != nil {
		return nil, Page[recipeSummary]{}, t.toolError(ctx, tool, err)
	}
	all := make([]recipeSummary, len(found))
	for i, f := range found {
		all[i] = summaryOf(f)
	}
	p, err := paginate(all, cursor, limit)
	if err != nil {
		return nil, Page[recipeSummary]{}, refuse("%s", err)
	}
	return nil, p, nil
}

func (t *tools) searchRecipes(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, Page[recipeSummary], error) {
	return t.page(ctx, "search_recipes", in.Query, in.Cursor, in.Limit)
}

func (t *tools) listRecipes(ctx context.Context, _ *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, Page[recipeSummary], error) {
	return t.page(ctx, "list_recipes", "", in.Cursor, in.Limit)
}

func (t *tools) getRecipe(ctx context.Context, _ *mcp.CallToolRequest, in getRecipeIn) (*mcp.CallToolResult, recipeOut, error) {
	rc, err := recipeCaller(ctx)
	if err != nil {
		return nil, recipeOut{}, t.toolError(ctx, "get_recipe", err)
	}
	// Info first: a recipe saved between the two reads shows an older version
	// than its content, and an edit made against it is refused as stale, never
	// the reverse.
	info, err := t.Recipes.Info(ctx, rc, in.ID)
	if err != nil {
		return nil, recipeOut{}, t.toolError(ctx, "get_recipe", err)
	}
	r, err := t.Recipes.Get(ctx, rc, in.ID)
	if err != nil {
		return nil, recipeOut{}, t.toolError(ctx, "get_recipe", err)
	}
	out := recipeOut{
		ID: in.ID, Version: info.Version, Name: r.Name, Description: r.Description, SourceURL: r.SourceURL, Author: r.Author,
		Tags: r.Tags, Rating: r.Rating, Yield: yieldText(r.Yield), Servings: r.Servings.String(),
	}
	lines := display.NewLineBuilder(t.Catalog, display.Default())
	for _, c := range r.Components {
		co := componentOut{Name: c.Name, Ingredients: []ingredientLine{}, Steps: []stepOut{}}
		for _, l := range c.Lines {
			built, err := lines.Build(ctx, l)
			if err != nil {
				return nil, recipeOut{}, t.toolError(ctx, "get_recipe", err)
			}
			co.Ingredients = append(co.Ingredients, ingredientLine{Key: l.Key, Text: built.Text, IngredientID: int64(l.Ingredient), Optional: l.Optional})
		}
		for _, st := range c.Steps {
			so := stepOut{ID: string(st.ID), Text: st.Text, Minutes: int(st.Duration.Minutes()), Passive: st.Passive}
			for _, d := range st.DependsOn {
				so.DependsOn = append(so.DependsOn, string(d))
			}
			co.Steps = append(co.Steps, so)
		}
		out.Components = append(out.Components, co)
	}
	return nil, out, nil
}

func yieldText(y recipe.Yield) string {
	q := y.Qty.String()
	switch {
	case q == "" && y.Unit == "":
		return ""
	case y.Unit == "":
		return q
	}
	return q + " " + y.Unit
}
