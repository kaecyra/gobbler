package shopping

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	domain "github.com/kaecyra/gobbler/internal/shopping"
	"github.com/kaecyra/gobbler/internal/store"
)

// The types below are aliases of the domain types the callers of the service
// work with, so a value the service returns is the domain's own value and the
// adapters never import the domain package (see the package doc).

// Change says that something about a list changed (shopping.Change).
type Change = domain.Change

// EntryView is a recipe on a list, as loaded (shopping.EntryView).
type EntryView = domain.EntryView

// Item is one merged item of a generated list (shopping.Item).
type Item = domain.Item

// ItemKey identifies an item for SetChecked (shopping.ItemKey).
type ItemKey = domain.ItemKey

// ListInfo is a list's header (shopping.ListInfo).
type ListInfo = domain.ListInfo

// ManualItem is a hand-written item to add (shopping.ManualItem).
type ManualItem = domain.ManualItem

// ManualView is a manual item as loaded (shopping.ManualView).
type ManualView = domain.ManualView

// View is a loaded list (shopping.View).
type View = domain.View

// ViewAisle is one aisle of a View (shopping.ViewAisle).
type ViewAisle = domain.ViewAisle

// ViewItem is one item of a ViewAisle (shopping.ViewItem).
type ViewItem = domain.ViewItem

// Store is the shopping store the service is built over (shopping.Store). The
// router and the MCP endpoint hold one so that neither names the domain
// package; every operation on it goes through a Service.
type Store = domain.Store

// RecipeSource is what the shopping store reads from the recipe store.
type RecipeSource = domain.RecipeSource

// CatalogSource is what the shopping store reads from the catalog.
type CatalogSource = domain.CatalogSource

// Ingredient is a catalog ingredient (catalog.Ingredient).
type Ingredient = catalog.Ingredient

// Unit is a catalog unit (catalog.Unit).
type Unit = catalog.Unit

// RecipeBrief is a recipe as listed for choosing one (store.Summary).
type RecipeBrief = store.Summary

// The errors a service method returns, for callers to branch on with errors.Is.
var (
	ErrNotFound          = domain.ErrNotFound
	ErrInvalid           = domain.ErrInvalid
	ErrDuplicate         = domain.ErrDuplicate
	ErrUnknownReference  = domain.ErrUnknownReference
	ErrInvalidMultiplier = domain.ErrInvalidMultiplier
	ErrInvalidManualItem = domain.ErrInvalidManualItem

	// ErrNotMember is returned for a Caller that is not a verified member of a
	// household.
	ErrNotMember = errors.New("not a member of a household")
)

// NewStore returns the shopping store over d, which must already be migrated.
// The router builds it here so that it, too, names no domain package.
func NewStore(d *sql.DB, rec RecipeSource, cat CatalogSource) *Store {
	return domain.New(d, rec, cat)
}

// Caller is who an operation is for: a verified person and the household they
// are a member of (ADR-00018). Adapters build it only from an identity they
// have verified; the service does not verify one.
type Caller struct {
	// Person is the verified email of the person.
	Person string
	// Household is the household the person is a member of.
	Household household.Household
}

// Recipes is what the service reads from the recipe store. *store.Store
// satisfies it.
type Recipes interface {
	Search(ctx context.Context, q store.SearchQuery) ([]store.Summary, error)
	Info(ctx context.Context, id int64) (store.Info, error)
}

// Catalog is what the service reads from the catalog store. *catalog.Store
// satisfies it.
type Catalog interface {
	ListIngredients(ctx context.Context) ([]catalog.Ingredient, error)
	ListUnits(ctx context.Context) ([]catalog.Unit, error)
	GetIngredient(ctx context.Context, id int64) (catalog.Ingredient, error)
}

// Deps is what the service is built from.
type Deps struct {
	Lists   *Store
	Recipes Recipes
	Catalog Catalog
}

// Service is the shopping operations. It is safe for concurrent use.
type Service struct {
	lists   *Store
	recipes Recipes
	catalog Catalog
}

