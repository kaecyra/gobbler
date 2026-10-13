package quantity

import (
	"errors"
	"testing"
)

func codecRat(t *testing.T, s string) Rat {
	t.Helper()
	r, err := ParseRat(s)
	if err != nil {
		t.Fatalf("ParseRat(%q): %v", s, err)
	}
	return r
}

func codecRange(t *testing.T, lo, hi string) Quantity {
	t.Helper()
	q, err := NewRange(codecRat(t, lo), codecRat(t, hi))
	if err != nil {
		t.Fatalf("NewRange(%s, %s): %v", lo, hi, err)
	}
	return q
}

// TestEncodeQtyStoredBytes pins the text rows already on disk were written
// with, as captured from the codecs this one replaced.
func TestEncodeQtyStoredBytes(t *testing.T) {
	cases := []struct {
		name   string
		q      Quantity
		want   string
		stored bool
	}{
		{"absent", Absent(), "", false},
		{"zero", Exact(Int(0)), "0/1", true},
		{"whole", Exact(Int(2)), "2/1", true},
		{"third", Exact(codecRat(t, "1/3")), "1/3", true},
		{"unicode fraction", Exact(codecRat(t, "⅓")), "1/3", true},
		{"mixed number", Exact(codecRat(t, "1 1/2")), "3/2", true},
		{"decimal", Exact(codecRat(t, "0.25")), "1/4", true},
		{"unreduced input", Exact(codecRat(t, "6/4")), "3/2", true},
		{"whole range", codecRange(t, "2", "3"), "2/1..3/1", true},
		{"fraction range", codecRange(t, "1/2", "3/4"), "1/2..3/4", true},
		{"degenerate range", codecRange(t, "2", "2"), "2/1..2/1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, stored, err := EncodeQty(c.q)
			if err != nil {
				t.Fatalf("EncodeQty: %v", err)
			}
			if got != c.want || stored != c.stored {
				t.Fatalf("EncodeQty = %q, %v; want %q, %v", got, stored, c.want, c.stored)
			}
			if !stored {
				return
			}
			back, err := DecodeQty(got)
			if err != nil {
				t.Fatalf("DecodeQty: %v", err)
			}
			if !back.Equal(c.q) || back.IsRange() != c.q.IsRange() {
				t.Fatalf("round trip = %v, want %v", back, c.q)
			}
		})
	}
}

func TestEncodeRatText(t *testing.T) {
	for in, want := range map[string]string{"1 1/2": "3/2", "2": "2/1", "6/4": "3/2"} {
		if got := EncodeRat(codecRat(t, in)); got != want {
			t.Errorf("EncodeRat(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEncodeQtyRejectsNegative(t *testing.T) {
	neg, err := NewRange(Int(-1), Int(2))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []Quantity{Exact(Int(-1)), neg} {
		if _, _, err := EncodeQty(q); !errors.Is(err, errNegativeStored) {
			t.Errorf("EncodeQty(%v) error = %v, want errNegativeStored", q, err)
		}
	}
}

func TestDecodeQtyAlternateForms(t *testing.T) {
	for in, want := range map[string]string{"6/4": "3/2", "1 1/2": "3/2", "2": "2/1", "1/2..1": "1/2..1/1"} {
		got, err := DecodeQty(in)
		if err != nil {
			t.Fatalf("DecodeQty(%q): %v", in, err)
		}
		enc, _, err := EncodeQty(got)
		if err != nil {
			t.Fatalf("EncodeQty(%v): %v", got, err)
		}
		if enc != want {
			t.Errorf("DecodeQty(%q) re-encodes to %q, want %q", in, enc, want)
		}
	}
}

func TestDecodeQtyRejectsMalformed(t *testing.T) {
	for _, in := range []string{"", "x", "1/0", "1/2..", "..1/2", "3/1..2/1", "1..2..3"} {
		if _, err := DecodeQty(in); err == nil {
			t.Errorf("DecodeQty(%q) succeeded, want error", in)
		}
	}
}

func codecUnits() *UnitTable {
	t := NewUnitTable()
	t.Add(1, MustUnit("g"))
	t.Add(2, MustUnit("cup"))
	t.Add(3, OtherUnit("can"))
	return t
}

func TestUnitTableRoundTrip(t *testing.T) {
	tbl := codecUnits()
	for id, name := range map[int64]string{1: "g", 2: "cup", 3: "can"} {
		u, ok := LookupUnit(name)
		if !ok {
			u = OtherUnit(name)
		}
		got, err := tbl.ID(u)
		if err != nil || got != id {
			t.Fatalf("ID(%s) = %d, %v; want %d", name, got, err, id)
		}
		back, err := tbl.Unit(id)
		if err != nil || back.Name != name || back.Dimension != u.Dimension || !back.Factor.Equal(u.Factor) {
			t.Fatalf("Unit(%d) = %+v, %v", id, back, err)
		}
	}
}

func TestUnitTableZeroUnitIsID0(t *testing.T) {
	tbl := codecUnits()
	if id, err := tbl.ID(Unit{}); err != nil || id != 0 {
		t.Fatalf("ID(zero) = %d, %v; want 0", id, err)
	}
	if u, err := tbl.Unit(0); err != nil || !u.IsZero() {
		t.Fatalf("Unit(0) = %+v, %v; want zero unit", u, err)
	}
}

func TestUnitTableUnknown(t *testing.T) {
	tbl := codecUnits()
	if _, err := tbl.ID(OtherUnit("smidge")); !errors.Is(err, errUnknownUnit) {
		t.Errorf("ID(unknown) error = %v, want errUnknownUnit", err)
	}
	if _, err := tbl.Unit(99); !errors.Is(err, errUnknownUnit) {
		t.Errorf("Unit(99) error = %v, want errUnknownUnit", err)
	}
}

// TestUnitTableOver lets a caller that already holds the id maps view
// them as a table without copying.
func TestUnitTableOver(t *testing.T) {
	ids := map[string]int64{"g": 7}
	units := map[int64]Unit{7: MustUnit("g")}
	tbl := UnitTableOver(ids, units)
	if id, err := tbl.ID(MustUnit("g")); err != nil || id != 7 {
		t.Fatalf("ID = %d, %v", id, err)
	}
}
