package layout

// PictureScriptPath is the script that turns a recipe picture that fails to
// load into its placeholder band. It is embedded with the other static assets
// (ADR-00002); a page that draws PictureBand links it with PictureFallback.
const PictureScriptPath = "/static/picture.js"

// BandVariants is the number of placeholder bands app.css draws (.ph0 to .ph5,
// from the mockup). A seventh needs a rule there and this constant changed
// together.
const BandVariants = 6

// BandVariant is the placeholder band of a recipe without a picture: its id
// modulo the size of the set, so the same recipe always shows the same band
// and recipes imported one after another differ (ADR-00039, Showing). It is a
// pure function of the id.
func BandVariant(id int64) int {
	v := int(id % BandVariants)
	if v < 0 {
		v += BandVariants
	}
	return v
}
