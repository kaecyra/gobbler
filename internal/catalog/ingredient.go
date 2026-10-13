package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/kaecyra/gobbler/internal/catalog/catalogdb"
)

// Ingredient is a canonical ingredient (ADR-00008). Name is the bare name
// without qualifiers and Aliases hold plurals and synonyms; both are stored
// trimmed and lower-case. DensityGPerML is grams per millilitre and nil when
// unknown, never guessed (ADR-00005). USDAID is the FoodData Central id.
// Reviewed is false for an ingredient proposed by ingestion and not yet
// checked on the catalog page.
//
// Central is true for an entry of the central layer, which households read but
// do not edit (ADR-00018). A household sees its own aliases and aisle for a
// central entry in Aliases and Aisle. Suggested is true for an overlay entry
// its household flagged for promotion.
type Ingredient struct {
	ID            int64
	Name          string
	Aisle         string
	Aliases       []string
	DensityGPerML *float64
	USDAID        *int64
	Reviewed      bool
	Central       bool
	Suggested     bool
}

func (i *Ingredient) prepare() error {
	i.Name = normalize(i.Name)
	if err := validateName(i.Name); err != nil {
		return err
	}
	i.Aisle = normalize(i.Aisle)
	if i.Aisle == "" {
		return fmt.Errorf("%w: ingredient %q has no aisle", ErrInvalid, i.Name)
	}
	if d := i.DensityGPerML; d != nil && (math.IsNaN(*d) || math.IsInf(*d, 0) || *d <= 0) {
		return fmt.Errorf("%w: ingredient %q density %v must be a positive number", ErrInvalid, i.Name, *d)
	}
	if i.USDAID != nil && *i.USDAID <= 0 {
		return fmt.Errorf("%w: ingredient %q USDA id %d must be positive", ErrInvalid, i.Name, *i.USDAID)
	}
	i.Aliases = normalizeAliases(i.Name, i.Aliases)
	return nil
}

func ingredientFrom(r catalogdb.Ingredient) Ingredient {
	i := Ingredient{
		ID: r.ID, Name: r.Name, Aisle: r.Aisle, Reviewed: r.Reviewed != 0,
		Central: !r.HouseholdID.Valid, Suggested: r.SuggestedAt.Valid,
	}
	if r.DensityGPerMl.Valid {
		i.DensityGPerML = &r.DensityGPerMl.Float64
	}
	if r.UsdaFdcID.Valid {
		i.USDAID = &r.UsdaFdcID.Int64
	}
	return i
}

// CreateIngredient stores a new ingredient and returns it with its id. In a
// household's scope it lands in that household's overlay; with no household
// on the context it is a central entry. Set Reviewed to false for an
// ingredient proposed by ingestion. It returns ErrConflict if the name or an
// alias already belongs to another ingredient the caller can see.
func (s *Store) CreateIngredient(ctx context.Context, in Ingredient) (Ingredient, error) {
	if err := in.prepare(); err != nil {
		return Ingredient{}, err
	}
	scope := scopeOf(ctx)
	err := s.withTx(ctx, func(q *catalogdb.Queries, _ *sql.Tx) error {
		return createIngredient(ctx, q, scope, &in)
	})
	if err != nil {
		return Ingredient{}, err
	}
	return in, nil
}

// createIngredient writes the prepared in on q and sets its ID and Central.
func createIngredient(ctx context.Context, q *catalogdb.Queries, scope int64, in *Ingredient) (err error) {
	if err := checkFreeIngredient(ctx, q, scope, 0, in.Name, in.Aliases); err != nil {
		return err
	}
	in.ID, err = q.CreateIngredient(ctx, catalogdb.CreateIngredientParams{
		HouseholdID: owner(scope), Name: in.Name, Aisle: in.Aisle, DensityGPerMl: nullFloat(in.DensityGPerML),
		UsdaFdcID: nullInt(in.USDAID), Reviewed: boolInt(in.Reviewed),
	})
	if err != nil {
		return wrapWrite("create ingredient "+in.Name, err)
	}
	if err := replaceIngredientAliases(ctx, q, owner(scope), in.ID, in.Aliases); err != nil {
		return err
	}
	in.Central = scope == 0
	return nil
}

// ResolveInTx returns the ingredient the household on the context knows by
// name, its own overlay first and the central layer second, or creates it in
// that household's overlay as unreviewed with the given aisle when neither
// matches. It runs on tx, the caller's open write transaction, so the entry it
// may create commits or rolls back with the caller's other writes. tx must come
// from db.WithTx over the same database.
func (s *Store) ResolveInTx(ctx context.Context, tx *sql.Tx, name, aisle string) (Ingredient, error) {
	q := s.q.WithTx(tx)
	scope := scopeOf(ctx)
	if in, ok, err := matchIngredient(ctx, q, scope, name); err != nil || ok {
		return in, err
	}
	in := Ingredient{Name: name, Aisle: aisle, Reviewed: false}
	if err := in.prepare(); err != nil {
		return Ingredient{}, err
	}
	if err := createIngredient(ctx, q, scope, &in); err != nil {
		return Ingredient{}, err
	}
	return in, nil
}

