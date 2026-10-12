package shopping

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/shopping/shoppingdb"
)

// ListInfo is a stored shopping list without its contents.
type ListInfo struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

func listInfoFrom(r shoppingdb.ShoppingList) (ListInfo, error) {
	t, err := time.Parse(time.RFC3339Nano, r.CreatedAt)
	if err != nil {
		return ListInfo{}, fmt.Errorf("%w: list %d created_at %q: %w", ErrCorrupt, r.ID, r.CreatedAt, err)
	}
	return ListInfo{ID: r.ID, Name: r.Name, CreatedAt: t}, nil
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: a list needs a name", ErrInvalid)
	}
	return name, nil
}

// CreateList stores a new, empty list.
func (s *Store) CreateList(ctx context.Context, name string) (ListInfo, error) {
	name, err := cleanName(name)
	if err != nil {
		return ListInfo{}, err
	}
	created := s.now().UTC()
	var id int64
	err = s.withTx(ctx, func(q *shoppingdb.Queries) error {
		var err error
		id, err = q.InsertList(ctx, shoppingdb.InsertListParams{Name: name, CreatedAt: created.Format(time.RFC3339Nano)})
		if err != nil {
			return fmt.Errorf("create list %q: %w", name, err)
		}
		return nil
	})
	if err != nil {
		return ListInfo{}, err
	}
	return ListInfo{ID: id, Name: name, CreatedAt: created}, nil
}

// Lists returns every list, oldest first.
func (s *Store) Lists(ctx context.Context) ([]ListInfo, error) {
	rows, err := s.q.ListLists(ctx)
	if err != nil {
		return nil, fmt.Errorf("read lists: %w", err)
	}
	out := make([]ListInfo, 0, len(rows))
	for _, r := range rows {
		li, err := listInfoFrom(r)
		if err != nil {
			return nil, err
		}
		out = append(out, li)
	}
	return out, nil
}

// RenameList changes a list's name, or returns ErrNotFound.
func (s *Store) RenameList(ctx context.Context, id int64, name string) error {
	name, err := cleanName(name)
	if err != nil {
		return err
	}
	err = s.withTx(ctx, func(q *shoppingdb.Queries) error {
		n, err := q.RenameList(ctx, shoppingdb.RenameListParams{Name: name, ID: id})
		if err != nil {
			return fmt.Errorf("rename list %d: %w", id, err)
		}
		if n == 0 {
			return fmt.Errorf("%w: list %d", ErrNotFound, id)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.notify(id)
	return nil
}

// DeleteList removes a list with its entries, manual items and check state, or
// returns ErrNotFound.
func (s *Store) DeleteList(ctx context.Context, id int64) error {
	err := s.withTx(ctx, func(q *shoppingdb.Queries) error {
		n, err := q.DeleteList(ctx, id)
		if err != nil {
			return fmt.Errorf("delete list %d: %w", id, err)
		}
		if n == 0 {
			return fmt.Errorf("%w: list %d", ErrNotFound, id)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.notify(id)
	return nil
}