// New returns a Service. All three collaborators are required.
func New(d Deps) *Service {
	return &Service{lists: d.Lists, recipes: d.Recipes, catalog: d.Catalog}
}

// enter checks the Caller and returns a context that runs in the Caller's
// household. It is the first line of every method.
func enter(ctx context.Context, c Caller) (context.Context, error) {
	if c.Person == "" || c.Household.ID == 0 {
		return nil, ErrNotMember
	}
	return household.WithContext(ctx, c.Household), nil
}

// OnChange registers a handler for list changes (shopping.Store.OnChange).
// Call it at start-up, before the service is shared between goroutines. It
// carries no Caller: a handler learns only the list id and reloads through a
// method that does.
func (s *Service) OnChange(h func(Change)) { s.lists.OnChange(h) }

// Lists returns the household's lists, oldest first.
func (s *Service) Lists(ctx context.Context, c Caller) ([]ListInfo, error) {
	ctx, err := enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.lists.Lists(ctx)
}

// CreateList makes an empty list.
func (s *Service) CreateList(ctx context.Context, c Caller, name string) (ListInfo, error) {
	ctx, err := enter(ctx, c)
	if err != nil {
		return ListInfo{}, err
	}
	return s.lists.CreateList(ctx, name)
}

// GetList returns one list's header, or ErrNotFound.
func (s *Service) GetList(ctx context.Context, c Caller, id int64) (ListInfo, error) {
	ctx, err := enter(ctx, c)
	if err != nil {
		return ListInfo{}, err
	}
	return s.lists.GetList(ctx, id)
}

// Load returns a list with its entries, manual items and merged items.
func (s *Service) Load(ctx context.Context, c Caller, id int64) (View, error) {
	ctx, err := enter(ctx, c)
	if err != nil {
		return View{}, err
	}
	return s.lists.Load(ctx, id)
}

// Regenerate rebuilds a list from its entries, dropping the check state of
// items that are gone.
func (s *Service) Regenerate(ctx context.Context, c Caller, id int64) (View, error) {
	ctx, err := enter(ctx, c)
	if err != nil {
		return View{}, err
	}
	return s.lists.Regenerate(ctx, id)
}

// AddEntry puts a recipe, scaled by multiplier, on a list.
func (s *Service) AddEntry(ctx context.Context, c Caller, listID, recipeID int64, multiplier quantity.Rat) (int64, error) {
	ctx, err := enter(ctx, c)
	if err != nil {
		return 0, err
	}
	return s.lists.AddEntry(ctx, listID, recipeID, multiplier)
}

// SetEntryMultiplier rescales a recipe entry.
func (s *Service) SetEntryMultiplier(ctx context.Context, c Caller, entryID int64, multiplier quantity.Rat) error {
	ctx, err := enter(ctx, c)
	if err != nil {
		return err
	}
	return s.lists.SetEntryMultiplier(ctx, entryID, multiplier)
}

// RemoveEntry takes a recipe entry off its list.
func (s *Service) RemoveEntry(ctx context.Context, c Caller, entryID int64) error {
	ctx, err := enter(ctx, c)
	if err != nil {
		return err
	}
	return s.lists.RemoveEntry(ctx, entryID)
}

// SetIncludeOptional sets whether a list keeps optional recipe lines.
func (s *Service) SetIncludeOptional(ctx context.Context, c Caller, listID int64, include bool) error {
	ctx, err := enter(ctx, c)
	if err != nil {
		return err
	}
	return s.lists.SetIncludeOptional(ctx, listID, include)
}

// AddManualItem adds a hand-written item to a list.
func (s *Service) AddManualItem(ctx context.Context, c Caller, listID int64, m ManualItem) (int64, error) {
	ctx, err := enter(ctx, c)
	if err != nil {
		return 0, err
	}
	return s.lists.AddManualItem(ctx, listID, m)
}

// RemoveManualItem takes a manual item off its list.
func (s *Service) RemoveManualItem(ctx context.Context, c Caller, itemID int64) error {
	ctx, err := enter(ctx, c)
	if err != nil {
		return err
	}
	return s.lists.RemoveManualItem(ctx, itemID)
}

