package mcp

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/recipepath"
)

// findPicturesIn names the recipe whose source page is searched for pictures.
type findPicturesIn struct {
	RecipeID int64 `json:"recipe_id" jsonschema:"the recipe's id from get_recipe or search_recipes"`
}

type findPicturesOut struct {
	// Found is how many pictures the search found.
	Found int `json:"found"`
	// EditPath is the recipe's edit page, where a person chooses among them.
	EditPath string `json:"edit_path"`
	Note     string `json:"note"`
}

// findPicturesNote tells the agent what happens next. The lifetime is read
// from the recipe service, so the note cannot drift from the sweep.
var findPicturesNote = fmt.Sprintf("The recipe's picture is unchanged. A person chooses among the pictures found, or keeps the current one, on the edit path. They are offered there for %d days or until the recipe is saved, and a new search replaces them.",
	int(apirecipes.OfferLifetime/(24*time.Hour)))

func (t *tools) registerPictures(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "find_recipe_pictures",
		Description: "Search a saved recipe's source page for pictures and offer them on the recipe's edit page, where a person chooses one (ADR-00051). " +
			"It reads the live page, or the saved copy when the page cannot be fetched. It never sets or removes the recipe's picture; no tool does.",
		Annotations: applies,
	}, t.findPictures)
}

func (t *tools) findPictures(ctx context.Context, _ *mcp.CallToolRequest, in findPicturesIn) (*mcp.CallToolResult, findPicturesOut, error) {
	const tool = "find_recipe_pictures"
	c, h, err := scoped(ctx)
	if err != nil {
		return nil, findPicturesOut{}, t.toolError(ctx, tool, err)
	}
	if in.RecipeID <= 0 {
		return nil, findPicturesOut{}, refuse("give the recipe's id")
	}
	found, err := t.Recipes.FindPictures(ctx, callerOf(c, h), in.RecipeID)
	if err != nil {
		return nil, findPicturesOut{}, t.toolError(ctx, tool, err)
	}
	if err := t.recordWrite(ctx, c, h.ID, tool, "pictures of recipe "+strconv.FormatInt(in.RecipeID, 10)); err != nil {
		return nil, findPicturesOut{}, t.toolError(ctx, tool, err)
	}
	return nil, findPicturesOut{Found: len(found), EditPath: recipepath.Base(in.RecipeID) + "/edit", Note: findPicturesNote}, nil
}
