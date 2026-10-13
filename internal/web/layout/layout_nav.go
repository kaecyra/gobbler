package layout

import (
	"context"
	"strings"

	"github.com/go-chi/chi/v5"
)

// navItem is one primary navigation entry. Paths are written out here rather
// than imported from the feature packages, so the layout depends on none of
// them (ADR-00012).
type navItem struct {
	Label string
	Path  string
}

var navItems = []navItem{
	{"Recipes", "/recipes"},
	{"Add recipe", "/ingest"},
	{"Catalog", "/catalog"},
	{"Shopping", "/shopping"},
	{"Household", "/household"},
	{"Invitations", "/operator"},
	{"Settings", "/settings"},
}

// isCurrent reports whether the page being rendered belongs to item's section.
// The section is the first path segment of the outermost chi route pattern the
// request matched, so /recipes/{id}/cook is part of Recipes. A context with no
// chi route (a unit test, a request that matched nothing) marks nothing.
func isCurrent(ctx context.Context, item navItem) bool {
	rctx := chi.RouteContext(ctx)
	if rctx == nil || len(rctx.RoutePatterns) == 0 {
		return false
	}
	section, _, _ := strings.Cut(strings.TrimPrefix(rctx.RoutePatterns[0], "/"), "/")
	return "/"+section == item.Path
}