// SetChecked ticks an item of a list off, or unticks it.
func (s *Service) SetChecked(ctx context.Context, c Caller, listID int64, key ItemKey, checked bool) error {
	ctx, err := enter(ctx, c)
	if err != nil {
		return err
	}
	return s.lists.SetChecked(ctx, listID, key, checked)
}

// Pantry returns the ingredients the household always has.
func (s *Service) Pantry(ctx context.Context, c Caller) ([]recipe.IngredientID, error) {
	ctx, err := enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.lists.Pantry(ctx)
}

// AddToPantry puts an ingredient in the pantry.
func (s *Service) AddToPantry(ctx context.Context, c Caller, ingredient recipe.IngredientID) error {
	ctx, err := enter(ctx, c)
	if err != nil {
		return err
	}
	return s.lists.AddToPantry(ctx, ingredient)
}

// RemoveFromPantry takes an ingredient out of the pantry.
func (s *Service) RemoveFromPantry(ctx context.Context, c Caller, ingredient recipe.IngredientID) error {
	ctx, err := enter(ctx, c)
	if err != nil {
		return err
	}
	return s.lists.RemoveFromPantry(ctx, ingredient)
}

// Ingredients returns the catalog ingredients the household can name.
func (s *Service) Ingredients(ctx context.Context, c Caller) ([]Ingredient, error) {
	ctx, err := enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.catalog.ListIngredients(ctx)
}

// Ingredient returns one catalog ingredient, such as to name a pantry entry.
func (s *Service) Ingredient(ctx context.Context, c Caller, id int64) (Ingredient, error) {
	ctx, err := enter(ctx, c)
	if err != nil {
		return Ingredient{}, err
	}
	return s.catalog.GetIngredient(ctx, id)
}

// Units returns the catalog units a manual item's amount may use.
func (s *Service) Units(ctx context.Context, c Caller) ([]Unit, error) {
	ctx, err := enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.catalog.ListUnits(ctx)
}

// Recipes returns the household's recipes, for choosing one to add to a list.
func (s *Service) Recipes(ctx context.Context, c Caller) ([]RecipeBrief, error) {
	ctx, err := enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.recipes.Search(ctx, store.SearchQuery{})
}

// ErrNoList is returned by AddToActiveList when the household has no list.
var ErrNoList = errors.New("the household has no shopping list")

// AddToActiveList puts a recipe, at multiplier, on the household's active list
// and returns that list's id. The active list is the most recently created
// one; the store has no other notion of a current list. A recipe already on it
// has its multiplier set to the one given, because the latest request is the
// latest statement of how much to cook. With no list it returns ErrNoList. The
// recipe is checked first, so one of another household is ErrNotFound whether
// or not this household has a list.
func (s *Service) AddToActiveList(ctx context.Context, c Caller, recipeID int64, multiplier quantity.Rat) (int64, error) {
	ctx, err := enter(ctx, c)
	if err != nil {
		return 0, err
	}
	if _, err := s.recipes.Info(ctx, recipeID); err != nil {
		return 0, fmt.Errorf("recipe %d: %w", recipeID, err)
	}
	lists, err := s.lists.Lists(ctx)
	if err != nil {
		return 0, fmt.Errorf("list shopping lists: %w", err)
	}
	if len(lists) == 0 {
		return 0, ErrNoList
	}
	id := lists[len(lists)-1].ID
	view, err := s.lists.Load(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("load list %d: %w", id, err)
	}
	for _, e := range view.Entries {
		if e.RecipeID != recipeID {
			continue
		}
		if err := s.lists.SetEntryMultiplier(ctx, e.ID, multiplier); err != nil {
			return 0, fmt.Errorf("set multiplier of entry %d: %w", e.ID, err)
		}
		return id, nil
	}
	if _, err := s.lists.AddEntry(ctx, id, recipeID, multiplier); err != nil {
		return 0, fmt.Errorf("add recipe %d to list %d: %w", recipeID, id, err)
	}
	return id, nil
}
