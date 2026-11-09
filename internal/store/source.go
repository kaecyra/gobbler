package store

import (
	"context"
	"strings"

	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// SourceAddress is the address two sources are compared by: rawURL without
// its fragment, everything from the first '#' (ADR-00048). Nothing else is
// normalised: the scheme, host, path and query are compared as written, as
// the pipeline stores them.
func SourceAddress(rawURL string) string {
	if i := strings.IndexByte(rawURL, '#'); i >= 0 {
		return rawURL[:i]
	}
	return rawURL
}

// SourcedRecipe is a recipe named by the address it was imported from.
type SourcedRecipe struct {
	ID   int64
	Name string
}

// RecipesFromSource lists the household's recipes whose source address is
// rawURL's, fragments ignored (SourceAddress), in id order. An address that
// is empty without its fragment matches nothing.
func (s *Store) RecipesFromSource(ctx context.Context, rawURL string) ([]SourcedRecipe, error) {
	hid, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	addr := SourceAddress(rawURL)
	if addr == "" {
		return nil, nil
	}
	var out []SourcedRecipe
	err = s.read(ctx, func(q *storedb.Queries) error {
		rows, err := q.ListSourcedRecipes(ctx, hid)
		if err != nil {
			return readErr("recipes from source", err)
		}
		for _, r := range rows {
			if SourceAddress(r.SourceUrl) == addr {
				out = append(out, SourcedRecipe{ID: r.ID, Name: r.Name})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
