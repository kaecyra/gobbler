package ingest

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

var (
	// ErrNotConfirmed is returned by Save for a Confirmed that did not come
	// from Confirm.
	ErrNotConfirmed = errors.New("draft was not confirmed")
	// ErrIncompleteDraft is returned by Confirm for a draft that cannot be a
	// recipe yet: no name, no ingredient lines, a line with no ingredient, or
	// a source snapshot that no longer matches what was ingested.
	ErrIncompleteDraft = errors.New("draft is incomplete")
)

// Confirmed is a draft the user has reviewed and approved. It can be made only
// by Confirm, and Saver.Save accepts nothing else, so there is no path that
// stores an unreviewed draft (ADR-00006 obligation 1). Its zero value is not
// confirmed.
type Confirmed struct {
	draft     Draft
	confirmed bool
}

// Confirm is the review step's output: the draft as the user left it, checked
// to be saveable. Call it only when the user has pressed confirm; the review
// screen is the one caller.
func Confirm(d Draft) (Confirmed, error) {
	var problems []error
	if strings.TrimSpace(d.Name.Value) == "" {
		problems = append(problems, errors.New("the recipe has no name"))
	}
	var lines int
	for ci, c := range d.Components {
		for li, l := range c.Lines {
			lines++
			if l.Ingredient == 0 && normalizeName(l.Name) == "" {
				problems = append(problems, fmt.Errorf("component %d line %d (%q) has no ingredient", ci+1, li+1, l.Raw))
			}
		}
	}
	if lines == 0 {
		problems = append(problems, errors.New("the recipe has no ingredient lines"))
	}
	if !d.Source.Kind.Valid() || !d.Source.intact() {
		problems = append(problems, errors.New("the source snapshot does not match the ingested input"))
	}
	if len(problems) > 0 {
		return Confirmed{}, fmt.Errorf("%w: %w", ErrIncompleteDraft, errors.Join(problems...))
	}
	return Confirmed{draft: d, confirmed: true}, nil
}

// RecipeStore is what Save calls on the recipe store; *store.Store satisfies
// it. Snapshots go through AddSnapshot, the only way to write one (ADR-00015).
type RecipeStore interface {
	Create(ctx context.Context, r recipe.Recipe) (int64, error)
	AddSnapshot(ctx context.Context, id int64, kind store.SnapshotKind, content []byte) error
	Delete(ctx context.Context, id int64) error
}

// CatalogWriter is what Save calls on the catalog: the reads of CatalogReader
// plus creating the ingredients and units a draft proposed.
// *catalog.Store satisfies it.
type CatalogWriter interface {
	CatalogReader
	CreateIngredient(ctx context.Context, in catalog.Ingredient) (catalog.Ingredient, error)
	CreateUnit(ctx context.Context, in catalog.Unit) (catalog.Unit, error)
}

// Saver writes confirmed drafts.
type Saver struct {
	recipes RecipeStore
	catalog CatalogWriter
}

// NewSaver returns a Saver over the recipe store and the catalog.
func NewSaver(r RecipeStore, c CatalogWriter) *Saver { return &Saver{recipes: r, catalog: c} }

// Saved reports what a Save wrote.
type Saved struct {
	RecipeID int64
	// NewIngredients and NewUnits were created, unreviewed, for this recipe.
	NewIngredients []catalog.Ingredient
	NewUnits       []catalog.Unit
}

