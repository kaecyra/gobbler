package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// reimportIn names the recipe to import again from its source.
type reimportIn struct {
	ID int64 `json:"id" jsonschema:"the recipe's id from get_recipe or search_recipes; it needs a source address"`
}

type reimportOut struct {
	// ReviewPath is the page where a person checks the re-read recipe and saves
	// it over the current one, or throws it away.
	ReviewPath string `json:"review_path"`
	Token      string `json:"token"`
	Note       string `json:"note"`
}

const reimportNote = "This is a draft. The recipe and its picture are unchanged until a person opens the review path in gobbler and saves it; saving replaces the recipe and keeps the earlier version in its history. A newer proposal or re-import for the same recipe replaces this one."

func (t *tools) registerReimport(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "reimport_recipe",
		Description: "Read a saved recipe's source page again and leave the result as a draft for a person to review. " +
			"Use it when a recipe was imported before gobbler read pictures or the author's notes. Nothing changes until a person saves the draft; no tool saves it.",
		Annotations: drafts,
	}, t.reimportRecipe)
}

func (t *tools) reimportRecipe(ctx context.Context, _ *mcp.CallToolRequest, in reimportIn) (*mcp.CallToolResult, reimportOut, error) {
	const tool = "reimport_recipe"
	c, h, err := scoped(ctx)
	if err != nil {
		return nil, reimportOut{}, t.toolError(ctx, tool, err)
	}
	if in.ID <= 0 {
		return nil, reimportOut{}, refuse("give the recipe's id")
	}
	token, err := t.reimporter.Start(ctx, in.ID, provenance(c, h.ID, tool))
	if err != nil {
		return nil, reimportOut{}, t.toolError(ctx, tool, err)
	}
	if err := t.recordWrite(ctx, c, h.ID, tool, "re-import "+token[:8]); err != nil {
		return nil, reimportOut{}, t.toolError(ctx, tool, err)
	}
	return nil, reimportOut{ReviewPath: ReviewPath + token, Token: token, Note: reimportNote}, nil
}
