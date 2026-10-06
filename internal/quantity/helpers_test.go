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
