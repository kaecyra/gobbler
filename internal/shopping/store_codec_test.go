package shopping

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

func nstr(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// TestQtyStoredBytes pins the exact text written to the database, which must
// stay identical to the recipe lines' (internal/store) form.
func TestQtyStoredBytes(t *testing.T) {
	cases := []struct {
		name string
		q    quantity.Quantity
		want sql.NullString
	}{
		{"absent", quantity.Absent(), sql.NullString{}},
		{"zero", quantity.Exact(quantity.Int(0)), nstr("0/1")},
		{"whole", quantity.Exact(quantity.Int(2)), nstr("2/1")},
		{"third", quantity.Exact(mustRat(t, "1/3")), nstr("1/3")},
		{"unicode fraction", quantity.Exact(mustRat(t, "⅓")), nstr("1/3")},
		{"mixed number", quantity.Exact(mustRat(t, "1 1/2")), nstr("3/2")},
		{"decimal", quantity.Exact(mustRat(t, "0.25")), nstr("1/4")},
		{"unreduced input", quantity.Exact(mustRat(t, "6/4")), nstr("3/2")},
		{"whole range", mustRange(t, "2", "3"), nstr("2/1..3/1")},
		{"fraction range", mustRange(t, "1/2", "3/4"), nstr("1/2..3/4")},
		{"degenerate range", mustRange(t, "2", "2"), nstr("2/1..2/1")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := encodeQty(c.q)
			if err != nil {
				t.Fatalf("encodeQty: %v", err)
			}
			if got != c.want {
				t.Fatalf("encodeQty = %+v, want %+v", got, c.want)
			}
			back, err := decodeQty(got)
			if err != nil {
				t.Fatalf("decodeQty: %v", err)
			}
			if !back.Equal(c.q) || back.IsRange() != c.q.IsRange() {
				t.Fatalf("round trip = %v, want %v", back, c.q)
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
		got, err := decodeQty(nstr(in))
		if err != nil {
			t.Fatalf("decodeQty(%q): %v", in, err)
		}
		enc, _ := encodeQty(got)
		if enc.String != want {
			t.Errorf("decodeQty(%q) re-encodes to %q, want %q", in, enc.String, want)
		}
	}
}

func TestQtyDecodeRejectsMalformedAsCorrupt(t *testing.T) {
	for _, in := range []string{"", "x", "1/0", "1/2..", "..1/2", "3/1..2/1", "1..2..3"} {
		if _, err := decodeQty(nstr(in)); !errors.Is(err, ErrCorrupt) {
			t.Errorf("decodeQty(%q) error = %v, want ErrCorrupt", in, err)
		}
	}
}

func TestMultiplierText(t *testing.T) {
	if got := ratText(mustRat(t, "1 1/2")); got != "3/2" {
		t.Errorf("ratText = %q, want 3/2", got)
	}
	if got := ratText(quantity.Int(2)); got != "2/1" {
		t.Errorf("ratText = %q, want 2/1", got)
	}
	if r, err := decodeMultiplier("3/2"); err != nil || !r.Equal(mustRat(t, "3/2")) {
		t.Errorf("decodeMultiplier = %v, %v", r, err)
	}
	if _, err := decodeMultiplier("x"); !errors.Is(err, ErrCorrupt) {
		t.Errorf("decodeMultiplier(x) error = %v, want ErrCorrupt", err)
	}
}

func testUnitTable() *unitTable {
	t := quantity.NewUnitTable()
	for i, u := range []quantity.Unit{
		quantity.MustUnit("g"), quantity.MustUnit("cup"), quantity.OtherUnit("can"),
	} {
		t.Add(int64(i+1), u)
	}
	return &unitTable{t}
}

func TestAmountColumns(t *testing.T) {
	tbl := testUnitTable()
	pkg := quantity.Amount{
		Qty: mustRange(t, "1", "2"), Unit: quantity.OtherUnit("can"), HasPackage: true,
		Package: quantity.PackageSize{Qty: quantity.Exact(quantity.Int(14)), Unit: quantity.MustUnit("g")},
	}
	cases := map[string]quantity.Amount{
		"empty":    {},
		"mass":     {Qty: quantity.Exact(mustRat(t, "1/3")), Unit: quantity.MustUnit("g")},
		"volume":   {Qty: quantity.Exact(mustRat(t, "1 1/2")), Unit: quantity.MustUnit("cup")},
		"other":    {Qty: quantity.Exact(quantity.Int(1)), Unit: quantity.OtherUnit("can")},
		"unitless": {Qty: quantity.Exact(quantity.Int(2))},
		"package":  pkg,
	}
	for name, a := range cases {
		t.Run(name, func(t *testing.T) {
			c, err := tbl.encodeAmount(a)
			if err != nil {
				t.Fatalf("encodeAmount: %v", err)
			}
			back, err := tbl.decodeAmount(c)
			if err != nil {
				t.Fatalf("decodeAmount: %v", err)
			}
			if back.String() != a.String() || back.HasPackage != a.HasPackage {
				t.Fatalf("round trip = %q, want %q", back, a)
			}
		})
	}
	c, _ := tbl.encodeAmount(pkg)
	if c.qty != nstr("1/1..2/1") || c.unit != (sql.NullInt64{Int64: 3, Valid: true}) ||
		c.hasPackage != 1 || c.pkgQty != nstr("14/1") || c.pkgUnit != (sql.NullInt64{Int64: 1, Valid: true}) {
		t.Fatalf("package columns = %+v", c)
	}
}

func TestUnitErrors(t *testing.T) {
	tbl := testUnitTable()
	if id, err := tbl.id(quantity.Unit{}); err != nil || id.Valid {
		t.Fatalf("id(zero) = %v, %v; want NULL", id, err)
	}
	if u, err := tbl.unit(sql.NullInt64{}); err != nil || !u.IsZero() {
		t.Fatalf("unit(NULL) = %+v, %v; want zero unit", u, err)
	}
	if _, err := tbl.id(quantity.OtherUnit("smidge")); !errors.Is(err, ErrUnknownReference) {
		t.Errorf("id(unknown) error = %v, want ErrUnknownReference", err)
	}
	if _, err := tbl.unit(sql.NullInt64{Int64: 99, Valid: true}); !errors.Is(err, ErrCorrupt) {
		t.Errorf("unit(99) error = %v, want ErrCorrupt", err)
	}
}

func negRange(t *testing.T) quantity.Quantity {
	t.Helper()
	q, err := quantity.NewRange(quantity.Int(-1), quantity.Int(2))
	if err != nil {
		t.Fatal(err)
	}
	return q
}
