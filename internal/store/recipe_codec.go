package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// rangeSep separates the bounds of a stored range.
const rangeSep = ".."

// ratText is the exact reduced fraction "n/d", which quantity.ParseRat reads
// back without loss.
func ratText(r quantity.Rat) string {
	return r.Num().String() + "/" + r.Denom().String()
}

// encodeQty returns the stored text of q: nil for none, "n/d" for an exact
// value, "n/d..n/d" for a range. A negative bound could not be read back, so
// it is refused here rather than at the next read.
func encodeQty(q quantity.Quantity) (*string, error) {
	if q.IsAbsent() {
		return nil, nil //nolint:nilnil // nil text is the stored form of "no quantity"
	}
	if q.Min().Sign() < 0 || q.Max().Sign() < 0 {
		return nil, fmt.Errorf("%w: negative quantity %s", ErrInvalid, q)
	}
	s := ratText(q.Min())
	if q.IsRange() {
		s += rangeSep + ratText(q.Max())
	}
	return &s, nil
}

// decodeQty is the inverse of encodeQty.
func decodeQty(s *string) (quantity.Quantity, error) {
	if s == nil {
		return quantity.Absent(), nil
	}
	lo, hi, isRange := strings.Cut(*s, rangeSep)
	loR, err := quantity.ParseRat(lo)
	if err != nil {
		return quantity.Quantity{}, fmt.Errorf("stored quantity %q: %w", *s, err)
	}
	if !isRange {
		return quantity.Exact(loR), nil
	}
	hiR, err := quantity.ParseRat(hi)
	if err != nil {
		return quantity.Quantity{}, fmt.Errorf("stored quantity %q: %w", *s, err)
	}
	q, err := quantity.NewRange(loR, hiR)
	if err != nil {
		return quantity.Quantity{}, fmt.Errorf("stored quantity %q: %w", *s, err)
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

// id returns the reference stored for u; the zero unit is NULL.
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

// unit resolves a stored reference.
func (t *unitTable) unit(id sql.NullInt64) (quantity.Unit, error) {
	if !id.Valid {
		return quantity.Unit{}, nil
	}
	u, ok := t.units[id.Int64]
	if !ok {
		return quantity.Unit{}, fmt.Errorf("%w: unit id %d", ErrUnknownReference, id.Int64)
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