// Save stores a confirmed draft as a recipe with its source snapshot, byte for
// byte as ingested. Ingredients and units the draft proposed that the catalog
// still lacks are created with reviewed false, since only a confirmed save may
// create them (ADR-00008); ones that exist by now are reused.
//
// The recipe and its snapshot are two store calls. If the snapshot cannot be
// written the recipe is deleted again and the error says so, so a recipe never
// remains without its source.
func (s *Saver) Save(ctx context.Context, c Confirmed) (Saved, error) {
	if !c.confirmed {
		return Saved{}, ErrNotConfirmed
	}
	d := c.draft
	var out Saved

	ids := map[string]recipe.IngredientID{}
	units := map[string]quantity.Unit{}
	comps := make([]recipe.Component, len(d.Components))
	for ci, dc := range d.Components {
		comps[ci] = recipe.Component{Name: dc.Name}
		for li, l := range dc.Lines {
			id, err := s.ingredient(ctx, l, ids, &out)
			if err != nil {
				return Saved{}, fmt.Errorf("component %d line %d (%q): %w", ci+1, li+1, l.Raw, err)
			}
			amount := l.Amount
			if amount.Unit, err = s.unit(ctx, amount.Unit, units, &out); err != nil {
				return Saved{}, fmt.Errorf("component %d line %d (%q): %w", ci+1, li+1, l.Raw, err)
			}
			if amount.HasPackage {
				if amount.Package.Unit, err = s.unit(ctx, amount.Package.Unit, units, &out); err != nil {
					return Saved{}, fmt.Errorf("component %d line %d (%q): %w", ci+1, li+1, l.Raw, err)
				}
			}
			rule := quantity.RuleLinear
			if l.ToTaste {
				rule = quantity.RuleToTaste
			}
			comps[ci].Lines = append(comps[ci].Lines, recipe.IngredientLine{
				Amount: amount, Ingredient: id, Qualifiers: l.Qualifiers, Optional: l.Optional, Rule: rule, Raw: l.Raw,
			})
		}
	}
	linkSteps(comps, d.Components)

	id, err := s.recipes.Create(ctx, toRecipe(d, comps))
	if err != nil {
		return Saved{}, fmt.Errorf("save recipe %q: %w", d.Name.Value, err)
	}
	if err := s.recipes.AddSnapshot(ctx, id, d.Source.Kind, d.Source.Content); err != nil {
		if derr := s.recipes.Delete(ctx, id); derr != nil {
			return Saved{}, fmt.Errorf("save %s snapshot of recipe %d: %w (and removing the recipe failed, it has no source: %w)", d.Source.Kind, id, err, derr)
		}
		return Saved{}, fmt.Errorf("save %s snapshot, recipe not saved: %w", d.Source.Kind, err)
	}
	out.RecipeID = id
	return out, nil
}

// ingredient returns the catalog id for a line, creating the proposed
// ingredient when the catalog still lacks it.
func (s *Saver) ingredient(ctx context.Context, l Line, seen map[string]recipe.IngredientID, out *Saved) (recipe.IngredientID, error) {
	if l.Ingredient != 0 {
		return l.Ingredient, nil
	}
	name := normalizeName(l.Name)
	if id, ok := seen[name]; ok {
		return id, nil
	}
	if ing, ok, err := s.catalog.MatchIngredient(ctx, name); err != nil {
		return 0, fmt.Errorf("look up ingredient %q: %w", name, err)
	} else if ok {
		seen[name] = recipe.IngredientID(ing.ID)
		return seen[name], nil
	}
	ing, err := s.catalog.CreateIngredient(ctx, catalog.Ingredient{Name: name, Aisle: proposedAisle, Reviewed: false})
	if err != nil {
		return 0, fmt.Errorf("create proposed ingredient %q: %w", name, err)
	}
	out.NewIngredients = append(out.NewIngredients, ing)
	seen[name] = recipe.IngredientID(ing.ID)
	return seen[name], nil
}

// unit returns the catalog's form of u, creating it unreviewed when absent.
func (s *Saver) unit(ctx context.Context, u quantity.Unit, seen map[string]quantity.Unit, out *Saved) (quantity.Unit, error) {
	if u.IsZero() {
		return u, nil
	}
	key := strings.ToLower(u.Name)
	if cu, ok := seen[key]; ok {
		return cu, nil
	}
	cu, ok, err := s.catalog.MatchUnit(ctx, u.Name)
	if err != nil {
		return u, fmt.Errorf("look up unit %q: %w", u.Name, err)
	}
	if ok {
		seen[key] = cu.Unit
		return cu.Unit, nil
	}
	created, err := s.catalog.CreateUnit(ctx, catalog.Unit{Unit: u, Reviewed: false})
	if err != nil {
		return u, fmt.Errorf("create proposed unit %q: %w", u.Name, err)
	}
	out.NewUnits = append(out.NewUnits, created)
	seen[key] = created.Unit
	return created.Unit, nil
}

// linkSteps adds the steps of the draft to comps with ids unique across the
// recipe: s1, s2, ... in reading order.
func linkSteps(comps []recipe.Component, from []Component) {
	n := 0
	for ci, dc := range from {
		for _, st := range dc.Steps {
			n++
			comps[ci].Steps = append(comps[ci].Steps, recipe.Step{ID: recipe.StepID(fmt.Sprintf("s%d", n)), Text: st.Text, Duration: st.Duration})
		}
	}
}

func toRecipe(d Draft, comps []recipe.Component) recipe.Recipe {
	r := recipe.Recipe{
		Name:        strings.TrimSpace(d.Name.Value),
		Description: d.Description.Value,
		SourceURL:   d.Source.URL,
		Author:      d.Author.Value,
		Yield:       d.Yield.Value,
		Servings:    d.Servings.Value,
		Times:       recipe.Times{Prep: d.Prep.Value, Cook: d.Cook.Value, Total: d.Total.Value},
		Components:  comps,
	}
	if d.Calories.Value > 0 {
		n := d.Calories.Value
		r.CaloriesPerServing = &n
	}
	return r
}
