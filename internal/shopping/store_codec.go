package shopping

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// The stored forms match the recipe lines' (migration 00002): a quantity is
// NULL for none, "n/d" for an exact value and "n/d..n/d" for a range, with n/d
// the exact reduced fraction; a unit is a reference to units(id).

const rangeSep = ".."

func ratText(r quantity.Rat) string {
	return r.Num().String() + "/" + r.Denom().String()
}

func encodeQty(q quantity.Quantity) (sql.NullString, error) {
	if q.IsAbsent() {
		return sql.NullString{}, nil
	}
	if q.Min().Sign() < 0 || q.Max().Sign() < 0 {
		return sql.NullString{}, fmt.Errorf("%w: negative quantity %s", ErrInvalid, q)
	}
	s := ratText(q.Min())
	if q.IsRange() {
		s += rangeSep + ratText(q.Max())
	}
	return sql.NullString{String: s, Valid: true}, nil
}

func decodeQty(n sql.NullString) (quantity.Quantity, error) {
	if !n.Valid {
		return quantity.Absent(), nil
	}
	lo, hi, isRange := strings.Cut(n.String, rangeSep)
	loR, err := quantity.ParseRat(lo)
	if err != nil {
		return quantity.Quantity{}, fmt.Errorf("%w: quantity %q: %w", ErrCorrupt, n.String, err)
	}
	if !isRange {
		return quantity.Exact(loR), nil
	}
	hiR, err := quantity.ParseRat(hi)
	if err != nil {
		return quantity.Quantity{}, fmt.Errorf("%w: quantity %q: %w", ErrCorrupt, n.String, err)
	}
	q, err := quantity.NewRange(loR, hiR)
	if err != nil {
		return quantity.Quantity{}, fmt.Errorf("%w: quantity %q: %w", ErrCorrupt, n.String, err)
	}
	return q, nil
}

func decodeMultiplier(s string) (quantity.Rat, error) {
	r, err := quantity.ParseRat(s)
	if err != nil {
		return quantity.Rat{}, fmt.Errorf("%w: multiplier %q: %w", ErrCorrupt, s, err)
	}
	return r, nil
}

// unitTable is the catalog's units, loaded once per operation: manual amounts
// hold full quantity.Units, the database a reference by id.
type unitTable struct {
	ids   map[string]int64
	units map[int64]quantity.Unit
}

func (s *Store) loadUnits(ctx context.Context) (*unitTable, error) {
	all, err := s.catalog.ListUnits(ctx)
	if err != nil {
		return nil, fmt.Errorf("load units: %w", err)
	}
	t := &unitTable{ids: make(map[string]int64, len(all)), units: make(map[int64]quantity.Unit, len(all))}
	for _, u := range all {
		t.ids[u.Unit.Name] = u.ID
		t.units[u.ID] = u.Unit
	}
	return t, nil
}

func (t *unitTable) id(u quantity.Unit) (sql.NullInt64, error) {
	if u.IsZero() {
		return sql.NullInt64{}, nil
	}
	id, ok := t.ids[u.Name]
	if !ok {
		return sql.NullInt64{}, fmt.Errorf("%w: unit %q", ErrUnknownReference, u.Name)
	}
	return sql.NullInt64{Int64: id, Valid: true}, nil
}

func (t *unitTable) unit(id sql.NullInt64) (quantity.Unit, error) {
	if !id.Valid {
		return quantity.Unit{}, nil
	}
	u, ok := t.units[id.Int64]
	if !ok {
		return quantity.Unit{}, fmt.Errorf("%w: unit id %d", ErrCorrupt, id.Int64)
	}
	return u, nil
}

// amountCols is the stored form of a quantity.Amount.
type amountCols struct {
	qty        sql.NullString
	unit       sql.NullInt64
	hasPackage int64
	pkgQty     sql.NullString
	pkgUnit    sql.NullInt64
}

func (t *unitTable) encodeAmount(a quantity.Amount) (amountCols, error) {
	var c amountCols
	var err error
	if c.qty, err = encodeQty(a.Qty); err != nil {
		return c, err
	}
	if c.unit, err = t.id(a.Unit); err != nil {
		return c, err
	}
	if !a.HasPackage {
		return c, nil
	}
	c.hasPackage = 1
	if c.pkgQty, err = encodeQty(a.Package.Qty); err != nil {
		return c, err
	}
	c.pkgUnit, err = t.id(a.Package.Unit)
	return c, err
}

func (t *unitTable) decodeAmount(c amountCols) (quantity.Amount, error) {
	var a quantity.Amount
	var err error
	if a.Qty, err = decodeQty(c.qty); err != nil {
		return a, err
	}
	if a.Unit, err = t.unit(c.unit); err != nil {
		return a, err
	}
	if c.hasPackage == 0 {
		return a, nil
	}
	a.HasPackage = true
	if a.Package.Qty, err = decodeQty(c.pkgQty); err != nil {
		return a, err
	}
	a.Package.Unit, err = t.unit(c.pkgUnit)
	return a, err
}
