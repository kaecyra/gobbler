package picture

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/picture/picturedb"
)

// Reaper deletes the picture objects of what is being deleted (ADR-00039,
// Storing). The recipe and household stores call it: they collect the keys
// inside the delete's transaction, before the rows that name them are gone,
// and remove the objects after it commits. The store packages cannot import
// this one, so each declares the part it calls.
type Reaper struct {
	media media.Store
	log   *slog.Logger
}

// NewReaper returns a Reaper over the media store. log may be nil.
func NewReaper(m media.Store, log *slog.Logger) *Reaper {
	if log == nil {
		log = slog.Default()
	}
	return &Reaper{media: m, log: log}
}

// CollectRecipe returns the keys of recipe id's picture and of the candidates
// of the proposals that name it. Call it inside the transaction that deletes
// the recipe, before the delete.
func (r *Reaper) CollectRecipe(ctx context.Context, tx *sql.Tx, householdID, id int64) ([]string, error) {
	keys, err := picturedb.New(tx).ListRecipeObjectKeys(ctx, picturedb.ListRecipeObjectKeysParams{Recipe: id, Household: householdID})
	if err != nil {
		return nil, fmt.Errorf("collect picture keys of recipe %d: %w", id, err)
	}
	return keys, nil
}

// CollectHousehold returns the keys of every picture and draft candidate of the
// household. Call it inside the transaction that deletes the household, before
// the delete.
func (r *Reaper) CollectHousehold(ctx context.Context, tx *sql.Tx, householdID int64) ([]string, error) {
	keys, err := picturedb.New(tx).ListHouseholdObjectKeys(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("collect picture keys of household %d: %w", householdID, err)
	}
	return keys, nil
}

// Remove deletes the objects. A failed delete is logged and does not stop the
// others: what remains is unreachable bytes, and the deletion it follows has
// already committed.
func (r *Reaper) Remove(ctx context.Context, keys []string) {
	for _, k := range keys {
		if err := r.media.Delete(ctx, k); err != nil {
			r.log.WarnContext(ctx, "delete picture object after delete", "key", k, "error", err.Error())
		}
	}
}
