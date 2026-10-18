package layout

import "strings"

// Equipment icons (ADR-00024) are drawn on the mockup's 24px grid in one line
// style: stroke currentColor, round caps and joins (EquipIcon sets those).
// Each constant is the inner path data of one drawing. Look-alike tools share a
// drawing, so a catalog item is keyed to the nearest recognisable glyph rather
// than every item needing its own; equipIcons maps the catalog name to it.
const (
	glyphGeneric    = `<path d="M7 3v6a2 2 0 0 0 4 0V3M9 3v18M17 3c-2 2-3 5-3 8h3v10"/>`
	glyphPot        = `<path d="M4 11h16v6a3 3 0 0 1-3 3H7a3 3 0 0 1-3-3zM2 11h2M20 11h2M10 8h4"/>`
	glyphSaucepan   = `<path d="M4 9h11v8a3 3 0 0 1-3 3H7a3 3 0 0 1-3-3zM15 11h6M8 6.5c0-1 1-1.5 1-2.5"/>`
	glyphLidded     = `<path d="M4 12h16v5a3 3 0 0 1-3 3H7a3 3 0 0 1-3-3zM6 12a6 5 0 0 1 12 0M12 7V5M2 14h2M20 14h2"/>`
	glyphSlowCook   = `<path d="M4 11h16v5a4 4 0 0 1-4 4H8a4 4 0 0 1-4-4zM7 11a5 4 0 0 1 10 0M12 7V5M9 20v1.5M15 20v1.5"/>`
	glyphPressure   = `<path d="M4 12h16v5a3 3 0 0 1-3 3H7a3 3 0 0 1-3-3zM6 12a6 5 0 0 1 12 0M10 5h4M12 5v2M2 14h2M20 14h2"/>`
	glyphRiceCook   = `<path d="M4 11h16v6a3 3 0 0 1-3 3H7a3 3 0 0 1-3-3zM6 11a6 4 0 0 1 12 0M10 15h4M12 7V5"/>`
	glyphPan        = `<path d="M3 11h13l-1 4a3 3 0 0 1-3 2H7a3 3 0 0 1-3-2zM16 12h5"/>`
	glyphGrillPan   = `<path d="M3 11h13l-1 4a3 3 0 0 1-3 2H7a3 3 0 0 1-3-2zM16 12h5M7 13h6"/>`
	glyphGriddle    = `<path d="M3 11h18l-1 5H4zM3 11l2-3h14l2 3"/>`
	glyphWok        = `<path d="M3 10h16a8 8 0 0 1-16 0zM8 20h6M19 11l3-1"/>`
	glyphSheet      = `<path d="M3 10h18v7H3zM5.5 7h13L21 10H3z"/>`
	glyphDish       = `<path d="M3 9h18v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2zM6 12h12"/>`
	glyphPie        = `<path d="M3 11h18l-2 7H5zM3 11c0-2 4-3 9-3s9 1 9 3"/>`
	glyphTart       = `<path d="M3 13h18M5 13l-1 4h16l-1-4M6 13l1-3h10l1 3"/>`
	glyphBundt      = `<path d="M8 8h8l3 11H5zM9.5 8c0-2 1-3 2.5-3s2.5 1 2.5 3M10 12v7M14 12v7"/>`
	glyphLoaf       = `<path d="M4 8h16l-2 10H6zM2.5 8H4M20 8h1.5M8 12h8"/>`
	glyphMuffin     = `<path d="M3 9h8l-1.2 9H4.2zM13 9h8l-1.2 9h-5.6z"/>`
	glyphCakePan    = `<path d="M4 9v8c0 1.7 3.6 3 8 3s8-1.3 8-3V9M4 9c0-1.7 3.6-3 8-3s8 1.3 8 3-3.6 3-8 3-8-1.3-8-3z"/>`
	glyphRamekin    = `<path d="M5 8h14l-1.5 10h-11zM4 8h16"/>`
	glyphOven       = `<path d="M3 4h18v16H3zM3 8h18M6 6h.01M9 6h.01M6 11h12v6H6z"/>`
	glyphGrill      = `<path d="M4 10h16a8 8 0 0 1-16 0zM8 18l-2 3M16 18l2 3M7 7c0-1 1-1 1-2M12 7c0-1 1-1 1-2M17 7c0-1 1-1 1-2"/>`
	glyphBlender    = `<path d="M7 3h10l-1.5 11h-7zM9 14v3h6v-3M8 17h8v4H8z"/>`
	glyphImmersion  = `<path d="M10 3h4v8h-4zM9 11h6v3H9zM8 14h8v3l-1 2H9l-1-2z"/>`
	glyphProcessor  = `<path d="M7 3h10l-1 9H8zM5 12h14v3H5zM7 15h10v6H7z"/>`
	glyphStandMix   = `<path d="M4 5h10a4 4 0 0 1 4 4v2H4zM11 11v4M7 15h8l-1 4H8zM5 21h14"/>`
	glyphHandMix    = `<path d="M4 4h9a3 3 0 0 1 3 3v3H4zM7 10v8M11 10v8M5 18h4M9 18h4"/>`
	glyphDough      = `<path d="M12 3v9M12 12a4 4 0 1 0 4 4"/>`
	glyphColander   = `<path d="M3 10h18a9 8 0 0 1-18 0zM8 14h.01M12 14h.01M16 14h.01M10 17h.01M14 17h.01M21 10h2"/>`
	glyphSieve      = `<path d="M3 9h14a7 7 0 0 1-14 0zM17 9l5-2M6 12.5h8"/>`
	glyphBowl       = `<path d="M3 10h18a9 8 0 0 1-18 0zM9 20h6"/>`
	glyphBasket     = `<path d="M3 10h18a9 8 0 0 1-18 0zM6.5 13.5h11M9 17h6"/>`
	glyphWhisk      = `<path d="M12 3v6M8.5 9c-2 3-2 8 3.5 12M15.5 9c2 3 2 8-3.5 12M12 9c-1.5 3-1.5 8 0 12M12 9c1.5 3 1.5 8 0 12"/>`
	glyphRollingPin = `<path d="M6 9h12v6H6zM2 12h4M18 12h4"/>`
	glyphBoard      = `<path d="M5 4h14a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1zM11 7h2"/>`
	glyphKnife      = `<path d="M20 4c-6 0-12 5-15 13l3 3c8-3 13-9 12-16zM5 17l-2 4"/>`
	glyphBreadKnife = `<path d="M3 21l1-3M4 18L21 4v3L8 20z"/>`
	glyphRack       = `<path d="M3 9h18v8H3zM3 13h18M7 9v8M12 9v8M17 9v8"/>`
	glyphParchment  = `<path d="M5 6h14v11l-3 3H5zM16 20v-3h3"/>`
	glyphFoil       = `<path d="M3 8h14v8H3zM17 8a2 4 0 0 1 0 8M3 8a2 4 0 0 0 0 8"/>`
	glyphSpoon      = `<path d="M12 2.5c2.5 0 3.5 2 3.5 4S14.5 11 12 11 8.5 9 8.5 6.5 9.5 2.5 12 2.5zM12 11v10.5"/>`
	glyphSlotted    = `<path d="M12 2.5c2.5 0 3.5 2 3.5 4S14.5 11 12 11 8.5 9 8.5 6.5 9.5 2.5 12 2.5zM12 11v10.5M12 5v4"/>`
	glyphLadle      = `<path d="M20 4L11 13M11 13a4 4 0 1 0-4 4 4 4 0 0 0 4-4z"/>`
	glyphSpatula    = `<path d="M8 3h8l1 8H7zM12 11v10"/>`
	glyphTurner     = `<path d="M4 4h12l3 6H4zM7 4v6M10 4v6M13 4l2 6M12 10v11"/>`
	glyphOffset     = `<path d="M2 16h12l3-4h5"/>`
	glyphTongs      = `<path d="M9 3h6M9 3c-1 6-1 11-3 17M15 3c1 6 1 11 3 17M6 20h3M15 20h3"/>`
	glyphPeeler     = `<path d="M8 3h8v5l-4 3-4-3zM12 11v10M10 6h4"/>`
	glyphPress      = `<path d="M3 12h13l5-6M3 12v5h12v-5M6 17v2M12 17v2"/>`
	glyphMasher     = `<path d="M12 3v10M7 13h10l-1 3H8zM8 16v4M12 16v4M16 16v4"/>`
	glyphGrater     = `<path d="M8 3h8l-1 15H9zM12 18v3M10.5 7h.01M13.5 7h.01M10.5 10.5h.01M13.5 10.5h.01M10.5 14h.01M13.5 14h.01"/>`
	glyphMicroplane = `<path d="M9 2h6l-.5 14h-5zM12 16v5M11 6h2M11 9h2M11 12h2"/>`
	glyphMandoline  = `<path d="M3 14l18-5v3L3 18zM8 12v5M12 11v4M16 10v3"/>`
	glyphThermo     = `<path d="M10 5a2 2 0 0 1 4 0v8.5a4 4 0 1 1-4 0zM12 9v7"/>`
	glyphScale      = `<path d="M5 7h14l2 13H3zM8 12a4 3 0 0 1 8 0M12 12l1.5-2"/>`
	glyphCups       = `<path d="M5 5h11l-1 14H6zM16 8h3a1.5 1.5 0 0 1 0 5h-2M5.5 9h5M5.8 13h4"/>`
	glyphMeasSpoon  = `<path d="M8 3c2 0 3 1.5 3 3.5S10 10 8 10 5 8.5 5 6.5 6 3 8 3zM8 10v11M17 9a2.5 2.5 0 1 0 .01 0M17 14v7"/>`
	glyphBrush      = `<path d="M12 2v8M9 10h6v3H9zM9 13l-1 8h8l-1-8M11 15v5M13 15v5"/>`
	glyphScraper    = `<path d="M3 5h18v7H3zM7 12v6h10v-6"/>`
	glyphCanOpener  = `<path d="M5 9a5 5 0 1 0 .01 0M10 11l8 8"/>`
	glyphMallet     = `<path d="M3 4h12v7H3zM9 11v10"/>`
	glyphPeel       = `<path d="M12 3a6.5 6.5 0 1 1 0 13 6.5 6.5 0 0 1 0-13zM12 16v5"/>`
	glyphScoop      = `<path d="M4 8a7 7 0 0 0 14 0zM14 14l6 6"/>`
	glyphMat        = `<path d="M3 6h18v12H3zM3 12l6-6M3 18L15 6M9 18l12-12M15 18l6-6"/>`
	glyphCloth      = `<path d="M4 4h16v16H4zM4 9h16M4 15h16M9 4v16M15 4v16"/>`
	glyphTwine      = `<path d="M6 4h12M6 20h12M8 4v16M16 4v16M8 8l8 2M8 12l8 2M8 16l8 2"/>`
	glyphSkewers    = `<path d="M3 21L21 3M7.5 12.5l4 4M11 9l4 4M14.5 5.5l4 4"/>`
	glyphFunnel     = `<path d="M4 5h16l-6 7v7l-4 2v-9z"/>`
	glyphPastryCut  = `<path d="M5 4h14M7 4v8a5 5 0 0 0 10 0V4M12 4v13"/>`
	glyphMicrowave  = `<path d="M3 6h18v12H3zM16 6v12M18.5 9h.01M18.5 12h.01M7 9h5v6H7z"/>`
	glyphToaster    = `<path d="M3 7h18v11H3zM6 10h10v5H6zM19 10v5"/>`
	glyphGrinder    = `<path d="M8 3h8v4H8zM7 7h10l-1 12H8zM12 19v2M10 21h4"/>`
	glyphJuicer     = `<path d="M4 14h16a8 6 0 0 1-16 0zM9 14c0-4 1.5-6 3-6s3 2 3 6"/>`
	glyphWaffle     = `<path d="M4 7h16v11H4zM4 11h16M4 14.5h16M9 7v11M14 7v11"/>`
	glyphLame       = `<path d="M4 20L16 8M14 6l4 4M16 8l3-3"/>`
	glyphTorch      = `<path d="M4 12h9v5H4zM13 13h3M16 12l5-2M16 15l4 2M7 17v4"/>`
	glyphBag        = `<path d="M6 4h12l-4.5 15-1.5 3-1.5-3zM9 8h6"/>`
	glyphSteamer    = `<path d="M4 11h16l-2 8H6zM9 11v8M12 11v8M15 11v8M9 8c0-1 1-1 1-2M14 8c0-1 1-1 1-2"/>`
	glyphAirFryer   = `<path d="M6 3h12a2 2 0 0 1 2 2v12a4 4 0 0 1-4 4H8a4 4 0 0 1-4-4V5a2 2 0 0 1 2-2zM8 8h8v7H8zM17 6h.01"/>`
	glyphDeepFryer  = `<path d="M3 10h18v9a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1zM7 10l1-5h8l1 5M10 14h4"/>`
	glyphMortar     = `<path d="M4 11h16a8 8 0 0 1-16 0zM14 11l6-8M9 20h6"/>`
	glyphSpinner    = `<path d="M3 11h18v7a3 3 0 0 1-3 3H6a3 3 0 0 1-3-3zM5 11c0-3 3-5 7-5s7 2 7 5M10 3h4"/>`
	glyphShears     = `<path d="M5 17a3 3 0 1 0 6 0 3 3 0 0 0-6 0zM13 17a3 3 0 1 0 6 0 3 3 0 0 0-6 0zM9 14.5L17 3M15 14.5L7 3"/>`
	glyphStone      = `<path d="M3 12c0-2 4-4 9-4s9 2 9 4-4 4-9 4-9-2-9-4zM3 12v2c0 2 4 4 9 4s9-2 9-4v-2"/>`
)

