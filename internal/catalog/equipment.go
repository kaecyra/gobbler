package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Equipment is a piece of kitchen equipment ("skillet", "stand mixer") with
// its aliases. Name and aliases are stored trimmed and lower-case.
type Equipment struct {
	ID       int64
	Name     string
	Aliases  []string
	Reviewed bool
}

func (e *Equipment) prepare() error {
	e.Name = normalize(e.Name)
	if err := validateName(e.Name); err != nil {
		return err
	}
	e.Aliases = normalizeAliases(e.Name, e.Aliases)
	return nil
}

func scanEquipment(sc interface{ Scan(...any) error }) (Equipment, error) {
	var (
		e   Equipment
		rev int
	)
	if err := sc.Scan(&e.ID, &e.Name, &rev); err != nil {
		return Equipment{}, err
	}
	e.Reviewed = rev != 0
	return e, nil
}

// CreateEquipment stores new equipment and returns it with its id.
func (s *Store) CreateEquipment(ctx context.Context, in Equipment) (Equipment, error) {
	if err := in.prepare(); err != nil {
		return Equipment{}, err
	}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if err := kindEquipment.checkFree(ctx, tx, 0, in.Name, in.Aliases); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO equipment (name, reviewed) VALUES (?, ?)`, in.Name, boolInt(in.Reviewed))
		if err != nil {
			return wrapWrite("create equipment "+in.Name, err)
		}
		if in.ID, err = res.LastInsertId(); err != nil {
			return fmt.Errorf("read new equipment id: %w", err)
		}
		return kindEquipment.replaceAliases(ctx, tx, in.ID, in.Aliases)
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
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := kindEquipment.checkFree(ctx, tx, in.ID, in.Name, in.Aliases); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE equipment SET name = ?, reviewed = ? WHERE id = ?`, in.Name, boolInt(in.Reviewed), in.ID)
		if err != nil {
			return wrapWrite(fmt.Sprintf("update equipment %d", in.ID), err)
		}
		if err := requireAffected(res, "equipment", in.ID); err != nil {
			return err
		}
		return kindEquipment.replaceAliases(ctx, tx, in.ID, in.Aliases)
	})
}

// GetEquipment returns the equipment with the given id, or ErrNotFound.
func (s *Store) GetEquipment(ctx context.Context, id int64) (Equipment, error) {
	return getEquipment(ctx, s.db, id)
}

func getEquipment(ctx context.Context, q queryer, id int64) (Equipment, error) {
	e, err := scanEquipment(q.QueryRowContext(ctx, `SELECT id, name, reviewed FROM equipment WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Equipment{}, fmt.Errorf("%w: equipment %d", ErrNotFound, id)
	}
	if err != nil {
		return Equipment{}, fmt.Errorf("read equipment %d: %w", id, err)
	}
	if e.Aliases, err = kindEquipment.aliases(ctx, q, id); err != nil {
		return Equipment{}, err
	}
	return e, nil
}

// ListEquipment returns all equipment ordered by name.
func (s *Store) ListEquipment(ctx context.Context) ([]Equipment, error) {
	return s.listEquipment(ctx, ``)
}

// ListUnreviewedEquipment returns equipment awaiting review, ordered by name.
func (s *Store) ListUnreviewedEquipment(ctx context.Context) ([]Equipment, error) {
	return s.listEquipment(ctx, `WHERE reviewed = 0`)
}

func (s *Store) listEquipment(ctx context.Context, where string) ([]Equipment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, reviewed FROM equipment `+where+` ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list equipment: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Equipment
	for rows.Next() {
		e, err := scanEquipment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan equipment: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list equipment: %w", err)
	}
	_ = rows.Close() // free the connection before the alias read
	aliases, err := kindEquipment.allAliases(ctx, s.db)
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
	id, ok, err := kindEquipment.match(ctx, s.db, term)
	if err != nil || !ok {
		return Equipment{}, false, err
	}
	e, err := getEquipment(ctx, s.db, id)
	if err != nil {
		return Equipment{}, false, err
	}
	return e, true, nil
}
