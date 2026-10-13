package shopping

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// The stored forms match the recipe lines' (migration 00002) and come from
// quantity's codec: a quantity is NULL for none, "n/d" for an exact value and
// "n/d..n/d" for a range; a unit is a reference to units(id).

func ratText(r quantity.Rat) string { return quantity.EncodeRat(r) }

func encodeQty(q quantity.Quantity) (sql.NullString, error) {
	s, ok, err := quantity.EncodeQty(q)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return sql.NullString{String: s, Valid: ok}, nil
}

func decodeQty(n sql.NullString) (quantity.Quantity, error) {
	if !n.Valid {
		return quantity.Absent(), nil
	}
	q, err := quantity.DecodeQty(n.String)
	if err != nil {
		return quantity.Quantity{}, fmt.Errorf("%w: %w", ErrCorrupt, err)
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
type unitTable struct{ *quantity.UnitTable }

func (s *Store) loadUnits(ctx context.Context) (*unitTable, error) {
	all, err := s.catalog.ListUnits(ctx)
	if err != nil {
		return nil, fmt.Errorf("load units: %w", err)
	}
	t := quantity.NewUnitTable()
	for _, u := range all {
		t.Add(u.ID, u.Unit)
	}
	return &unitTable{t}, nil
}

func (t *unitTable) id(u quantity.Unit) (sql.NullInt64, error) {
	id, err := t.ID(u)
	if err != nil {
		return sql.NullInt64{}, fmt.Errorf("%w: %w", ErrUnknownReference, err)
	}
	return sql.NullInt64{Int64: id, Valid: id != 0}, nil
}

func (t *unitTable) unit(id sql.NullInt64) (quantity.Unit, error) {
	if !id.Valid {
		return quantity.Unit{}, nil
	}
	u, err := t.Unit(id.Int64)
	if err != nil {
		return quantity.Unit{}, fmt.Errorf("%w: %w", ErrCorrupt, err)
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
