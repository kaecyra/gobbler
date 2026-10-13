package store

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
)

func mustRat(t *testing.T, s string) quantity.Rat {
	t.Helper()
	r, err := quantity.ParseRat(s)
	if err != nil {
		t.Fatalf("ParseRat(%q): %v", s, err)
	}
	return r
}

func mustRange(t *testing.T, lo, hi string) quantity.Quantity {
	t.Helper()
	q, err := quantity.NewRange(mustRat(t, lo), mustRat(t, hi))
	if err != nil {
		t.Fatalf("NewRange(%s, %s): %v", lo, hi, err)
	}
	return q
}

// TestQtyStoredBytes pins the exact text written to the database. Rows
// already on disk were written in this form, so it must never change.
func TestQtyStoredBytes(t *testing.T) {
	cases := []struct {
		name string
		q    quantity.Quantity
		want *string // nil: stored as NULL
	}{
		{"absent", quantity.Absent(), nil},
		{"zero", quantity.Exact(quantity.Int(0)), strRef("0/1")},
		{"whole", quantity.Exact(quantity.Int(2)), strRef("2/1")},
		{"third", quantity.Exact(mustRat(t, "1/3")), strRef("1/3")},
		{"unicode fraction", quantity.Exact(mustRat(t, "⅓")), strRef("1/3")},
		{"mixed number", quantity.Exact(mustRat(t, "1 1/2")), strRef("3/2")},
		{"decimal", quantity.Exact(mustRat(t, "0.25")), strRef("1/4")},
		{"unreduced input", quantity.Exact(mustRat(t, "6/4")), strRef("3/2")},
		{"whole range", mustRange(t, "2", "3"), strRef("2/1..3/1")},
		{"fraction range", mustRange(t, "1/2", "3/4"), strRef("1/2..3/4")},
		{"degenerate range", mustRange(t, "2", "2"), strRef("2/1..2/1")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := encodeQty(c.q)
			if err != nil {
				t.Fatalf("encodeQty: %v", err)
			}
			if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
				t.Fatalf("encodeQty = %v, want %v", deref(got), deref(c.want))
			}
			back, err := decodeQty(got)
			if err != nil {
				t.Fatalf("decodeQty: %v", err)
			}
			if !back.Equal(c.q) || back.IsRange() != c.q.IsRange() {
				t.Fatalf("round trip = %v, want %v", back, c.q)
			}
			again, err := encodeQty(back)
			if err != nil || deref(again) != deref(got) {
				t.Fatalf("re-encode = %v (%v), want %v", deref(again), err, deref(got))
			}
		})
	}
}

func TestQtyRejectsNegative(t *testing.T) {
	for _, q := range []quantity.Quantity{
		quantity.Exact(quantity.Int(-1)),
		negRange(t),
	} {
		if _, err := encodeQty(q); !errors.Is(err, ErrInvalid) {
			t.Errorf("encodeQty(%v) error = %v, want ErrInvalid", q, err)
		}
	}
}

func TestQtyDecodesAlternateForms(t *testing.T) {
	for in, want := range map[string]string{"6/4": "3/2", "1 1/2": "3/2", "2": "2/1", "1/2..1": "1/2..1/1"} {
		got, err := decodeQty(&in)
		if err != nil {
			t.Fatalf("decodeQty(%q): %v", in, err)
		}
		enc, _ := encodeQty(got)
		if deref(enc) != want {
			t.Errorf("decodeQty(%q) re-encodes to %q, want %q", in, deref(enc), want)
		}
	}
}

func TestQtyDecodeRejectsMalformed(t *testing.T) {
	for _, in := range []string{"", "x", "1/0", "1/2..", "..1/2", "3/1..2/1", "1..2..3"} {
		if _, err := decodeQty(&in); err == nil {
			t.Errorf("decodeQty(%q) succeeded, want error", in)
		}
	}
}

func testUnitTable() *unitTable {
	t := &unitTable{ids: map[string]int64{}, units: map[int64]quantity.Unit{}}
	for i, u := range []quantity.Unit{
		quantity.MustUnit("g"), quantity.MustUnit("cup"), quantity.OtherUnit("can"),
	} {
		t.ids[u.Name], t.units[int64(i+1)] = int64(i+1), u
	}
	return t
}

func TestUnitRefs(t *testing.T) {
	tbl := testUnitTable()
	for _, name := range []string{"g", "cup", "can"} {
		u, _ := tbl.units[tbl.ids[name]], 0
		id, err := tbl.id(u)
		if err != nil || !id.Valid || id.Int64 != tbl.ids[name] {
			t.Fatalf("id(%s) = %v, %v", name, id, err)
		}
		back, err := tbl.unit(id)
		if err != nil || back.Name != name || back.Dimension != u.Dimension || !back.Factor.Equal(u.Factor) {
			t.Fatalf("unit(%v) = %+v, %v", id, back, err)
		}
		ref, err := tbl.refID(u)
		if err != nil || ref != id.Int64 {
			t.Fatalf("refID(%s) = %d, %v", name, ref, err)
		}
		if got, err := tbl.byRef(ref); err != nil || got.Name != name {
			t.Fatalf("byRef(%d) = %+v, %v", ref, got, err)
		}
	}
}

func TestUnitZeroIsNull(t *testing.T) {
	tbl := testUnitTable()
	if id, err := tbl.id(quantity.Unit{}); err != nil || id.Valid {
		t.Fatalf("id(zero) = %v, %v; want NULL", id, err)
	}
	if ref, err := tbl.refID(quantity.Unit{}); err != nil || ref != 0 {
		t.Fatalf("refID(zero) = %d, %v; want 0", ref, err)
	}
	if u, err := tbl.unit(sql.NullInt64{}); err != nil || !u.IsZero() {
		t.Fatalf("unit(NULL) = %+v, %v; want zero unit", u, err)
	}
	if u, err := tbl.byRef(0); err != nil || !u.IsZero() {
		t.Fatalf("byRef(0) = %+v, %v; want zero unit", u, err)
	}
}

func TestUnitUnknownIsUnknownReference(t *testing.T) {
	tbl := testUnitTable()
	if _, err := tbl.id(quantity.OtherUnit("smidge")); !errors.Is(err, ErrUnknownReference) {
		t.Errorf("id(unknown) error = %v, want ErrUnknownReference", err)
	}
	if _, err := tbl.unit(sql.NullInt64{Int64: 99, Valid: true}); !errors.Is(err, ErrUnknownReference) {
		t.Errorf("unit(99) error = %v, want ErrUnknownReference", err)
	}
}

func strRef(s string) *string { return &s }

func deref(s *string) string {
	if s == nil {
		return "<NULL>"
	}
	return *s
}

func negRange(t *testing.T) quantity.Quantity {
	t.Helper()
	q, err := quantity.NewRange(quantity.Int(-1), quantity.Int(2))
	if err != nil {
		t.Fatal(err)
	}
	return q
}
