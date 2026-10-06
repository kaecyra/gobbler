package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// kind describes one family of named entries with an alias table: the table
// names are package constants, never caller input, so they are safe to splice
// into SQL.
type kind struct {
	label      string // for messages: "ingredient"
	table      string
	aliasTable string
	fk         string // alias table column pointing at table.id
}

var (
	kindIngredient = kind{"ingredient", "ingredients", "ingredient_aliases", "ingredient_id"}
	kindUnit       = kind{"unit", "units", "unit_aliases", "unit_id"}
	kindEquipment  = kind{"equipment", "equipment", "equipment_aliases", "equipment_id"}
)

// lookup returns the id of the entry whose name or alias is exactly term
// (already normalized). Names and aliases share one namespace, enforced by
// checkFree on write.
func (k kind) lookup(ctx context.Context, q queryer, term string) (int64, bool, error) {
	var id int64
	err := q.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT id FROM %[1]s WHERE name = ?1
		 UNION ALL SELECT %[3]s FROM %[2]s WHERE alias = ?1
		 ORDER BY 1 LIMIT 1`, k.table, k.aliasTable, k.fk), term).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("look up %s %q: %w", k.label, term, err)
	}
	return id, true, nil
}

// match resolves free text to an entry id, tolerating case, surrounding
// whitespace and plural/singular differences. An exact hit beats any
// inflected one. Not found is (0, false, nil).
func (k kind) match(ctx context.Context, q queryer, term string) (int64, bool, error) {
	for _, cand := range inflections(normalize(term)) {
		id, ok, err := k.lookup(ctx, q, cand)
		if err != nil || ok {
			return id, ok, err
		}
	}
	return 0, false, nil
}

// checkFree returns ErrConflict if name or any alias is already used by an
// entry other than selfID (0 for a new entry).
func (k kind) checkFree(ctx context.Context, q queryer, selfID int64, name string, aliases []string) error {
	for _, term := range append([]string{name}, aliases...) {
		id, ok, err := k.lookup(ctx, q, term)
		if err != nil {
			return err
		}
		if ok && id != selfID {
			return fmt.Errorf("%w: %s %q is already used by %s %d", ErrConflict, k.label, term, k.label, id)
		}
	}
	return nil
}

// replaceAliases sets the alias list of id to aliases.
func (k kind) replaceAliases(ctx context.Context, tx execer, id int64, aliases []string) error {
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, k.aliasTable, k.fk), id); err != nil {
		return fmt.Errorf("clear %s aliases: %w", k.label, err)
	}
	for _, a := range aliases {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (alias, %s) VALUES (?, ?)`, k.aliasTable, k.fk), a, id); err != nil {
			return fmt.Errorf("add %s alias %q: %w", k.label, a, err)
		}
	}
	return nil
}

// aliases returns the aliases of id, sorted.
func (k kind) aliases(ctx context.Context, q queryer, id int64) ([]string, error) {
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`SELECT alias FROM %s WHERE %s = ? ORDER BY alias`, k.aliasTable, k.fk), id)
	if err != nil {
		return nil, fmt.Errorf("read %s aliases: %w", k.label, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, fmt.Errorf("scan %s alias: %w", k.label, err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// delete removes the entry row; its aliases go with it by cascade.
func (k kind) delete(ctx context.Context, tx execer, id int64) error {
	res, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, k.table), id)
	if err != nil {
		return fmt.Errorf("delete %s %d: %w", k.label, id, err)
	}
	return requireAffected(res, k.label, id)
}

func requireAffected(res sql.Result, label string, id int64) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("check %s %d update: %w", label, id, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s %d", ErrNotFound, label, id)
	}
	return nil
}
