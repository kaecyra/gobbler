package shopping

import (
	"fmt"
	"sort"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// bucketKey groups amounts that may be summed into one.
type bucketKey struct {
	rank int    // display order: volume, mass, count, other
	name string // dimension, or "other:<unit>"
	pkg  string // package size, empty when unpackaged
}

const (
	rankVolume = iota
	rankMass
	rankCount
	rankOther
)

func keyOf(a quantity.Amount) bucketKey {
	var k bucketKey
	switch {
	case a.Unit.Converts() && a.Unit.Dimension == quantity.DimVolume:
		k = bucketKey{rank: rankVolume, name: string(quantity.DimVolume)}
	case a.Unit.Converts() && a.Unit.Dimension == quantity.DimMass:
		k = bucketKey{rank: rankMass, name: string(quantity.DimMass)}
	case a.Unit.Converts() && a.Unit.Dimension == quantity.DimCount:
		k = bucketKey{rank: rankCount, name: string(quantity.DimCount)}
	default:
		k = bucketKey{rank: rankOther, name: "other:" + a.Unit.Name}
	}
	if a.HasPackage {
		k.pkg = a.Package.Qty.String() + " " + a.Package.Unit.Name
	}
	return k
}

// mergeAmounts sums amounts that share a dimension (and package size) and
// returns one amount per group, in display order. Amounts without a number
// ("to taste") are ignored. density is grams per millilitre, zero when
// unknown; only then may volume and mass merge into one mass amount.
func mergeAmounts(in []quantity.Amount, density quantity.Rat) ([]quantity.Amount, error) {
	buckets := map[bucketKey]quantity.Amount{}
	for _, a := range in {
		if a.Qty.IsAbsent() {
			continue
		}
		if a.Unit.IsZero() && !a.HasPackage {
			a.Unit = quantity.MustUnit("each")
		}
		if err := addTo(buckets, a); err != nil {
			return nil, err
		}
	}

	vol, mass := bucketKey{rank: rankVolume, name: string(quantity.DimVolume)}, bucketKey{rank: rankMass, name: string(quantity.DimMass)}
	if v, ok := buckets[vol]; ok {
		if _, hasMass := buckets[mass]; hasMass {
			g, ok, err := quantity.ToMass(v, density)
			if err != nil {
				return nil, fmt.Errorf("convert volume to mass: %w", err)
			}
			if ok {
				delete(buckets, vol)
				if err := addTo(buckets, g); err != nil {
					return nil, err
				}
			}
		}
	}

	keys := make([]bucketKey, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.name != b.name {
			return a.name < b.name
		}
		return a.pkg < b.pkg
	})

	out := make([]quantity.Amount, 0, len(keys))
	for _, k := range keys {
		a := buckets[k]
		r, err := quantity.Readable(a)
		if err != nil {
			return nil, fmt.Errorf("make %s readable: %w", a, err)
		}
		if r.Unit.Name == "each" && !r.HasPackage {
			r.Unit = quantity.Unit{} // "3", not "3 each"
		}
		out = append(out, r)
	}
	return out, nil
}

// addTo adds a to the bucket for its key, expressing the sum in whichever of
// the two units is smaller so the result is exact.
func addTo(buckets map[bucketKey]quantity.Amount, a quantity.Amount) error {
	k := keyOf(a)
	cur, ok := buckets[k]
	if !ok {
		buckets[k] = a
		return nil
	}
	target := cur.Unit
	if a.Unit.Converts() && a.Unit.Factor.Cmp(cur.Unit.Factor) < 0 {
		target = a.Unit
	}
	x, err := quantity.Convert(cur, target)
	if err != nil {
		return fmt.Errorf("add %s to %s: %w", a, cur, err)
	}
	y, err := quantity.Convert(a, target)
	if err != nil {
		return fmt.Errorf("add %s to %s: %w", a, cur, err)
	}
	x.Qty = addQty(x.Qty, y.Qty)
	buckets[k] = x
	return nil
}

// addQty sums two present quantities; the result is a range if either is.
func addQty(a, b quantity.Quantity) quantity.Quantity {
	lo, hi := a.Min().Add(b.Min()), a.Max().Add(b.Max())
	if !a.IsRange() && !b.IsRange() {
		return quantity.Exact(lo)
	}
	q, err := quantity.NewRange(lo, hi)
	if err != nil {
		// Unreachable: both inputs have min <= max, so the sums do too.
		panic("shopping: summed range inverted: " + err.Error())
	}
	return q
}
