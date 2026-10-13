package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// encodeQty returns the stored text of q: nil for none, "n/d" for an exact
// value, "n/d..n/d" for a range (quantity.EncodeQty).
func encodeQty(q quantity.Quantity) (*string, error) {
	s, ok, err := quantity.EncodeQty(q)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if !ok {
		return nil, nil //nolint:nilnil // nil text is the stored form of "no quantity"
	}
	return &s, nil
}

// decodeQty is the inverse of encodeQty.
func decodeQty(s *string) (quantity.Quantity, error) {
	if s == nil {
		return quantity.Absent(), nil
	}
	q, err := quantity.DecodeQty(*s)
	if err != nil {
		return quantity.Quantity{}, fmt.Errorf("stored %w", err)
	}
	return q, nil
}

func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func stringPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	return &n.String
}

func nullInt(p *int) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*p), Valid: true}
}

func intPtr(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

// unitTable is the catalog's units, loaded once per operation. Recipes hold
// full quantity.Units; the database holds a reference by id.
type unitTable struct {
	ids   map[string]int64
	units map[int64]quantity.Unit
}

func loadUnits(ctx context.Context, q *storedb.Queries) (*unitTable, error) {
	rows, err := q.ListUnits(ctx)
	if err != nil {
		return nil, fmt.Errorf("load units: %w", err)
	}
	t := &unitTable{ids: map[string]int64{}, units: map[int64]quantity.Unit{}}
	for _, r := range rows {
		u := quantity.Unit{Name: r.Name, System: quantity.System(r.System), Dimension: quantity.Dimension(r.Dimension)}
		if r.Factor.Valid {
			if u.Factor, err = quantity.ParseRat(r.Factor.String); err != nil {
				return nil, fmt.Errorf("unit %q factor: %w", r.Name, err)
			}
		}
		t.ids[r.Name] = r.ID
		t.units[r.ID] = u
	}
	return t, nil
}

// table views the maps as the shared quantity.UnitTable.
func (t *unitTable) table() *quantity.UnitTable { return quantity.UnitTableOver(t.ids, t.units) }

// id returns the reference stored for u; the zero unit is NULL.
func (t *unitTable) id(u quantity.Unit) (sql.NullInt64, error) {
	id, err := t.table().ID(u)
	if err != nil {
		return sql.NullInt64{}, fmt.Errorf("%w: %w", ErrUnknownReference, err)
	}
	return sql.NullInt64{Int64: id, Valid: id != 0}, nil
}

// unit resolves a stored reference.
func (t *unitTable) unit(id sql.NullInt64) (quantity.Unit, error) {
	if !id.Valid {
		return quantity.Unit{}, nil
	}
	u, err := t.table().Unit(id.Int64)
	if err != nil {
		return quantity.Unit{}, fmt.Errorf("%w: %w", ErrUnknownReference, err)
	}
	return u, nil
}

// refID returns the unit id stored in a revision document; the zero unit is 0.
func (t *unitTable) refID(u quantity.Unit) (int64, error) {
	id, err := t.id(u)
	return id.Int64, err
}

// byRef resolves a unit id stored in a revision document; 0 is no unit.
func (t *unitTable) byRef(id int64) (quantity.Unit, error) {
	return t.unit(sql.NullInt64{Int64: id, Valid: id != 0})
}
