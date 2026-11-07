package layout

import (
	"context"
	"fmt"
	"io"

	"github.com/a-h/templ"
)

// The app's one set of line icons lives here, and every page draws from it
// through Icon. Each is drawn on the mockup's 24px grid in currentColor
// (docs/ui/design.md), so the caller's text colour and CSS size it. They are
// decoration, hidden from screen readers: the text beside each one, or an
// aria-label on its wrapper, is the accessible name.
//
// Pictures with a meaning of their own stay with their owners: the meal-type
// and equipment glyphs (MealIcon, EquipIcon, ADR-00024), the filled chili and
// spoon of the recipe scales, the maple leaf and the identicon avatar.

// IconName is an icon in the set.
type IconName string

// The icons in the set.
const (
	IconAlert    IconName = "alert"
	IconBack     IconName = "back"
	IconBasket   IconName = "basket"
	IconCart     IconName = "cart"
	IconBulb     IconName = "bulb"
	IconCheck    IconName = "check"
	IconChevron  IconName = "chevron"
	IconClock    IconName = "clock"
	IconClose    IconName = "close"
	IconCode     IconName = "code"
	IconDown     IconName = "down"
	IconExternal IconName = "external"
	IconGear     IconName = "gear"
	IconGrip     IconName = "grip"
	IconHand     IconName = "hand"
	IconHome     IconName = "home"
	IconInfo     IconName = "info"
	IconLibrary  IconName = "library"
	IconMail     IconName = "mail"
	IconMenu     IconName = "menu"
	IconMoon     IconName = "moon"
	IconMore     IconName = "more"
	IconNone     IconName = "none"
	IconPencil   IconName = "pencil"
	IconPlus     IconName = "plus"
	IconPot      IconName = "pot"
	IconRetry    IconName = "retry"
	IconSearch   IconName = "search"
	IconShare    IconName = "share"
	IconSignIn   IconName = "signin"
	IconSignOut  IconName = "signout"
	IconTag      IconName = "tag"
	IconUp       IconName = "up"
	IconUser     IconName = "user"
)

// lineGlyph is one drawing. Most share the default line; a few were drawn heavier,
// at a fixed size, or with square ends where they sit, and keep that look.
type lineGlyph struct {
	body string
	// stroke is the line width, "1.75" when empty.
	stroke string
	// size is the width and height in pixels for an icon its CSS does not size.
	size int
	// square drops the round line ends and joins.
	square bool
}

