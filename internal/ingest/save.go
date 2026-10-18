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
	"github.com/kaecyra/gobbler/internal/video"
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

// UnknownUnitError is returned (wrapped) by Save for a draft line whose unit
// the catalog does not have. Drafting only emits catalog units, so this means
// the catalog changed after the draft was made; the draft is still good and
// the caller can show the problem instead of failing.
type UnknownUnitError struct{ Name string }

func (e *UnknownUnitError) Error() string {
	return fmt.Sprintf("unit %q is not in the catalog", e.Name)
}

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
	if err := checkStepGraph(d.Components); err != nil {
		problems = append(problems, err)
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
// it. The recipe and its snapshot are written in one transaction, so neither
// can exist without the other.
type RecipeStore interface {
	// HasThumbnail and SaveThumbnail keep the preview image of the recipe's
	// video (ADR-00021 decision 3).
	video.ThumbnailStore
	CreateWithSnapshots(ctx context.Context, r recipe.Recipe, snaps ...store.SnapshotInput) (int64, error)
}

// CatalogWriter is what Save calls on the catalog: the reads of CatalogReader
// plus creating the ingredients and equipment a draft proposed.
// *catalog.Store satisfies it.
type CatalogWriter interface {
	CatalogReader
	CreateIngredient(ctx context.Context, in catalog.Ingredient) (catalog.Ingredient, error)
	CreateEquipment(ctx context.Context, in catalog.Equipment) (catalog.Equipment, error)
}

// Saver writes confirmed drafts.
type Saver struct {
	recipes RecipeStore
	catalog CatalogWriter
	// thumbs downloads a video's thumbnail; nil means YouTube's image host.
	thumbs video.ThumbnailSource
}

// WithThumbnails returns s downloading video thumbnails from src instead of
// YouTube's image host. It exists so a test can stand in for the network.
func (s *Saver) WithThumbnails(src video.ThumbnailSource) *Saver {
	c := *s
	c.thumbs = src
	return &c
}

// NewSaver returns a Saver over the recipe store and the catalog.
func NewSaver(r RecipeStore, c CatalogWriter) *Saver { return &Saver{recipes: r, catalog: c} }

// Saved reports what a Save wrote.
type Saved struct {
	RecipeID int64
	// NewIngredients were created, unreviewed, for this recipe.
	NewIngredients []catalog.Ingredient
	// NewEquipment were created, unreviewed, for this recipe.
	NewEquipment []catalog.Equipment
}

// Save stores a confirmed draft as a recipe with its source snapshot, byte for
// byte as ingested, and the parser version that produced the draft.
// Ingredients the draft proposed that the catalog still lacks are created with
// reviewed false, since only a confirmed save may create them (ADR-00008); ones
// that exist by now are reused. Proposed equipment is treated the same way.
// Units are never created here: the catalog owns them, so a unit it does not
// know is an error.
//
// The recipe and its snapshot are one store call and one transaction. If the
// snapshot cannot be written nothing is, so a recipe never exists without its
// source. The catalog entries created above are separate and stay.
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
			if amount.Unit, err = s.unit(ctx, amount.Unit, units); err != nil {
				return Saved{}, fmt.Errorf("component %d line %d (%q): %w", ci+1, li+1, l.Raw, err)
			}
			if amount.HasPackage {
				if amount.Package.Unit, err = s.unit(ctx, amount.Package.Unit, units); err != nil {
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
	links, err := s.equipment(ctx, d.Equipment, &out)
	if err != nil {
		return Saved{}, err
	}

	id, err := s.recipes.CreateWithSnapshots(ctx, toRecipe(d, comps, links),
		store.SnapshotInput{Kind: d.Source.Kind, Content: d.Source.Content, ParserVersion: d.ParserVersion})
	if err != nil {
		return Saved{}, fmt.Errorf("save recipe %q with its %s snapshot, recipe not stored: %w", d.Name.Value, d.Source.Kind, err)
	}
	out.RecipeID = id
	video.KeepThumbnail(ctx, s.recipes, s.thumbs, id, d.VideoID)
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

// equipment returns the recipe's equipment links, once per catalog entry,
// creating the proposed equipment the catalog still lacks (unreviewed, never
// sized: only the operator sets that, ADR-00019).
func (s *Saver) equipment(ctx context.Context, items []Equipment, out *Saved) ([]recipe.EquipmentLink, error) {
	var links []recipe.EquipmentLink
	have := map[recipe.EquipmentID]bool{}
	byName := map[string]recipe.EquipmentID{}
	for _, it := range items {
		id := it.Equipment
		if id == 0 {
			name := normalizeName(it.Name)
			if name == "" {
				continue
			}
			var err error
			if id, err = s.equipmentID(ctx, name, byName, out); err != nil {
				return nil, err
			}
		}
		if have[id] {
			continue
		}
		have[id] = true
		links = append(links, recipe.EquipmentLink{Equipment: id})
	}
	return links, nil
}

// equipmentID returns the catalog id for a proposed name, creating it when
// the catalog still lacks it.
func (s *Saver) equipmentID(ctx context.Context, name string, seen map[string]recipe.EquipmentID, out *Saved) (recipe.EquipmentID, error) {
	if id, ok := seen[name]; ok {
		return id, nil
	}
	if eq, ok, err := s.catalog.MatchEquipment(ctx, name); err != nil {
		return 0, fmt.Errorf("look up equipment %q: %w", name, err)
	} else if ok {
		seen[name] = recipe.EquipmentID(eq.ID)
		return seen[name], nil
	}
	eq, err := s.catalog.CreateEquipment(ctx, catalog.Equipment{Name: name, Reviewed: false})
	if err != nil {
		return 0, fmt.Errorf("create proposed equipment %q: %w", name, err)
	}
	out.NewEquipment = append(out.NewEquipment, eq)
	seen[name] = recipe.EquipmentID(eq.ID)
	return seen[name], nil
}

// unit returns the catalog's form of u. A draft only carries units the catalog
// resolved while drafting, so one it no longer knows means the contract broke
// and the save fails rather than inventing a unit.
func (s *Saver) unit(ctx context.Context, u quantity.Unit, seen map[string]quantity.Unit) (quantity.Unit, error) {
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
	return u, &UnknownUnitError{Name: u.Name}
}

// linkSteps adds the steps of the draft to comps with ids unique across the
// recipe: s1, s2, ... in reading order. A draft step's DependsOn numbers are
// those ids' numbers, so they map straight onto step ids.
func linkSteps(comps []recipe.Component, from []Component) {
	n := 0
	for ci, dc := range from {
		for _, st := range dc.Steps {
			n++
			step := recipe.Step{ID: stepID(n), Text: st.Text, Duration: st.Duration, Passive: st.Passive, Kind: st.Kind, Title: st.Title}
			for _, dep := range st.DependsOn {
				step.DependsOn = append(step.DependsOn, stepID(dep))
			}
			comps[ci].Steps = append(comps[ci].Steps, step)
		}
	}
}

func stepID(n int) recipe.StepID { return recipe.StepID(fmt.Sprintf("s%d", n)) }

func toRecipe(d Draft, comps []recipe.Component, links []recipe.EquipmentLink) recipe.Recipe {
	r := recipe.Recipe{
		Name:        strings.TrimSpace(d.Name.Value),
		Description: d.Description.Value,
		SourceURL:   d.Source.URL,
		Author:      d.Author.Value,
		Yield:       d.Yield.Value,
		Servings:    d.Servings.Value,
		Times:       recipe.Times{Prep: d.Prep.Value, Cook: d.Cook.Value, Total: d.Total.Value},
		Components:  comps,
		Equipment:   links,
		VideoID:     d.VideoID,
		MealType:    d.MealType,
		Cuisine:     d.Cuisine,
		SpiceLevel:  d.SpiceLevel,
	}
	if d.Calories.Value > 0 {
		n := d.Calories.Value
		r.CaloriesPerServing = &n
	}
	return r
}