// UpdateIngredient replaces every field of the ingredient with in.ID,
// including its aliases. It returns ErrNotFound if the id does not exist in
// the caller's scope. A household may edit its overlay entries freely; for a
// central entry it may change only its own aliases and aisle, and any other
// change is refused with ErrCentral. With no household on the context the
// central entry is edited as a whole.
func (s *Store) UpdateIngredient(ctx context.Context, in Ingredient) error {
	if err := in.prepare(); err != nil {
		return err
	}
	scope := scopeOf(ctx)
	return s.withTx(ctx, func(q *catalogdb.Queries, _ *sql.Tx) error {
		cur, err := q.GetIngredient(ctx, catalogdb.GetIngredientParams{ID: in.ID, Scope: readScope(scope)})
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: ingredient %d", ErrNotFound, in.ID)
		}
		if err != nil {
			return fmt.Errorf("read ingredient %d: %w", in.ID, err)
		}
		if cur.HouseholdID.Valid || scope == 0 {
			return updateWhole(ctx, q, cur, in, scope)
		}
		return updateOverride(ctx, q, cur, in, scope)
	})
}

// updateWhole writes every field of an entry the caller owns: an overlay entry
// of its household, or a central entry with no household in scope.
func updateWhole(ctx context.Context, q *catalogdb.Queries, cur catalogdb.Ingredient, in Ingredient, scope int64) error {
	if err := checkFreeIngredient(ctx, q, scope, in.ID, in.Name, in.Aliases); err != nil {
		return err
	}
	n, err := q.UpdateIngredient(ctx, catalogdb.UpdateIngredientParams{
		Name: in.Name, Aisle: in.Aisle, DensityGPerMl: nullFloat(in.DensityGPerML),
		UsdaFdcID: nullInt(in.USDAID), Reviewed: boolInt(in.Reviewed), ID: in.ID, HouseholdID: cur.HouseholdID,
	})
	if err != nil {
		return wrapWrite(fmt.Sprintf("update ingredient %d", in.ID), err)
	}
	if err := requireAffected(n, "ingredient", in.ID); err != nil {
		return err
	}
	return replaceIngredientAliases(ctx, q, cur.HouseholdID, in.ID, in.Aliases)
}

// updateOverride applies a household's edit of a central entry: its own
// aliases and its aisle. Anything else that differs from the central entry is
// refused rather than dropped, so a form never appears to save what it did not.
func updateOverride(ctx context.Context, q *catalogdb.Queries, cur catalogdb.Ingredient, in Ingredient, scope int64) error {
	if in.Name != cur.Name || !sameFloat(in.DensityGPerML, cur.DensityGPerMl) || !sameInt(in.USDAID, cur.UsdaFdcID) {
		return errCentral(fmt.Sprintf("only the aliases and aisle of %q can be changed", cur.Name))
	}
	central, err := q.ListIngredientAliases(ctx, catalogdb.ListIngredientAliasesParams{IngredientID: in.ID})
	if err != nil {
		return fmt.Errorf("read central aliases of ingredient %d: %w", in.ID, err)
	}
	isCentral := map[string]bool{}
	for _, a := range central {
		isCentral[a] = true
	}
	var own []string
	for _, a := range in.Aliases {
		if !isCentral[a] {
			own = append(own, a)
		}
	}
	if err := checkFreeIngredient(ctx, q, scope, in.ID, in.Name, own); err != nil {
		return err
	}
	if err := replaceIngredientAliases(ctx, q, owner(scope), in.ID, own); err != nil {
		return err
	}
	if in.Aisle == cur.Aisle {
		err = q.DeleteAisleOverride(ctx, catalogdb.DeleteAisleOverrideParams{HouseholdID: scope, IngredientID: in.ID})
	} else {
		err = q.SetAisleOverride(ctx, catalogdb.SetAisleOverrideParams{HouseholdID: scope, IngredientID: in.ID, Aisle: in.Aisle})
	}
	if err != nil {
		return fmt.Errorf("set aisle of ingredient %d: %w", in.ID, err)
	}
	return nil
}

func sameFloat(p *float64, n sql.NullFloat64) bool {
	if p == nil || !n.Valid {
		return p == nil && !n.Valid
	}
	return *p == n.Float64
}

func sameInt(p *int64, n sql.NullInt64) bool {
	if p == nil || !n.Valid {
		return p == nil && !n.Valid
	}
	return *p == n.Int64
}

