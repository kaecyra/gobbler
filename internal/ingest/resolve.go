package ingest

import (
	"context"
	"strings"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/parse"
	"github.com/kaecyra/gobbler/internal/quantity"
)

// CatalogReader is what the pipeline reads from the catalog (ADR-00008).
// *catalog.Store satisfies it.
type CatalogReader interface {
	MatchIngredient(ctx context.Context, term string) (catalog.Ingredient, bool, error)
	MatchUnit(ctx context.Context, term string) (catalog.Unit, bool, error)
}

// resolver answers catalog lookups for one ingest. The line parser is pure and
// its matcher interface has no context or error, so the resolver answers from
// a cache and remembers the first catalog failure; the caller checks err after
// parsing, because a failed lookup is a broken pipeline, not a hard line.
type resolver struct {
	ctx         context.Context
	cat         CatalogReader
	ingredients map[string]ingredientHit
	units       map[string]unitHit
	err         error
}

type ingredientHit struct {
	match parse.IngredientMatch
	ok    bool
}

type unitHit struct {
	unit quantity.Unit
	ok   bool
}

func newResolver(ctx context.Context, cat CatalogReader) *resolver {
	return &resolver{ctx: ctx, cat: cat, ingredients: map[string]ingredientHit{}, units: map[string]unitHit{}}
}

// MatchIngredient implements parse.IngredientMatcher.
func (r *resolver) MatchIngredient(name string) (parse.IngredientMatch, bool) {
	if r.err != nil {
		return parse.IngredientMatch{}, false
	}
	if h, ok := r.ingredients[name]; ok {
		return h.match, h.ok
	}
	ing, ok, err := r.cat.MatchIngredient(r.ctx, name)
	if err != nil {
		r.err = err
		return parse.IngredientMatch{}, false
	}
	h := ingredientHit{ok: ok}
	if ok {
		h.match = parse.IngredientMatch{ID: ing.ID, Name: ing.Name}
	}
	r.ingredients[name] = h
	return h.match, h.ok
}

// unit is the parser's unit source (MatchUnit calls it for every unit spelling
// in a line). It returns the catalog's unit for u's name or alias, so a draft
// only ever carries catalog units, answering from a per-ingest cache. found is
// false for a spelling the catalog does not have, or after a catalog failure
// (r.err, which the caller checks); u is then returned unchanged. The zero
// unit is always found.
func (r *resolver) unit(u quantity.Unit) (quantity.Unit, bool) {
	if u.IsZero() || r.err != nil {
		return u, r.err == nil
	}
	key := strings.ToLower(u.Name)
	if h, ok := r.units[key]; ok {
		return pick(h, u)
	}
	cu, ok, err := r.cat.MatchUnit(r.ctx, u.Name)
	if err != nil {
		r.err = err
		return u, false
	}
	r.units[key] = unitHit{unit: cu.Unit, ok: ok}
	return pick(r.units[key], u)
}

func pick(h unitHit, orig quantity.Unit) (quantity.Unit, bool) {
	if h.ok {
		return h.unit, true
	}
	return orig, false
}
