package quantity

import "math/big"

// Rounded pairs a display-ready amount with the exact one it came from, so
// views can show "about 1/3 cup" and still total the exact value.
type Rounded struct {
	Exact   Amount
	Rounded Amount
}

// fractions are the fractional parts a US kitchen reads, ascending, ending at 1.
var fractions = [][2]int64{
	{0, 1}, {1, 8}, {1, 4}, {1, 3}, {3, 8}, {1, 2}, {5, 8}, {2, 3}, {3, 4}, {7, 8}, {1, 1},
}

// Round rounds a to kitchen precision. US, count, other and unit-less amounts
// go to the nearest common fraction (eighths, quarters, thirds); a positive
// amount never rounds to zero. Metric amounts go to a sensible step of the
// base unit: 0.5 below 10, 1 below 100, 5 below 1000, 10 from 1000. Ranges
// round each bound. Exact is returned alongside, unchanged. The error is
// non-nil only for an invalid unit (ErrInvalidUnit).
func Round(a Amount) (Rounded, error) {
	if err := a.Unit.Validate(); err != nil {
		return Rounded{}, err
	}
	if a.Qty.IsAbsent() {
		return Rounded{Exact: a, Rounded: a}, nil
	}
	round := func(r Rat) (Rat, error) { return roundFraction(r), nil }
	if a.Unit.System == SystemMetric && a.Unit.Converts() {
		round = func(r Rat) (Rat, error) { return roundMetric(r, a.Unit.Factor) }
	}
	q := a.Qty
	lo, err := round(q.Min())
	if err != nil {
		return Rounded{}, err
	}
	hi, err := round(q.Max())
	if err != nil {
		return Rounded{}, err
	}
	var out Quantity
	if q.IsRange() {
		out, _ = NewRange(lo, hi) // rounding is monotonic, so lo <= hi
	} else {
		out = Exact(lo)
	}
	return Rounded{Exact: a, Rounded: a.withQty(out)}, nil
}

// roundFraction rounds a non-negative r to the nearest common fraction; ties
// go up. A positive r never rounds to zero.
func roundFraction(r Rat) Rat {
	if r.Sign() <= 0 {
		return r
	}
	whole := r.Floor()
	rem := r.Sub(whole)
	best, bestDist := Rat{}, Rat{}
	for i, f := range fractions {
		cand := Rat{v: big.NewRat(f[0], f[1])}
		dist := rem.Sub(cand)
		if dist.Sign() < 0 {
			dist = cand.Sub(rem)
		}
		// <= so a tie resolves to the larger fraction.
		if i == 0 || dist.Cmp(bestDist) <= 0 {
			best, bestDist = cand, dist
		}
	}
	out := whole.Add(best)
	if out.Sign() == 0 {
		return Rat{v: big.NewRat(1, 8)}
	}
	return out
}

// roundMetric rounds r units (each worth factor base units) to a step of the
// base unit chosen by magnitude. A positive r never rounds to zero.
func roundMetric(r, factor Rat) (Rat, error) {
	if r.Sign() <= 0 {
		return r, nil
	}
	base := r.Mul(factor)
	var step Rat
	switch {
	case base.Cmp(Int(10)) < 0:
		step = Rat{v: big.NewRat(1, 2)}
	case base.Cmp(Int(100)) < 0:
		step = Int(1)
	case base.Cmp(Int(1000)) < 0:
		step = Int(5)
	default:
		step = Int(10)
	}
	// floor(base/step + 1/2) * step; step is positive.
	n, err := base.Div(step)
	if err != nil {
		return Rat{}, err
	}
	n = n.Add(Rat{v: big.NewRat(1, 2)}).Floor()
	rounded := n.Mul(step)
	if rounded.Sign() == 0 {
		rounded = step
	}
	return rounded.Div(factor)
}
