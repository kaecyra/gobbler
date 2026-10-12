package shopping

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/shopping/shoppingdb"
)

// This file starts the persistence half of the package (ADR-00009): lists,
// recipe entries, manual items, the pantry and check state in SQLite, with the
// SQL in queries/ generated into shoppingdb (ADR-00014). Loading a list reads
// the stored entries, manual items and pantry and hands them to Generate; no
// merge logic lives here.
//
// Check state is keyed by ItemKey, the identity of a generated item that
// survives regeneration: a canonical ingredient, a manual free-text item, or
// an unresolved recipe line. A check therefore stays when an item's amount
// changes, and is dropped by Regenerate when the item no longer exists.

// Sentinel errors returned (wrapped) by the Store.
var (
	// ErrNotFound is returned when a list, entry or manual item does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInvalid is returned for input the store refuses, such as an empty list
	// name or a malformed item key.
	ErrInvalid = errors.New("invalid input")
	// ErrUnknownReference is returned when a write names a recipe, ingredient or
	// unit that does not exist.
	ErrUnknownReference = errors.New("reference to an unknown recipe, ingredient or unit")
	// ErrDuplicate is returned when a recipe is already an entry of the list.
	ErrDuplicate = errors.New("already on the list")
	// ErrCorrupt is returned when stored rows cannot be read back, such as an
	// unparseable quantity. It means the database is broken, not that the
	// caller asked for something absent.
	ErrCorrupt = errors.New("stored data is inconsistent")
)

// RecipeSource reads recipes. *store.Store satisfies it.
type RecipeSource interface {
	Get(ctx context.Context, id int64) (recipe.Recipe, error)
}

// CatalogSource reads the catalog facts generation needs. *catalog.Store
// satisfies it.
type CatalogSource interface {
	GetIngredient(ctx context.Context, id int64) (catalog.Ingredient, error)
	ListUnits(ctx context.Context) ([]catalog.Unit, error)
}

// Change says that something about a list changed: its check state, its
// entries, its manual items, its name, or the pantry it is generated against.
type Change struct {
	ListID int64
}

// Store reads and writes shopping lists. It is safe for concurrent use.
type Store struct {
	db      *sql.DB
	q       *shoppingdb.Queries
	recipes RecipeSource
	catalog CatalogSource
	now     func() time.Time
	// onChange holds the handlers of OnChange; it is written only at start-up.
	onChange []func(Change)
}

// New returns a Store over d, which must already be migrated.
func New(d *sql.DB, recipes RecipeSource, cat CatalogSource) *Store {
	return &Store{db: d, q: shoppingdb.New(d), recipes: recipes, catalog: cat, now: time.Now}
}

// OnChange registers a handler called after a change to a list has been
// committed, once per affected list. It is the seam the live view uses to learn
// that check state changed (ADR-00009 obligation 3). A handler runs on the
// goroutine of the writer, so it must return quickly and never block; hand the
// change to a channel. Register handlers at start-up, before the store is
// shared between goroutines.
func (s *Store) OnChange(h func(Change)) {
	s.onChange = append(s.onChange, h)
}

func (s *Store) notify(listIDs ...int64) {
	for _, id := range listIDs {
		for _, h := range s.onChange {
			h(Change{ListID: id})
		}
	}
}

func (s *Store) stamp() string { return s.now().UTC().Format(time.RFC3339Nano) }

// withTx runs fn in one transaction (db.WithTx). A caller that reads before it
// writes makes its first statement a write, such as lockList, so the
// transaction takes the write lock up front instead of failing to upgrade a
// stale read snapshot.
func (s *Store) withTx(ctx context.Context, fn func(q *shoppingdb.Queries) error) error {
	return db.WithTx(ctx, s.db, s.q, func(q *shoppingdb.Queries, _ *sql.Tx) error { return fn(q) })
}

// lockList takes the write lock with a no-op write to list id, and returns
// ErrNotFound when it does not exist.
func lockList(ctx context.Context, q *shoppingdb.Queries, id int64) error {
	n, err := q.LockList(ctx, id)
	if err != nil {
		return fmt.Errorf("lock list %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: list %d", ErrNotFound, id)
	}
	return nil
}

// wrapWrite wraps a failed write with what it was doing. A foreign key
// violation is a reference to something that does not exist (the list itself
// was checked first by lockList); a unique violation is a duplicate entry.
func wrapWrite(what string, err error) error {
	if err == nil {
		return nil
	}
	wrapped := db.WrapWrite(what, err, ErrUnknownReference, db.ConstraintForeignKey)
	if errors.Is(wrapped, ErrUnknownReference) {
		return wrapped
	}
	return db.WrapWrite(what, err, ErrDuplicate, db.ConstraintUnique)
}

// rowOrNotFound turns sql.ErrNoRows from a RETURNING query into ErrNotFound.
func rowOrNotFound(what string, id int64, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s %d", ErrNotFound, what, id)
	}
	if err != nil {
		return fmt.Errorf("%s %d: %w", what, id, err)
	}
	return nil
}
