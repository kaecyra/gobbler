package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kaecyra/gobbler/internal/catalog/catalogdb"
	"github.com/kaecyra/gobbler/internal/quantity"
)

// Unit is a persisted unit: the quantity.Unit (canonical name, system,
// dimension and exact factor to the base unit, ADR-00005) plus its aliases
// ("tablespoon", "tbs") and review flag. Convertible units seeded by the
// migration agree with quantity's built-in table.
type Unit struct {
	ID       int64
	Unit     quantity.Unit
	Aliases  []string
	Reviewed bool
}

func (u *Unit) prepare() (factor sql.NullString, err error) {
	u.Unit.Name = normalize(u.Unit.Name)
	if err := validateName(u.Unit.Name); err != nil {
		return sql.NullString{}, err
	}
	switch u.Unit.System {
	case quantity.SystemUS, quantity.SystemMetric, quantity.SystemNone:
	default:
		return sql.NullString{}, fmt.Errorf("%w: unit %q has unknown system %q", ErrInvalid, u.Unit.Name, u.Unit.System)
	}
	switch u.Unit.Dimension {
	case quantity.DimMass, quantity.DimVolume, quantity.DimCount:
		if err := u.Unit.Validate(); err != nil {
			return sql.NullString{}, fmt.Errorf("%w: %w", ErrInvalid, err)
		}
		factor = sql.NullString{String: u.Unit.Factor.String(), Valid: true}
	case quantity.DimOther:
		factor = sql.NullString{}
	default:
		return sql.NullString{}, fmt.Errorf("%w: unit %q has unknown dimension %q", ErrInvalid, u.Unit.Name, u.Unit.Dimension)
	}
	u.Aliases = normalizeAliases(u.Unit.Name, u.Aliases)
	return factor, nil
}

func unitFrom(r catalogdb.Unit) (Unit, error) {
	u := Unit{ID: r.ID, Reviewed: r.Reviewed != 0}
	u.Unit.Name, u.Unit.System, u.Unit.Dimension = r.Name, quantity.System(r.System), quantity.Dimension(r.Dimension)
	if r.Factor.Valid {
		f, err := quantity.ParseRat(r.Factor.String)
		if err != nil {
			return Unit{}, fmt.Errorf("unit %q has unreadable factor %q: %w", u.Unit.Name, r.Factor.String, err)
		}
		u.Unit.Factor = f
	}
	if err := u.Unit.Validate(); err != nil {
		return Unit{}, err
	}
	return u, nil
}

// CreateUnit stores a new unit and returns it with its id.
func (s *Store) CreateUnit(ctx context.Context, in Unit) (Unit, error) {
	factor, err := in.prepare()
	if err != nil {
		return Unit{}, err
	}
	err = s.withTx(ctx, func(q *catalogdb.Queries, _ *sql.Tx) (err error) {
		if err := kindUnit.checkFree(ctx, q, 0, in.Unit.Name, in.Aliases); err != nil {
			return err
		}
		in.ID, err = q.CreateUnit(ctx, catalogdb.CreateUnitParams{
			Name: in.Unit.Name, System: string(in.Unit.System), Dimension: string(in.Unit.Dimension),
			Factor: factor, Reviewed: boolInt(in.Reviewed),
		})
		if err != nil {
			return wrapWrite("create unit "+in.Unit.Name, err)
		}
		return kindUnit.replaceAliases(ctx, q, in.ID, in.Aliases)
	})
	if err != nil {
		return Unit{}, err
	}
	return in, nil
}

// UpdateUnit replaces every field of the unit with in.ID, including aliases.
func (s *Store) UpdateUnit(ctx context.Context, in Unit) error {
	factor, err := in.prepare()
	if err != nil {
		return err
	}
	return s.withTx(ctx, func(q *catalogdb.Queries, _ *sql.Tx) error {
		if err := kindUnit.checkFree(ctx, q, in.ID, in.Unit.Name, in.Aliases); err != nil {
			return err
		}
		n, err := q.UpdateUnit(ctx, catalogdb.UpdateUnitParams{
			Name: in.Unit.Name, System: string(in.Unit.System), Dimension: string(in.Unit.Dimension),
			Factor: factor, Reviewed: boolInt(in.Reviewed), ID: in.ID,
		})
		if err != nil {
			return wrapWrite(fmt.Sprintf("update unit %d", in.ID), err)
		}
		if err := requireAffected(n, "unit", in.ID); err != nil {
			return err
		}
		return kindUnit.replaceAliases(ctx, q, in.ID, in.Aliases)
	})
}

// GetUnit returns the unit with the given id, or ErrNotFound.
func (s *Store) GetUnit(ctx context.Context, id int64) (Unit, error) {
	return getUnit(ctx, s.q, id)
}

func getUnit(ctx context.Context, q *catalogdb.Queries, id int64) (Unit, error) {
	row, err := q.GetUnit(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Unit{}, fmt.Errorf("%w: unit %d", ErrNotFound, id)
	}
	if err != nil {
		return Unit{}, fmt.Errorf("read unit %d: %w", id, err)
	}
	u, err := unitFrom(row)
	if err != nil {
		return Unit{}, fmt.Errorf("read unit %d: %w", id, err)
	}
	if u.Aliases, err = kindUnit.aliasesOf(ctx, q, id); err != nil {
		return Unit{}, err
	}
	return u, nil
}

// ListUnits returns every unit ordered by name.
func (s *Store) ListUnits(ctx context.Context) ([]Unit, error) {
	rows, err := s.q.ListUnits(ctx)
	return s.withUnitAliases(ctx, rows, err)
}

// ListUnreviewedUnits returns units awaiting review, ordered by name.
func (s *Store) ListUnreviewedUnits(ctx context.Context) ([]Unit, error) {
	rows, err := s.q.ListUnreviewedUnits(ctx)
	return s.withUnitAliases(ctx, rows, err)
}

// withUnitAliases converts the rows of a list query, which failed with err if
// it is set, and attaches every alias in one further query.
func (s *Store) withUnitAliases(ctx context.Context, rows []catalogdb.Unit, err error) ([]Unit, error) {
	if err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}
	var out []Unit
	for _, r := range rows {
		u, err := unitFrom(r)
		if err != nil {
			return nil, fmt.Errorf("scan unit: %w", err)
		}
		out = append(out, u)
	}
	aliases, err := kindUnit.aliasesByID(ctx, s.q)
	if err != nil {
		return nil, err
	}
	for n := range out {
		out[n].Aliases = aliases[out[n].ID]
	}
	return out, nil
}

// MatchUnit finds the unit whose canonical name or alias equals term, ignoring
// case and tolerating plurals. Not found is (Unit{}, false, nil).
func (s *Store) MatchUnit(ctx context.Context, term string) (Unit, bool, error) {
	id, ok, err := kindUnit.match(ctx, s.q, term)
	if err != nil || !ok {
		return Unit{}, false, err
	}
	u, err := getUnit(ctx, s.q, id)
	if err != nil {
		return Unit{}, false, err
	}
	return u, true, nil
}
