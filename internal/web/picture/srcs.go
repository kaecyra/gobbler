package picture

import "context"

// Srcs gives the library and the recipe page the address of a recipe's
// picture. Those feature packages may not import this one (ADR-00012), so each
// declares the one-method interface it needs and the router hands them this.
type Srcs struct {
	Pictures interface {
		Key(ctx context.Context, recipeID int64) (string, bool, error)
	}
}

// Src is the address of recipe id's picture (see Src), empty when the recipe
// has none. A recipe that is not the household's is store.ErrNotFound, as from
// the service.
func (s Srcs) Src(ctx context.Context, id int64) (string, error) {
	key, ok, err := s.Pictures.Key(ctx, id)
	if err != nil || !ok {
		return "", err
	}
	return Src(id, key), nil
}
