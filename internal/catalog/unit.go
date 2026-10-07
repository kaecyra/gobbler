package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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

func (u *Unit) prepare() (factor any, err error) {
	u.Unit.Name = normalize(u.Unit.Name)
	if err := validateName(u.Unit.Name); err != nil {
		return nil, err
	}
	switch u.Unit.System {
	case quantity.SystemUS, quantity.SystemMetric, quantity.SystemNone:
	default:
		return nil, fmt.Errorf("%w: unit %q has unknown system %q", ErrInvalid, u.Unit.Name, u.Unit.System)
	}
	switch u.Unit.Dimension {
	case quantity.DimMass, quantity.DimVolume, quantity.DimCount:
		if err := u.Unit.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
		}
		factor = u.Unit.Factor.String()
	case quantity.DimOther:
		factor = nil
	default:
		return nil, fmt.Errorf("%w: unit %q has unknown dimension %q", ErrInvalid, u.Unit.Name, u.Unit.Dimension)
	}
	u.Aliases = normalizeAliases(u.Unit.Name, u.Aliases)
	return factor, nil
}

func scanUnit(sc interface{ Scan(...any) error }) (Unit, error) {
	var (
		u      Unit
		sys    string
		dim    string
		factor sql.NullString
		rev    int
	)
	if err := sc.Scan(&u.ID, &u.Unit.Name, &sys, &dim, &factor, &rev); err != nil {
		return Unit{}, err
	}
	u.Unit.System, u.Unit.Dimension, u.Reviewed = quantity.System(sys), quantity.Dimension(dim), rev != 0
	if factor.Valid {
		f, err := quantity.ParseRat(factor.String)
		if err != nil {
			return Unit{}, fmt.Errorf("unit %q has unreadable factor %q: %w", u.Unit.Name, factor.String, err)
		}
		u.Unit.Factor = f
	}
	if err := u.Unit.Validate(); err != nil {
		return Unit{}, err
	}
	return u, nil
}

const unitColumns = `id, name, system, dimension, factor, reviewed`

// CreateUnit stores a new unit and returns it with its id.
func (s *Store) CreateUnit(ctx context.Context, in Unit) (Unit, error) {
	factor, err := in.prepare()
	if err != nil {
		return Unit{}, err
	}
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if err := kindUnit.checkFree(ctx, tx, 0, in.Unit.Name, in.Aliases); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO units (name, system, dimension, factor, reviewed) VALUES (?, ?, ?, ?, ?)`,
			in.Unit.Name, string(in.Unit.System), string(in.Unit.Dimension), factor, boolInt(in.Reviewed))
		if err != nil {
			return wrapWrite("create unit "+in.Unit.Name, err)
		}
		if in.ID, err = res.LastInsertId(); err != nil {
			return fmt.Errorf("read new unit id: %w", err)
		}
		return kindUnit.replaceAliases(ctx, tx, in.ID, in.Aliases)
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
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := kindUnit.checkFree(ctx, tx, in.ID, in.Unit.Name, in.Aliases); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE units SET name = ?, system = ?, dimension = ?, factor = ?, reviewed = ? WHERE id = ?`,
			in.Unit.Name, string(in.Unit.System), string(in.Unit.Dimension), factor, boolInt(in.Reviewed), in.ID)
		if err != nil {
			return wrapWrite(fmt.Sprintf("update unit %d", in.ID), err)
		}
		if err := requireAffected(res, "unit", in.ID); err != nil {
			return err
		}
		return kindUnit.replaceAliases(ctx, tx, in.ID, in.Aliases)
	})
}

// GetUnit returns the unit with the given id, or ErrNotFound.
func (s *Store) GetUnit(ctx context.Context, id int64) (Unit, error) {
	return getUnit(ctx, s.db, id)
}

func getUnit(ctx context.Context, q queryer, id int64) (Unit, error) {
	u, err := scanUnit(q.QueryRowContext(ctx, `SELECT `+unitColumns+` FROM units WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Unit{}, fmt.Errorf("%w: unit %d", ErrNotFound, id)
	}
	if err != nil {
		return Unit{}, fmt.Errorf("read unit %d: %w", id, err)
	}
	if u.Aliases, err = kindUnit.aliases(ctx, q, id); err != nil {
		return Unit{}, err
	}
	return u, nil
}

// ListUnits returns every unit ordered by name.
func (s *Store) ListUnits(ctx context.Context) ([]Unit, error) {
	return s.listUnits(ctx, ``)
}

// ListUnreviewedUnits returns units awaiting review, ordered by name.
func (s *Store) ListUnreviewedUnits(ctx context.Context) ([]Unit, error) {
	return s.listUnits(ctx, `WHERE reviewed = 0`)
}

func (s *Store) listUnits(ctx context.Context, where string) ([]Unit, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+unitColumns+` FROM units `+where+` ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Unit
	for rows.Next() {
		u, err := scanUnit(rows)
		if err != nil {
			return nil, fmt.Errorf("scan unit: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}
	_ = rows.Close() // free the connection before the alias read
	aliases, err := kindUnit.allAliases(ctx, s.db)
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
	id, ok, err := kindUnit.match(ctx, s.db, term)
	if err != nil || !ok {
		return Unit{}, false, err
	}
	u, err := getUnit(ctx, s.db, id)
	if err != nil {
		return Unit{}, false, err
	}
	return u, true, nil
}