// GetIngredient returns the ingredient with the given id, or ErrNotFound for
// an id that does not exist or belongs to another household's overlay.
func (s *Store) GetIngredient(ctx context.Context, id int64) (Ingredient, error) {
	return getIngredient(ctx, s.q, scopeOf(ctx), id)
}

func getIngredient(ctx context.Context, q *catalogdb.Queries, scope, id int64) (Ingredient, error) {
	row, err := q.GetIngredient(ctx, catalogdb.GetIngredientParams{ID: id, Scope: readScope(scope)})
	if errors.Is(err, sql.ErrNoRows) {
		return Ingredient{}, fmt.Errorf("%w: ingredient %d", ErrNotFound, id)
	}
	if err != nil {
		return Ingredient{}, fmt.Errorf("read ingredient %d: %w", id, err)
	}
	i := ingredientFrom(row)
	if i.Aliases, err = q.ListIngredientAliases(ctx, catalogdb.ListIngredientAliasesParams{IngredientID: id, Scope: readScope(scope)}); err != nil {
		return Ingredient{}, fmt.Errorf("read ingredient aliases: %w", err)
	}
	if i.Central && scope != 0 {
		aisle, err := q.GetAisleOverride(ctx, catalogdb.GetAisleOverrideParams{HouseholdID: scope, IngredientID: id})
		switch {
		case err == nil:
			i.Aisle = aisle
		case !errors.Is(err, sql.ErrNoRows):
			return Ingredient{}, fmt.Errorf("read aisle of ingredient %d: %w", id, err)
		}
	}
	return i, nil
}

// ListIngredients returns every ingredient the caller can see, central and
// overlay, ordered by name.
func (s *Store) ListIngredients(ctx context.Context) ([]Ingredient, error) {
	scope := scopeOf(ctx)
	rows, err := s.q.ListIngredients(ctx, readScope(scope))
	return s.withIngredientAliases(ctx, scope, rows, err)
}

// ListUnreviewedIngredients returns the household's own ingredients awaiting
// review, ordered by name, for the catalog cleanup page. Central entries are
// the operator's, so they are not listed (with no household, only they are).
func (s *Store) ListUnreviewedIngredients(ctx context.Context) ([]Ingredient, error) {
	scope := scopeOf(ctx)
	rows, err := s.q.ListUnreviewedIngredients(ctx, owner(scope))
	return s.withIngredientAliases(ctx, scope, rows, err)
}

// withIngredientAliases converts the rows of a list query, which failed with
// err if it is set, and attaches every alias and the household's aisles in
// two further queries.
func (s *Store) withIngredientAliases(ctx context.Context, scope int64, rows []catalogdb.Ingredient, err error) ([]Ingredient, error) {
	if err != nil {
		return nil, fmt.Errorf("list ingredients: %w", err)
	}
	aliasRows, err := s.q.ListAllIngredientAliases(ctx, readScope(scope))
	if err != nil {
		return nil, fmt.Errorf("read all ingredient aliases: %w", err)
	}
	aliases := map[int64][]string{}
	for _, r := range aliasRows {
		aliases[r.IngredientID] = append(aliases[r.IngredientID], r.Alias)
	}
	overrides := map[int64]string{}
	if scope != 0 {
		rows, err := s.q.ListAisleOverrides(ctx, scope)
		if err != nil {
			return nil, fmt.Errorf("read ingredient aisles: %w", err)
		}
		for _, r := range rows {
			overrides[r.IngredientID] = r.Aisle
		}
	}
	var out []Ingredient
	for _, r := range rows {
		i := ingredientFrom(r)
		i.Aliases = aliases[i.ID]
		if a, ok := overrides[i.ID]; ok && i.Central {
			i.Aisle = a
		}
		out = append(out, i)
	}
	return out, nil
}

// MatchIngredient finds the ingredient whose name or alias equals term,
// ignoring case and tolerating plural and singular forms. The household's own
// entries and aliases are consulted before the central layer (ADR-00018). It
// returns (Ingredient{}, false, nil) when nothing matches; that is an answer,
// not an error, and nothing is created. This is the method internal/parse
// consumes through an interface it declares itself.
func (s *Store) MatchIngredient(ctx context.Context, term string) (Ingredient, bool, error) {
	return matchIngredient(ctx, s.q, scopeOf(ctx), term)
}

// matchIngredient is MatchIngredient on any queries handle.
func matchIngredient(ctx context.Context, q *catalogdb.Queries, scope int64, term string) (Ingredient, bool, error) {
	for _, cand := range inflections(normalize(term)) {
		id, ok, err := findIngredient(ctx, q, scope, cand)
		if err != nil {
			return Ingredient{}, false, err
		}
		if !ok {
			continue
		}
		i, err := getIngredient(ctx, q, scope, id)
		if err != nil {
			return Ingredient{}, false, err
		}
		return i, true, nil
	}
	return Ingredient{}, false, nil
}

