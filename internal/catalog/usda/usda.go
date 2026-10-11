// Package usda enriches the canonical ingredient catalog from a USDA FoodData
// Central CSV download (ADR-00008). It fills a density and an FDC id on
// ingredients that lack them, and offers a candidate for foods that match no
// ingredient. It is an offline, operator-run import, never a start-up step.
//
// Three rules hold throughout:
//
//   - A value a human curated is never replaced: only an empty density or an
//     empty FDC id is filled, and the name, aliases, aisle and reviewed flag
//     are written back exactly as read (ADR-00008 obligation 2).
//   - Nothing is created: a food that matches no ingredient becomes an Offer in
//     the Report, for the catalog cleanup flow to act on (obligation 3).
//   - Re-running on the same input changes nothing: a filled field is no longer
//     empty, and the candidate chosen for an ingredient is independent of file
//     order.
//
// # Input format
//
// The input is a directory holding three files from a FoodData Central CSV
// download (https://fdc.nal.usda.gov/download-datasets), for example the
// Foundation Foods or SR Legacy archive:
//
//	food.csv          fdc_id, data_type, description
//	food_portion.csv  fdc_id, seq_num, amount, measure_unit_id,
//	                  portion_description, modifier, gram_weight
//	measure_unit.csv  id, name
//
// Other columns are ignored. Branded foods are skipped: their names are
// product names, not ingredients.
package usda

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/kaecyra/gobbler/internal/catalog"
)

// Input file names inside the download directory.
const (
	foodFile        = "food.csv"
	foodPortionFile = "food_portion.csv"
	measureUnitFile = "measure_unit.csv"
)

// brandedDataType is the food.csv data_type of commercial products. UNVERIFIED:
// the value is from memory. It could not be checked against the official
// Download Field Descriptions PDF (unreadable here) or the third-party
// catalogue that was checked, which describes data_type without listing values.
const brandedDataType = "branded_food"

// Catalog is what the import needs from the catalog store.
// *catalog.Store satisfies it.
type Catalog interface {
	MatchIngredient(ctx context.Context, term string) (catalog.Ingredient, bool, error)
	GetIngredient(ctx context.Context, id int64) (catalog.Ingredient, error)
	UpdateIngredient(ctx context.Context, in catalog.Ingredient) error
}

// Offer is a food that matched no catalog ingredient, suggested as a new
// canonical ingredient. It is only data: nothing is saved.
type Offer struct {
	// Name is the suggested bare ingredient name (the first segment of the
	// FoodData Central description, lower-case).
	Name string
	// FDCID and Description identify the food the suggestion came from.
	FDCID       int64
	Description string
	// DensityGPerML is nil when the food has no plain volume portion.
	DensityGPerML *float64
}

// Report summarises one import. Every count is accumulated while the import
// runs.
type Report struct {
	// Foods is the number of non-branded foods read.
	Foods int
	// Matched is the number of distinct catalog ingredients that at least one
	// food matched.
	Matched int
	// Filled is the number of ingredients that were updated.
	Filled int
	// SkippedCurated is the number of matched ingredients left untouched
	// because every field the import could fill already held a value, or is
	// linked to a different food.
	SkippedCurated int
	// Unmatched is the number of foods that matched no ingredient.
	Unmatched int
	// Offers is one suggestion per distinct suggested name, ordered by name.
	Offers []Offer
}

// candidate is what one food contributes to an ingredient.
type candidate struct {
	fdcID       int64
	description string
	density     *float64
}

// better reports whether c should be preferred over o for the same target: a
// food with a density beats one without, then the lowest FDC id wins, so the
// choice does not depend on the order of the file.
func (c candidate) better(o candidate) bool {
	if (c.density != nil) != (o.density != nil) {
		return c.density != nil
	}
	return c.fdcID < o.fdcID
}

// Import reads the FoodData Central CSV files in fsys and enriches cat. It
// stops at the first malformed record with an error naming the file and line.
// Each ingredient is updated independently, so a failure part-way leaves some
// ingredients filled and the rest untouched; re-running after fixing the cause
// completes the job and changes nothing already done. Each update is a
// read-then-write of one ingredient: only an edit landing between that read
// and write (the store offers no field-level fill) can be lost.
func Import(ctx context.Context, cat Catalog, fsys fs.FS) (Report, error) {
	units, err := readMeasureUnits(fsys)
	if err != nil {
		return Report{}, err
	}
	densities, err := readDensities(fsys, units)
	if err != nil {
		return Report{}, err
	}

	var rep Report
	chosen := map[int64]candidate{}
	offers := map[string]Offer{}

	err = readCSV(fsys, foodFile, []string{"fdc_id", "data_type", "description"}, func(r row) error {
		if r.get("data_type") == brandedDataType {
			return nil
		}
		id, err := strconv.ParseInt(r.get("fdc_id"), 10, 64)
		if err != nil || id <= 0 {
			return r.errAt("fdc_id %q is not a positive integer", r.get("fdc_id"))
		}
		desc := r.get("description")
		if desc == "" {
			return r.errAt("food %d has no description", id)
		}
		rep.Foods++
		c := candidate{fdcID: id, description: desc, density: densities[id]}

		ing, ok, err := matchFood(ctx, cat, desc)
		if err != nil {
			return r.errAt("match food %d %q: %w", id, desc, err)
		}
		if !ok {
			rep.Unmatched++
			o := Offer{Name: suggestedName(desc), FDCID: id, Description: desc, DensityGPerML: c.density}
			if prev, seen := offers[o.Name]; !seen || c.better(candidate{fdcID: prev.FDCID, density: prev.DensityGPerML}) {
				offers[o.Name] = o
			}
			return nil
		}
		if prev, seen := chosen[ing.ID]; !seen || c.better(prev) {
			chosen[ing.ID] = c
		}
		return nil
	})
	if err != nil {
		return Report{}, err
	}

	rep.Matched = len(chosen)
	ids := make([]int64, 0, len(chosen))
	for id := range chosen {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		// Re-read just before writing: the scan above can run for a long time
		// and UpdateIngredient replaces the whole row, so filling a copy taken
		// at match time would revert any edit made since.
		current, err := cat.GetIngredient(ctx, id)
		if err != nil {
			return rep, fmt.Errorf("re-read ingredient %d before filling from FDC %d: %w", id, chosen[id].fdcID, err)
		}
		updated, changed := fill(current, chosen[id])
		if !changed {
			rep.SkippedCurated++
			continue
		}
		if err := cat.UpdateIngredient(ctx, updated); err != nil {
			return rep, fmt.Errorf("fill ingredient %q from FDC %d: %w", updated.Name, chosen[id].fdcID, err)
		}
		rep.Filled++
	}

	for _, o := range offers {
		rep.Offers = append(rep.Offers, o)
	}
	slices.SortFunc(rep.Offers, func(a, b Offer) int { return strings.Compare(a.Name, b.Name) })

	slog.InfoContext(ctx, "usda import finished",
		"foods", rep.Foods, "matched", rep.Matched, "filled", rep.Filled,
		"skipped_curated", rep.SkippedCurated, "unmatched", rep.Unmatched, "offers", len(rep.Offers))
	return rep, nil
}

