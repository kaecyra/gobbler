package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kaecyra/gobbler/internal/catalog/catalogdb"
)

// Equipment is a piece of kitchen equipment ("skillet", "stand mixer") with
// its aliases. Name and aliases are stored trimmed and lower-case.
type Equipment struct {
	ID       int64
	Name     string
	Aliases  []string
	Reviewed bool
	// Sized marks equipment with a size (pans, tins, dishes, pots). The
	// operator sets it by hand during catalog review and ingestion never does;
	// a scaled recipe view shows the larger-pan hint on it (ADR-00019).
	Sized bool
}

func (e *Equipment) prepare() error {
	e.Name = normalize(e.Name)
	if err := validateName(e.Name); err != nil {
		return err
	}
	e.Aliases = normalizeAliases(e.Name, e.Aliases)
	return nil
}

func equipmentFrom(r catalogdb.Equipment) Equipment {
	return Equipment{ID: r.ID, Name: r.Name, Reviewed: r.Reviewed != 0, Sized: r.Sized != 0}
}

// CreateEquipment stores new equipment and returns it with its id.
func (s *Store) CreateEquipment(ctx context.Context, in Equipment) (Equipment, error) {
	if err := in.prepare(); err != nil {
		return Equipment{}, err
	}
	err := s.withTx(ctx, func(q *catalogdb.Queries, _ *sql.Tx) (err error) {
		if err := kindEquipment.checkFree(ctx, q, 0, in.Name, in.Aliases); err != nil {
			return err
		}
		in.ID, err = q.CreateEquipment(ctx, catalogdb.CreateEquipmentParams{Name: in.Name, Reviewed: boolInt(in.Reviewed), Sized: boolInt(in.Sized)})
		if err != nil {
			return wrapWrite("create equipment "+in.Name, err)
		}
		return kindEquipment.replaceAliases(ctx, q, in.ID, in.Aliases)
	})
	if err != nil {
		return Equipment{}, err
	}
	return in, nil
}

// UpdateEquipment replaces every field of the equipment with in.ID.
func (s *Store) UpdateEquipment(ctx context.Context, in Equipment) error {
	if err := in.prepare(); err != nil {
		return err
	}
	return s.withTx(ctx, func(q *catalogdb.Queries, _ *sql.Tx) error {
		if err := kindEquipment.checkFree(ctx, q, in.ID, in.Name, in.Aliases); err != nil {
			return err
		}
		n, err := q.UpdateEquipment(ctx, catalogdb.UpdateEquipmentParams{Name: in.Name, Reviewed: boolInt(in.Reviewed), Sized: boolInt(in.Sized), ID: in.ID})
		if err != nil {
			return wrapWrite(fmt.Sprintf("update equipment %d", in.ID), err)
		}
		if err := requireAffected(n, "equipment", in.ID); err != nil {
			return err
		}
		return kindEquipment.replaceAliases(ctx, q, in.ID, in.Aliases)
	})
}

// GetEquipment returns the equipment with the given id, or ErrNotFound.
func (s *Store) GetEquipment(ctx context.Context, id int64) (Equipment, error) {
	return getEquipment(ctx, s.q, id)
}

func getEquipment(ctx context.Context, q *catalogdb.Queries, id int64) (Equipment, error) {
	row, err := q.GetEquipment(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Equipment{}, fmt.Errorf("%w: equipment %d", ErrNotFound, id)
	}
	if err != nil {
		return Equipment{}, fmt.Errorf("read equipment %d: %w", id, err)
	}
	e := equipmentFrom(row)
	if e.Aliases, err = kindEquipment.aliasesOf(ctx, q, id); err != nil {
		return Equipment{}, err
	}
	return e, nil
}

// ListEquipment returns all equipment ordered by name.
func (s *Store) ListEquipment(ctx context.Context) ([]Equipment, error) {
	rows, err := s.q.ListEquipment(ctx)
	return s.withEquipmentAliases(ctx, rows, err)
}

// ListUnreviewedEquipment returns equipment awaiting review, ordered by name.
func (s *Store) ListUnreviewedEquipment(ctx context.Context) ([]Equipment, error) {
	rows, err := s.q.ListUnreviewedEquipment(ctx)
	return s.withEquipmentAliases(ctx, rows, err)
}

// withEquipmentAliases converts the rows of a list query, which failed with
// err if it is set, and attaches every alias in one further query.
func (s *Store) withEquipmentAliases(ctx context.Context, rows []catalogdb.Equipment, err error) ([]Equipment, error) {
	if err != nil {
		return nil, fmt.Errorf("list equipment: %w", err)
	}
	var out []Equipment
	for _, r := range rows {
		out = append(out, equipmentFrom(r))
	}
	aliases, err := kindEquipment.aliasesByID(ctx, s.q)
	if err != nil {
		return nil, err
	}
	for n := range out {
		out[n].Aliases = aliases[out[n].ID]
	}
	return out, nil
}

// MatchEquipment finds the equipment whose name or alias equals term, ignoring
// case and tolerating plurals. Not found is (Equipment{}, false, nil).
func (s *Store) MatchEquipment(ctx context.Context, term string) (Equipment, bool, error) {
	id, ok, err := kindEquipment.match(ctx, s.q, term)
	if err != nil || !ok {
		return Equipment{}, false, err
	}
	e, err := getEquipment(ctx, s.q, id)
	if err != nil {
		return Equipment{}, false, err
	}
	return e, true, nil
}
