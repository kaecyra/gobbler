package shopping

import (
	"context"
	"fmt"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/shopping/shoppingdb"
)

// SetChecked sets or clears the check on one item of a list. It states the
// wanted result rather than flipping the current one, so two devices or two
// goroutines acting on the same item end in one of the two states they asked
// for, never in an error or a duplicate. Setting what is already set changes
// nothing and notifies nobody. Setting a check on a missing list returns
// ErrNotFound; clearing one there is a no-op that returns nil, since the wanted
// state already holds. A key KeyOf could not have produced returns ErrInvalid. A key naming no item is
// stored all the same and dropped by the next Regenerate.
func (s *Store) SetChecked(ctx context.Context, listID int64, key ItemKey, checked bool) error {
	if !key.valid() {
		return fmt.Errorf("%w: item key %q", ErrInvalid, key)
	}
	var changed int64
	err := s.withTx(ctx, func(q *shoppingdb.Queries) error {
		var err error
		if checked {
			changed, err = q.SetChecked(ctx, shoppingdb.SetCheckedParams{ListID: listID, ItemKey: string(key), CheckedAt: s.stamp()})
			// The only foreign key is the list.
			return db.WrapWrite(fmt.Sprintf("check %q on list %d", key, listID), err, ErrNotFound, db.ConstraintForeignKey)
		}
		changed, err = q.ClearChecked(ctx, shoppingdb.ClearCheckedParams{ListID: listID, ItemKey: string(key)})
		if err != nil {
			return fmt.Errorf("uncheck %q on list %d: %w", key, listID, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if changed > 0 {
		s.notify(listID)
	}
	return nil
}
