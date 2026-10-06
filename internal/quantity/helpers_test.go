package quantity

import "testing"

// rat builds n/d or fails the test.
func rat(t *testing.T, n, d int64) Rat {
	t.Helper()
	r, err := NewRat(n, d)
	if err != nil {
		t.Fatalf("NewRat(%d, %d): %v", n, d, err)
	}
	return r
}

// amt builds an exact amount of n/d in the named unit.
func amt(t *testing.T, n, d int64, unit string) Amount {
	t.Helper()
	return Amount{Qty: Exact(rat(t, n, d)), Unit: MustUnit(unit)}
}

// rng builds a range amount lo to hi, whole numbers, in the named unit.
func rng(t *testing.T, lo, hi int64, unit string) Amount {
	t.Helper()
	q, err := NewRange(Int(lo), Int(hi))
	if err != nil {
		t.Fatal(err)
	}
	return Amount{Qty: q, Unit: MustUnit(unit)}
}

// canOf builds "n (14 oz) can".
func canOf(n int64) Amount {
	return Amount{
		Qty: Exact(Int(n)), Unit: OtherUnit("can"),
		Package: PackageSize{Qty: Exact(Int(14)), Unit: MustUnit("oz")}, HasPackage: true,
	}
}

// brokenUnit is a volume unit as a bad stored row would build it: no factor.
func brokenUnit() Unit {
	return Unit{Name: "scoop", System: SystemUS, Dimension: DimVolume}
}
