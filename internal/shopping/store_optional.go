package shopping

import (
	"context"
	"fmt"

	"github.com/kaecyra/gobbler/internal/shopping/shoppingdb"
)

// SetIncludeOptional sets whether list id keeps the optional lines of its
// recipes (Input.IncludeOptional). A list drops them until this is set. It
// states the wanted result, so setting what is already set changes nothing and
// notifies nobody. It returns ErrNotFound for a missing list.
//
// It does not drop check state: a check on an item that only an optional line
// brought stays stored, and comes back if the lines are included again.
// Regenerate drops it, as for any item that does not exist.
func (s *Store) SetIncludeOptional(ctx context.Context, id int64, include bool) error {
	var v int64
	if include {
		v = 1
	}
	hid, err := scope(ctx)
	if err != nil {
		return err
	}
	var changed int64
	err = s.withTx(ctx, func(q *shoppingdb.Queries) error {
		if err := lockList(ctx, q, hid, id); err != nil {
			return err
		}
		var err error
		changed, err = q.SetIncludeOptional(ctx, shoppingdb.SetIncludeOptionalParams{IncludeOptional: v, ID: id, HouseholdID: hid})
		if err != nil {
			return fmt.Errorf("set include optional on list %d: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if changed > 0 {
		s.notify(id)
	}
	return nil
}
