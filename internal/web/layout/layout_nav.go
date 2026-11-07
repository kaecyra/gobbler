package layout

import (
	"context"
	"strings"

	"github.com/go-chi/chi/v5"
)

// navItem is one navigation entry. Paths are written out here rather than
// imported from the feature packages, so the layout depends on none of them
// (ADR-00012). Icon names a drawing in icon (layout.templ).
type navItem struct {
	Label string
	Path  string
	Icon  IconName
	// CTA marks the one coral action in the nav bar.
	CTA bool
	// Menu puts the entry behind the nav's menu button instead of in the bar.
	Menu bool
}

// navItems lists the sections in display order: the bar first, then the menu.
var navItems = []navItem{
	{Label: "Library", Path: "/recipes", Icon: IconLibrary},
	{Label: "Shopping list", Path: "/shopping", Icon: IconBasket},
	{Label: "Add recipe", Path: "/ingest", Icon: IconPlus, CTA: true},
	{Label: "Catalog", Path: "/catalog", Icon: IconTag, Menu: true},
	{Label: "Shared", Path: "/shares", Icon: IconShare, Menu: true},
	{Label: "Household", Path: "/household", Icon: IconHome, Menu: true},
	{Label: "Operator", Path: "/operator", Icon: IconGear, Menu: true},
}

// profileIcon is the icon of the account menu's link to the person's own
// profile. The link is not a navItem: it is never marked current, since the
// layout cannot tell cheaply whose profile the page shows, and /profile/{address}
// may be someone else's.
const profileIcon = IconUser

// menuCurrent reports whether the page being rendered belongs to a section
// behind the nav's menu button, so the button can say so while it is closed.
func menuCurrent(ctx context.Context) bool {
	for _, item := range navItems {
		if item.Menu && visible(ctx, item) && isCurrent(ctx, item) {
			return true
		}
	}
	return false
}

// hasMenu reports whether any section behind the menu button is visible to the
// request in ctx, so a non-member is not offered an empty menu. Sign out is not
// one: it is in the account menu (ADR-00032).
func hasMenu(ctx context.Context) bool {
	for _, item := range navItems {
		if item.Menu && visible(ctx, item) {
			return true
		}
	}
	return false
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
