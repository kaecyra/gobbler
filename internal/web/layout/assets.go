package layout

// Asset URLs served from the embedded static directory by package web.
const (
	StylesheetPath = "/static/app.css"
	HTMXPath       = "/static/htmx.min.js"
	// ThemePath is the light/dark toggle. It loads in the head, without defer,
	// so a remembered dark choice is applied before the first paint.
	ThemePath = "/static/theme.js"
)

// Mascot drawings, one embedded SVG each (docs/ui/design.md, Mascots). Pages
// show them with an img tag; they are served cached like every static file.
const (
	MascotHeader     = "/static/img/gob.svg"
	MascotRollingPin = "/static/img/gob-full-rolling-pin.svg"
	MascotMixingBowl = "/static/img/gob-full-mixing-bowl.svg"
	MascotWhisk      = "/static/img/gob-whisk.svg"
	MascotHerbs      = "/static/img/gob-herbs.svg"
	MascotPie        = "/static/img/gob-full-pie.svg"
	MascotTasting    = "/static/img/gob-tasting.svg"
)

// The repository the footer links, as a URL and as the text shown for it.
const (
	RepositoryURL     = "https://github.com/kaecyra/gobbler"
	RepositoryDisplay = "github.com/kaecyra/gobbler"
)