// equipIcons maps a central catalog equipment name to its drawing. A test
// holds it equal to the catalog the migrations seed, so adding central
// equipment without an icon fails the build.
var equipIcons = map[string]string{
	"skillet":                  glyphPan,
	"saucepan":                 glyphSaucepan,
	"stockpot":                 glyphPot,
	"dutch oven":               glyphLidded,
	"baking sheet":             glyphSheet,
	"roasting pan":             glyphDish,
	"loaf pan":                 glyphLoaf,
	"muffin tin":               glyphMuffin,
	"cake pan":                 glyphCakePan,
	"springform pan":           glyphCakePan,
	"baking dish":              glyphDish,
	"oven":                     glyphOven,
	"grill":                    glyphGrill,
	"blender":                  glyphBlender,
	"immersion blender":        glyphImmersion,
	"food processor":           glyphProcessor,
	"stand mixer":              glyphStandMix,
	"hand mixer":               glyphHandMix,
	"slow cooker":              glyphSlowCook,
	"pressure cooker":          glyphPressure,
	"wok":                      glyphWok,
	"colander":                 glyphColander,
	"whisk":                    glyphWhisk,
	"rolling pin":              glyphRollingPin,
	"mixing bowl":              glyphBowl,
	"cutting board":            glyphBoard,
	"chef's knife":             glyphKnife,
	"wire rack":                glyphRack,
	"parchment paper":          glyphParchment,
	"aluminum foil":            glyphFoil,
	"half sheet pan":           glyphSheet,
	"quarter sheet pan":        glyphSheet,
	"jelly roll pan":           glyphSheet,
	"square baking pan":        glyphDish,
	"cast-iron skillet":        glyphPan,
	"nonstick pan":             glyphPan,
	"braiser":                  glyphLidded,
	"griddle":                  glyphGriddle,
	"grill pan":                glyphGrillPan,
	"pie dish":                 glyphPie,
	"tart pan":                 glyphTart,
	"bundt pan":                glyphBundt,
	"ramekin":                  glyphRamekin,
	"piping bag":               glyphBag,
	"kitchen torch":            glyphTorch,
	"steamer basket":           glyphSteamer,
	"rice cooker":              glyphRiceCook,
	"air fryer":                glyphAirFryer,
	"deep fryer":               glyphDeepFryer,
	"mortar and pestle":        glyphMortar,
	"salad spinner":            glyphSpinner,
	"kitchen shears":           glyphShears,
	"paring knife":             glyphKnife,
	"bread knife":              glyphBreadKnife,
	"baking stone":             glyphStone,
	"dough hook":               glyphDough,
	"fine-mesh sieve":          glyphSieve,
	"mandoline":                glyphMandoline,
	"instant-read thermometer": glyphThermo,
	"kitchen scale":            glyphScale,
	"measuring cups":           glyphCups,
	"measuring spoons":         glyphMeasSpoon,
	"pastry brush":             glyphBrush,
	"bench scraper":            glyphScraper,
	"microplane":               glyphMicroplane,
	"box grater":               glyphGrater,
	"tongs":                    glyphTongs,
	"ladle":                    glyphLadle,
	"spatula":                  glyphSpatula,
	"fish spatula":             glyphTurner,
	"offset spatula":           glyphOffset,
	"wooden spoon":             glyphSpoon,
	"slotted spoon":            glyphSlotted,
	"peeler":                   glyphPeeler,
	"garlic press":             glyphPress,
	"potato masher":            glyphMasher,
	"can opener":               glyphCanOpener,
	"meat mallet":              glyphMallet,
	"pizza peel":               glyphPeel,
	"cookie scoop":             glyphScoop,
	"baking mat":               glyphMat,
	"cheesecloth":              glyphCloth,
	"kitchen twine":            glyphTwine,
	"skewers":                  glyphSkewers,
	"funnel":                   glyphFunnel,
	"pastry cutter":            glyphPastryCut,
	"microwave":                glyphMicrowave,
	"toaster oven":             glyphToaster,
	"spice grinder":            glyphGrinder,
	"citrus juicer":            glyphJuicer,
	"waffle iron":              glyphWaffle,
	"proofing basket":          glyphBasket,
	"bread lame":               glyphLame,
}

func equipKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// EquipHasIcon reports whether the central catalog equipment of that name has
// its own icon rather than the generic one.
func EquipHasIcon(name string) bool {
	_, ok := equipIcons[equipKey(name)]
	return ok
}

// equipGlyph is the drawing for name, the generic equipment icon when the name
// is not central catalog equipment (a household's own item, an unknown name).
func equipGlyph(name string) string {
	if g, ok := equipIcons[equipKey(name)]; ok {
		return g
	}
	return glyphGeneric
}
