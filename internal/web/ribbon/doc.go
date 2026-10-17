// Package ribbon renders a recipe's time ribbon (docs/ui/design.md, Time
// ribbon): one segment per timed step, an icon and a time in each, soft
// translucent fill with a same-hue border.
//
// The server computes everything: Build turns steps into segments at two merge
// levels, and the templ components print both. The embedded script
// (ScriptPath) only picks which level fits the width it is given, so the
// ribbon is complete without it, at the unmerged level.
//
// A page that shows a ribbon links StylesheetPath, and ScriptPath if it wants
// the fit.
package ribbon

// Paths of the ribbon's own assets, embedded in the binary (ADR-00002).
const (
	StylesheetPath = "/static/ribbon.css"
	ScriptPath     = "/static/ribbon.js"
)