// fill returns in with its empty density and empty FDC id set from c, and
// whether anything changed. Nothing else is touched. An ingredient already
// linked to a different food is left alone entirely, so its density never
// comes from a food it is not linked to.
func fill(in catalog.Ingredient, c candidate) (catalog.Ingredient, bool) {
	if in.USDAID != nil && *in.USDAID != c.fdcID {
		return in, false
	}
	changed := false
	if in.USDAID == nil {
		id := c.fdcID
		in.USDAID = &id
		changed = true
	}
	if in.DensityGPerML == nil && c.density != nil {
		d := *c.density
		in.DensityGPerML = &d
		changed = true
	}
	return in, changed
}

// matchFood looks for an ingredient named by a FoodData Central description.
// Descriptions run general to specific ("Butter, without salt"), so it tries
// the whole description and then each shorter leading run of segments,
// returning the first match. It stops at the first error.
func matchFood(ctx context.Context, cat Catalog, desc string) (catalog.Ingredient, bool, error) {
	segs := segments(desc)
	for n := len(segs); n >= 1; n-- {
		ing, ok, err := cat.MatchIngredient(ctx, strings.Join(segs[:n], " "))
		if err != nil || ok {
			return ing, ok, err
		}
	}
	return catalog.Ingredient{}, false, nil
}

func suggestedName(desc string) string {
	segs := segments(desc)
	if len(segs) == 0 {
		return normalizeText(desc)
	}
	return segs[0]
}

func segments(desc string) []string {
	var out []string
	for _, s := range strings.Split(desc, ",") {
		if s = normalizeText(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// readMeasureUnits returns measure_unit id to unit name. UNVERIFIED: "name" is
// confirmed by a third-party catalogue of the download fields, but the "id"
// column is assumed (that source omits it, though food_portion.measure_unit_id
// must reference something). Neither was checked against the official Download
// Field Descriptions PDF, which could not be read.
func readMeasureUnits(fsys fs.FS) (map[string]string, error) {
	units := map[string]string{}
	err := readCSV(fsys, measureUnitFile, []string{"id", "name"}, func(r row) error {
		units[r.get("id")] = r.get("name")
		return nil
	})
	return units, err
}

// readDensities returns, per FDC id, the density from the food's first plain
// volume portion in seq_num order (file order breaks ties). Portions without
// an amount or gram weight, and portions that are not a plain volume, give no
// density and are skipped; an unparsable number is an error.
func readDensities(fsys fs.FS, units map[string]string) (map[int64]*float64, error) {
	type pick struct {
		seq     int
		density float64
	}
	best := map[int64]pick{}
	required := []string{"fdc_id", "seq_num", "amount", "measure_unit_id", "portion_description", "modifier", "gram_weight"}
	err := readCSV(fsys, foodPortionFile, required, func(r row) error {
		id, err := strconv.ParseInt(r.get("fdc_id"), 10, 64)
		if err != nil || id <= 0 {
			return r.errAt("fdc_id %q is not a positive integer", r.get("fdc_id"))
		}
		amount, haveAmount, err := parseNumber(r.get("amount"))
		if err != nil {
			return r.errAt("food %d amount: %w", id, err)
		}
		grams, haveGrams, err := parseNumber(r.get("gram_weight"))
		if err != nil {
			return r.errAt("food %d gram_weight: %w", id, err)
		}
		seq := 0
		if s := r.get("seq_num"); s != "" {
			if seq, err = strconv.Atoi(s); err != nil {
				return r.errAt("food %d seq_num %q is not an integer", id, s)
			}
		}
		if !haveAmount || !haveGrams {
			return nil
		}
		measure := units[r.get("measure_unit_id")]
		if r.get("measure_unit_id") == undeterminedUnit {
			measure = ""
		}
		unit, ok := portionUnit(measure, r.get("modifier"), r.get("portion_description"))
		if !ok {
			return nil
		}
		d, ok := densityGPerML(amount, unit, grams)
		if !ok {
			return nil
		}
		if prev, seen := best[id]; !seen || seq < prev.seq {
			best[id] = pick{seq: seq, density: d}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make(map[int64]*float64, len(best))
	for id, p := range best {
		d := p.density
		out[id] = &d
	}
	return out, nil
}