var lineGlyphs = map[IconName]lineGlyph{
	IconAlert:    {body: `<path d="M12 3.5 21.5 20h-19zM12 10v4.5M12 17.5v.01"></path>`},
	IconBack:     {body: `<path d="M15 5l-7 7 7 7"></path>`, stroke: "2"},
	IconCart:     {body: `<path d="M2.5 4h2.6l2.4 10.5h10.2l2.3-7.5H6"></path><circle cx="9" cy="19" r="1.4"></circle><circle cx="17" cy="19" r="1.4"></circle>`},
	IconBasket:   {body: `<path d="M3 10h18l-2 10H5zM8 10l3-6M16 10l-3-6M9 14v3M12 14v3M15 14v3"></path>`},
	IconBulb:     {body: `<path d="M9 18h6M10 21h4M12 3a6 6 0 0 0-3.5 10.9c.6.5 1 1.2 1 2.1h5c0-.9.4-1.6 1-2.1A6 6 0 0 0 12 3z"></path>`},
	IconCheck:    {body: `<path d="M5 13l4.4 4.4L19 7.6"></path>`, stroke: "4.4"},
	IconChevron:  {body: `<path d="M6 9l6 6 6-6"></path>`, stroke: "1.8"},
	IconClock:    {body: `<circle cx="12" cy="12" r="8.5"></circle><path d="M12 7.5V12l3 2"></path>`},
	IconClose:    {body: `<path d="M6 6l12 12M18 6 6 18"></path>`, stroke: "1.9"},
	IconCode:     {body: `<path d="M16 18l6-6-6-6M8 6l-6 6 6 6"></path>`},
	IconDown:     {body: `<path d="M12 5v14M6 13l6 6 6-6"></path>`, stroke: "1.8"},
	IconExternal: {body: `<path d="M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"></path>`, stroke: "2", size: 15},
	IconGear:     {body: `<circle cx="12" cy="12" r="3"></circle><path d="M12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M18.4 5.6l-2.1 2.1M7.7 16.3l-2.1 2.1"></path>`},
	IconGrip:     {body: `<path d="M9 6h.01M15 6h.01M9 12h.01M15 12h.01M9 18h.01M15 18h.01"></path>`, stroke: "3"},
	IconHand:     {body: `<path d="M8 13V6a1.5 1.5 0 0 1 3 0v5M11 10.5V4.5a1.5 1.5 0 0 1 3 0v6M14 10.5V6a1.5 1.5 0 0 1 3 0v8a6.5 6.5 0 0 1-6.5 6.5h-.3a6 6 0 0 1-4.7-2.3l-2.3-3a1.5 1.5 0 0 1 2.3-1.9L8 15"></path>`},
	IconHome:     {body: `<path d="M3 11l9-7 9 7M5.5 9.5V20h13V9.5M10 20v-6h4v6"></path>`},
	IconInfo:     {body: `<circle cx="12" cy="12" r="8.5"></circle><path d="M12 11v5.5M12 7.8v.01"></path>`},
	IconLibrary:  {body: `<path d="M4 5h4v15H4zM10 5h4v15h-4zM15.6 6.2l3.4-.9 3 14.2-3.4.9z"></path>`},
	IconMail:     {body: `<rect x="3" y="5" width="18" height="14" rx="2"></rect><path d="M3.5 7l8.5 6 8.5-6"></path>`},
	IconMenu:     {body: `<path d="M4 7h16M4 12h16M4 17h16"></path>`},
	IconMore:     {body: `<circle cx="5" cy="12" r="1"></circle><circle cx="12" cy="12" r="1"></circle><circle cx="19" cy="12" r="1"></circle>`, stroke: "2.5"},
	IconMoon:     {body: `<path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z"></path>`},
	IconNone:     {body: `<rect x="4" y="5" width="16" height="14" rx="2"></rect><path d="M4 19 20 5"></path>`},
	IconPencil:   {body: `<path d="M12 20h9"></path><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z"></path>`, stroke: "2", size: 20},
	IconPlus:     {body: `<rect x="4" y="4" width="16" height="16" rx="3"></rect><path d="M12 8.5v7M8.5 12h7"></path>`},
	IconPot:      {body: `<path d="M4 11h16v6a3 3 0 0 1-3 3H7a3 3 0 0 1-3-3zM2 11h2M20 11h2M9 7.5c0-1 1-1.5 1-2.5M14 7.5c0-1 1-1.5 1-2.5"></path>`, stroke: "1.9"},
	IconRetry:    {body: `<path d="M20 11a8 8 0 1 0-2.3 5.7M20 5v6h-6"></path>`, stroke: "1.8"},
	IconSearch:   {body: `<circle cx="11" cy="11" r="7"></circle><path d="m20 20-4-4"></path>`, stroke: "2", size: 20, square: true},
	IconShare:    {body: `<circle cx="6" cy="12" r="2.5"></circle><circle cx="18" cy="6" r="2.5"></circle><circle cx="18" cy="18" r="2.5"></circle><path d="M8.2 10.8l7.6-3.6M8.2 13.2l7.6 3.6"></path>`},
	IconSignIn:   {body: `<path d="M14 4h5v16h-5M3 12h11M10 8l4 4-4 4"></path>`},
	IconSignOut:  {body: `<path d="M10 4H5v16h5M9 12h11M16 8l4 4-4 4"></path>`},
	IconTag:      {body: `<path d="M3 12V4h8l10 10-8 8z"></path><circle cx="7.5" cy="8.5" r="1.2"></circle>`},
	IconUp:       {body: `<path d="M12 19V5M6 11l6-6 6 6"></path>`, stroke: "1.8"},
	IconUser:     {body: `<circle cx="12" cy="8.5" r="3.5"></circle><path d="M5 20c0-3.9 3.1-6.5 7-6.5s7 2.6 7 6.5"></path>`},
}

// Icon draws n. A name outside the set fails the render rather than drawing
// nothing, so a typo shows up as an error and not as a missing picture.
func Icon(n IconName) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		g, ok := lineGlyphs[n]
		if !ok {
			return fmt.Errorf("icon %q is not in the set", n)
		}
		_, err := io.WriteString(w, lineSVG(g))
		return err
	})
}

// lineSVG is the whole drawing. Every part is a constant of this package.
func lineSVG(g lineGlyph) string {
	stroke := g.stroke
	if stroke == "" {
		stroke = "1.75"
	}
	s := `<svg viewBox="0 0 24 24"`
	if g.size > 0 {
		s += fmt.Sprintf(` width="%d" height="%d"`, g.size, g.size)
	}
	s += ` fill="none" stroke="currentColor" stroke-width="` + stroke + `"`
	if !g.square {
		s += ` stroke-linecap="round" stroke-linejoin="round"`
	}
	return s + ` aria-hidden="true">` + g.body + `</svg>`
}
