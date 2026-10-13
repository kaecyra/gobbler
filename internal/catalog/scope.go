package catalog

import (
	"context"
	"database/sql"

	"github.com/kaecyra/gobbler/internal/household"
)

// scopeOf is the id of the household on the request context, or 0 outside a
// household member's request. The ingredient catalog is read and written in
// that scope (ADR-00018): a household sees the central layer plus its own
// overlay; with no household only the central layer is visible and a write
// goes to it. Seeding, the USDA import and the operator's promotion run
// there; every member's request carries a household.
func scopeOf(ctx context.Context) int64 {
	h, ok := household.FromContext(ctx)
	if !ok {
		return 0
	}
	return h.ID
}

// readScope is scopeOf as the parameter of a visibility query. Scope 0 matches
// no household, so it reads the central layer alone.
func readScope(scope int64) sql.NullInt64 { return sql.NullInt64{Int64: scope, Valid: true} }

// owner is the household_id a row written in scope carries: NULL (central)
// for scope 0.
func owner(scope int64) sql.NullInt64 { return sql.NullInt64{Int64: scope, Valid: scope != 0} }
