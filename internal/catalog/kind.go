package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kaecyra/gobbler/internal/catalog/catalogdb"
)

// kind describes one family of named entries with an alias table. sqlc
// generates one set of queries per table, so a kind binds the generated
// functions of its own tables (as method expressions, so the receiver comes
// first); the logic over them is shared.
type kind struct {
	label        string // for messages: "ingredient"
	lookup       func(q *catalogdb.Queries, ctx context.Context, term string) (int64, error)
	aliases      func(q *catalogdb.Queries, ctx context.Context, id int64) ([]string, error)
	allAliases   func(q *catalogdb.Queries, ctx context.Context) ([]aliasRow, error)
	clearAliases func(q *catalogdb.Queries, ctx context.Context, id int64) error
	addAlias     func(q *catalogdb.Queries, ctx context.Context, alias string, id int64) error
}

// aliasRow is one alias and the id of the entry it names.
type aliasRow struct {
	id    int64
	alias string
}

var kindUnit = kind{
	label:        "unit",
	lookup:       (*catalogdb.Queries).LookupUnitID,
	aliases:      (*catalogdb.Queries).ListUnitAliases,
	clearAliases: (*catalogdb.Queries).DeleteUnitAliases,
	addAlias: func(q *catalogdb.Queries, ctx context.Context, alias string, id int64) error {
		return q.AddUnitAlias(ctx, catalogdb.AddUnitAliasParams{Alias: alias, UnitID: id})
	},
	allAliases: func(q *catalogdb.Queries, ctx context.Context) ([]aliasRow, error) {
		rows, err := q.ListAllUnitAliases(ctx)
		out := make([]aliasRow, len(rows))
		for i, r := range rows {
			out[i] = aliasRow{r.UnitID, r.Alias}
		}
		return out, err
	},
}

var kindEquipment = kind{
	label:        "equipment",
	lookup:       (*catalogdb.Queries).LookupEquipmentID,
	aliases:      (*catalogdb.Queries).ListEquipmentAliases,
	clearAliases: (*catalogdb.Queries).DeleteEquipmentAliases,
	addAlias: func(q *catalogdb.Queries, ctx context.Context, alias string, id int64) error {
		return q.AddEquipmentAlias(ctx, catalogdb.AddEquipmentAliasParams{Alias: alias, EquipmentID: id})
	},
	allAliases: func(q *catalogdb.Queries, ctx context.Context) ([]aliasRow, error) {
		rows, err := q.ListAllEquipmentAliases(ctx)
		out := make([]aliasRow, len(rows))
		for i, r := range rows {
			out[i] = aliasRow{r.EquipmentID, r.Alias}
		}
		return out, err
	},
}

// find returns the id of the entry whose name or alias is exactly term
// (already normalized). Names and aliases share one namespace, enforced by
// checkFree on write.
func (k kind) find(ctx context.Context, q *catalogdb.Queries, term string) (int64, bool, error) {
	id, err := k.lookup(q, ctx, term)
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
func (k kind) match(ctx context.Context, q *catalogdb.Queries, term string) (int64, bool, error) {
	for _, cand := range inflections(normalize(term)) {
		id, ok, err := k.find(ctx, q, cand)
		if err != nil || ok {
			return id, ok, err
		}
	}
	return 0, false, nil
}

// checkFree returns ErrConflict if name or any alias is already used by an
// entry other than selfID (0 for a new entry).
func (k kind) checkFree(ctx context.Context, q *catalogdb.Queries, selfID int64, name string, aliases []string) error {
	for _, term := range append([]string{name}, aliases...) {
		id, ok, err := k.find(ctx, q, term)
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
func (k kind) replaceAliases(ctx context.Context, q *catalogdb.Queries, id int64, aliases []string) error {
	if err := k.clearAliases(q, ctx, id); err != nil {
		return fmt.Errorf("clear %s aliases: %w", k.label, err)
	}
	for _, a := range aliases {
		if err := k.addAlias(q, ctx, a, id); err != nil {
			return fmt.Errorf("add %s alias %q: %w", k.label, a, err)
		}
	}
	return nil
}

// aliasesOf returns the aliases of id, sorted.
func (k kind) aliasesOf(ctx context.Context, q *catalogdb.Queries, id int64) ([]string, error) {
	out, err := k.aliases(q, ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read %s aliases: %w", k.label, err)
	}
	return out, nil
}

func requireAffected(n int64, label string, id int64) error {
	if n == 0 {
		return fmt.Errorf("%w: %s %d", ErrNotFound, label, id)
	}
	return nil
}

// aliasesByID returns the aliases of every entry of the kind, keyed by entry
// id and sorted, in one query.
func (k kind) aliasesByID(ctx context.Context, q *catalogdb.Queries) (map[int64][]string, error) {
	rows, err := k.allAliases(q, ctx)
	if err != nil {
		return nil, fmt.Errorf("read all %s aliases: %w", k.label, err)
	}
	out := map[int64][]string{}
	for _, r := range rows {
		out[r.id] = append(out[r.id], r.alias)
	}
	return out, nil
}
