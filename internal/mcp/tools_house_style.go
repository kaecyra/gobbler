package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kaecyra/gobbler/internal/api/caller"
)

// HouseStyles reads a household's house style (ADR-00048).
// *households.Service satisfies it. There is no write: no MCP tool changes the
// house style, which members edit on the household page.
type HouseStyles interface {
	HouseStyle(ctx context.Context, c caller.Caller) (string, error)
}

type houseStyleOut struct {
	// Style is the household's house style, plain text; empty when none has
	// been written.
	Style string `json:"style"`
}

func (t *tools) registerHouseStyle(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_house_style",
		Description: "Read the household's house style: plain-text rules for how the household writes its recipes. Follow it when you revise a draft or propose a recipe edit. Empty when the household has written none. Members edit it on the household page; no tool changes it.",
		Annotations: readOnly,
	}, t.getHouseStyle)
}

func (t *tools) getHouseStyle(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, houseStyleOut, error) {
	c, err := recipeCaller(ctx)
	if err != nil {
		return nil, houseStyleOut{}, t.toolError(ctx, "get_house_style", err)
	}
	style, err := t.HouseStyle.HouseStyle(ctx, c)
	if err != nil {
		return nil, houseStyleOut{}, t.toolError(ctx, "get_house_style", err)
	}
	return nil, houseStyleOut{Style: style}, nil
}