// findIngredient returns the id of the entry whose name or alias is exactly
// term (already normalized): the household's own first, then the central
// layer's.
func findIngredient(ctx context.Context, q *catalogdb.Queries, scope int64, term string) (int64, bool, error) {
	if scope != 0 {
		id, err := q.LookupOverlayIngredientID(ctx, catalogdb.LookupOverlayIngredientIDParams{Owner: readScope(scope), Term: term})
		if err == nil {
			return id, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, false, fmt.Errorf("look up ingredient %q: %w", term, err)
		}
	}
	id, err := q.LookupCentralIngredientID(ctx, term)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("look up ingredient %q: %w", term, err)
	}
	return id, true, nil
}

// checkFreeIngredient returns ErrConflict if name or any alias is already used
// by an entry other than selfID (0 for a new entry) that the scope can see.
// Another household's overlay is not visible, so it never conflicts.
func checkFreeIngredient(ctx context.Context, q *catalogdb.Queries, scope, selfID int64, name string, aliases []string) error {
	for _, term := range append([]string{name}, aliases...) {
		id, ok, err := findIngredient(ctx, q, scope, term)
		if err != nil {
			return err
		}
		if ok && id != selfID {
			return fmt.Errorf("%w: ingredient %q is already used by ingredient %d", ErrConflict, term, id)
		}
	}
	return nil
}

// replaceIngredientAliases sets the aliases of one layer of id: the central
// ones when layer is NULL, otherwise that household's.
func replaceIngredientAliases(ctx context.Context, q *catalogdb.Queries, layer sql.NullInt64, id int64, aliases []string) error {
	if err := checkAliasScope(ctx, q, layer, id); err != nil {
		return err
	}
	if err := q.DeleteIngredientAliases(ctx, catalogdb.DeleteIngredientAliasesParams{IngredientID: id, HouseholdID: layer}); err != nil {
		return fmt.Errorf("clear ingredient aliases: %w", err)
	}
	for _, a := range aliases {
		if err := addIngredientAlias(ctx, q, layer, a, id); err != nil {
			return err
		}
	}
	return nil
}

// addIngredientAlias adds one alias to a layer of id, after checking the scope.
func addIngredientAlias(ctx context.Context, q *catalogdb.Queries, layer sql.NullInt64, alias string, id int64) error {
	if err := checkAliasScope(ctx, q, layer, id); err != nil {
		return err
	}
	if err := q.AddIngredientAlias(ctx, catalogdb.AddIngredientAliasParams{HouseholdID: layer, Alias: alias, IngredientID: id}); err != nil {
		return wrapWrite(fmt.Sprintf("add ingredient alias %q", alias), err)
	}
	return nil
}

// checkAliasScope enforces that an alias stays in its scope (ADR-00018): a
// central alias belongs to a central entry, and a household's alias to a central
// entry or one of that household's own, never another's. The store is the only
// writer of aliases, so the rule lives here and not in the schema.
func checkAliasScope(ctx context.Context, q *catalogdb.Queries, layer sql.NullInt64, id int64) error {
	entry, err := q.GetIngredientOwner(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: ingredient %d", ErrNotFound, id)
	}
	if err != nil {
		return fmt.Errorf("read owner of ingredient %d: %w", id, err)
	}
	if entry.Valid && (!layer.Valid || layer.Int64 != entry.Int64) {
		return fmt.Errorf("%w: an alias must stay in the scope of ingredient %d", ErrInvalid, id)
	}
	return nil
}

// SuggestIngredient flags an overlay entry of the caller's household for the
// operator to consider promoting to the central layer (ADR-00018). It returns
// ErrNotFound for a central entry, another household's entry or no household.
func (s *Store) SuggestIngredient(ctx context.Context, id int64) error {
	return s.setSuggested(ctx, id, sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true})
}

// UnsuggestIngredient withdraws the flag SuggestIngredient set.
func (s *Store) UnsuggestIngredient(ctx context.Context, id int64) error {
	return s.setSuggested(ctx, id, sql.NullString{})
}

func (s *Store) setSuggested(ctx context.Context, id int64, at sql.NullString) error {
	scope := scopeOf(ctx)
	return s.withTx(ctx, func(q *catalogdb.Queries, _ *sql.Tx) error {
		var n int64
		var err error
		if at.Valid {
			n, err = q.SuggestIngredient(ctx, catalogdb.SuggestIngredientParams{SuggestedAt: at, ID: id, Owner: readScope(scope)})
		} else {
			n, err = q.UnsuggestIngredient(ctx, catalogdb.UnsuggestIngredientParams{ID: id, Owner: readScope(scope)})
		}
		if err != nil {
			return fmt.Errorf("flag ingredient %d for central: %w", id, err)
		}
		return requireAffected(n, "ingredient", id)
	})
}
