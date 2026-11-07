// Package catalog is the catalog service: every operation on the ingredient,
// equipment and unit catalog that the web handlers and the MCP tools perform,
// written once (ADR-00043). It is in-process. It is not a network API, it
// speaks neither HTTP nor JSON and it reads no cookie or header. It follows the
// pattern internal/api/journal and internal/api/sharing set: a method takes the
// context, then the caller.Caller, then its own arguments; it refuses a Caller
// that is not a member of the household with ErrNotMember before touching a
// row (ADR-00018); and it returns the domain's errors, which the web turns into
// statuses in internal/web/layout (statusTable) and the MCP tools into tool
// errors. The Caller is the same whichever credential authenticated: a session,
// a household key, a personal key or an OAuth grant (ADR-00031).
//
// The catalog itself (the central layer, each household's overlay, matching and
// the merge) stays in internal/catalog, which this package wraps. The package
// re-exports the types and errors its callers need as aliases, so internal/web
// and internal/mcp import this package and not internal/catalog; the
// catalog-through-api rule in .golangci.yml fails make lint when they do.
//
// Two things here are not in the siblings. Bound serves the callers whose
// lookups carry only a context (the line builder in internal/recipe/display, the
// shopping service's catalog reads, a page's Catalog interface): it holds a
// function that names the Caller from that context and sends every call through
// the Service. And the operator's curation of the central catalog (promotions
// and the equipment review) is not here: an operator need not be a household
// member, so no membership check fits it, and what should stand in its place is
// not decided.
package catalog

import (
	"context"
	"database/sql"
	"errors"

	"github.com/kaecyra/gobbler/internal/api/caller"
	domain "github.com/kaecyra/gobbler/internal/catalog"
)

// Caller is who an operation is for (caller.Caller).
type Caller = caller.Caller

// Members is the membership lookup the service enforces (caller.Members).
type Members = caller.Members

// The catalog values the callers of the service work with.
type (
	// Ingredient is a catalog ingredient (catalog.Ingredient).
	Ingredient = domain.Ingredient
	// Equipment is a catalog piece of equipment (catalog.Equipment).
	Equipment = domain.Equipment
	// Unit is a catalog unit of measure (catalog.Unit).
	Unit = domain.Unit
	// Store is the catalog store (catalog.Store). The router builds it with
	// NewStore and registers the merge hooks of the stores that reference
	// ingredients on it, so the web and MCP packages name no domain package.
	Store = domain.Store
	// Usage counts what a merge would re-point (catalog.Usage).
	Usage = domain.Usage
)

// The errors a service method returns, for callers to branch on with
// errors.Is. They are the domain's own values, so errors.Is matches either name.
var (
	// ErrNotFound is an entry the household cannot see (catalog.ErrNotFound).
	ErrNotFound = domain.ErrNotFound
	// ErrConflict is a name or alias already taken (catalog.ErrConflict).
	ErrConflict = domain.ErrConflict
	// ErrInvalid is an entry that fails validation (catalog.ErrInvalid).
	ErrInvalid = domain.ErrInvalid
	// ErrCentral is a change to a central entry that only the operator may make
	// (catalog.ErrCentral). It arrives wrapped in ErrInvalid.
	ErrCentral = domain.ErrCentral
	// ErrNotMember is returned for a Caller that is not a verified member of a
	// household (caller.ErrNotMember).
	ErrNotMember = caller.ErrNotMember
)

// NewStore returns the catalog store over d, which must already be migrated.
// The router builds it here so that it, too, names no domain package.
func NewStore(d *sql.DB) *Store { return domain.New(d) }

// NewUsage returns the usage counter over s, with no counter registered.
func NewUsage(s *Store) *Usage { return domain.NewUsage(s) }

// Catalog is what the service calls on the catalog store. *catalog.Store
// satisfies it. Every call runs in the household the Caller was admitted to.
type Catalog interface {
	ListIngredients(ctx context.Context) ([]Ingredient, error)
	ListUnreviewedIngredients(ctx context.Context) ([]Ingredient, error)
	GetIngredient(ctx context.Context, id int64) (Ingredient, error)
	MatchIngredient(ctx context.Context, term string) (Ingredient, bool, error)
	CreateIngredient(ctx context.Context, in Ingredient) (Ingredient, error)
	UpdateIngredient(ctx context.Context, in Ingredient) error
	SuggestIngredient(ctx context.Context, id int64) error
	UnsuggestIngredient(ctx context.Context, id int64) error
	MergeIngredients(ctx context.Context, winnerID, loserID int64) (Ingredient, error)
	ListEquipment(ctx context.Context) ([]Equipment, error)
	GetEquipment(ctx context.Context, id int64) (Equipment, error)
	MatchEquipment(ctx context.Context, term string) (Equipment, bool, error)
	CreateEquipment(ctx context.Context, in Equipment) (Equipment, error)
	ListUnits(ctx context.Context) ([]Unit, error)
	MatchUnit(ctx context.Context, term string) (Unit, bool, error)
}

// UsageCounter counts the rows a merge of an ingredient would re-point.
// *catalog.Usage satisfies it.
type UsageCounter interface {
	IngredientUsage(ctx context.Context, id int64) (int64, error)
}

// Deps is what the service is built from.
type Deps struct {
	// Catalog is the catalog store. Required.
	Catalog Catalog
	// Members is the membership lookup. Required.
	Members Members
	// Usage counts the references a merge re-points. Optional: without it
	// IngredientUsage is an error.
	Usage UsageCounter
}

// Service is the catalog operations. It is safe for concurrent use.
type Service struct {
	catalog Catalog
	members Members
	usage   UsageCounter
}

// New returns a Service. Catalog and Members are required; a missing one is an
// error here, not a nil dereference on the first request.
func New(d Deps) (*Service, error) {
	if d.Catalog == nil || d.Members == nil {
		return nil, errors.New("catalog service: catalog and members are required")
	}
	return &Service{catalog: d.Catalog, members: d.Members, usage: d.Usage}, nil
}

// enter checks the Caller and returns a context that runs in its household
// (caller.Enter). It is the first line of every method.
func (s *Service) enter(ctx context.Context, c Caller) (context.Context, error) {
	return caller.Enter(ctx, s.members, c)
}

// ListIngredients returns the ingredients the household sees: the central
// catalog and its own overlay.
func (s *Service) ListIngredients(ctx context.Context, c Caller) ([]Ingredient, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.catalog.ListIngredients(ctx)
}

// ListUnreviewedIngredients returns the ingredients awaiting review.
func (s *Service) ListUnreviewedIngredients(ctx context.Context, c Caller) ([]Ingredient, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.catalog.ListUnreviewedIngredients(ctx)
}

// GetIngredient returns ingredient id, or ErrNotFound when the household cannot
// see it.
func (s *Service) GetIngredient(ctx context.Context, c Caller, id int64) (Ingredient, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Ingredient{}, err
	}
	return s.catalog.GetIngredient(ctx, id)
}

// MatchIngredient finds the ingredient a term names, or reports false.
func (s *Service) MatchIngredient(ctx context.Context, c Caller, term string) (Ingredient, bool, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Ingredient{}, false, err
	}
	return s.catalog.MatchIngredient(ctx, term)
}

// CreateIngredient adds an ingredient to the household's overlay.
func (s *Service) CreateIngredient(ctx context.Context, c Caller, in Ingredient) (Ingredient, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Ingredient{}, err
	}
	return s.catalog.CreateIngredient(ctx, in)
}

// UpdateIngredient saves in over the ingredient with its id.
func (s *Service) UpdateIngredient(ctx context.Context, c Caller, in Ingredient) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	return s.catalog.UpdateIngredient(ctx, in)
}

// SuggestIngredient flags ingredient id of the household's overlay for the
// operator to promote.
func (s *Service) SuggestIngredient(ctx context.Context, c Caller, id int64) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	return s.catalog.SuggestIngredient(ctx, id)
}

// UnsuggestIngredient withdraws the suggestion of ingredient id.
func (s *Service) UnsuggestIngredient(ctx context.Context, c Caller, id int64) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	return s.catalog.UnsuggestIngredient(ctx, id)
}

// MergeIngredients folds ingredient loserID into winnerID and re-points the rows
// that referenced it.
func (s *Service) MergeIngredients(ctx context.Context, c Caller, winnerID, loserID int64) (Ingredient, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Ingredient{}, err
	}
	return s.catalog.MergeIngredients(ctx, winnerID, loserID)
}

// IngredientUsage returns how many rows a merge of ingredient id would
// re-point. It is ErrNotFound when the household cannot see the ingredient.
func (s *Service) IngredientUsage(ctx context.Context, c Caller, id int64) (int64, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return 0, err
	}
	if s.usage == nil {
		return 0, errors.New("catalog service: no usage counter configured")
	}
	return s.usage.IngredientUsage(ctx, id)
}

// ListEquipment returns the catalog's equipment.
func (s *Service) ListEquipment(ctx context.Context, c Caller) ([]Equipment, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.catalog.ListEquipment(ctx)
}

// GetEquipment returns equipment id, or ErrNotFound.
func (s *Service) GetEquipment(ctx context.Context, c Caller, id int64) (Equipment, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Equipment{}, err
	}
	return s.catalog.GetEquipment(ctx, id)
}

// MatchEquipment finds the equipment a term names, or reports false.
func (s *Service) MatchEquipment(ctx context.Context, c Caller, term string) (Equipment, bool, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Equipment{}, false, err
	}
	return s.catalog.MatchEquipment(ctx, term)
}

// CreateEquipment adds a piece of equipment to the catalog.
func (s *Service) CreateEquipment(ctx context.Context, c Caller, in Equipment) (Equipment, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Equipment{}, err
	}
	return s.catalog.CreateEquipment(ctx, in)
}

// ListUnits returns the catalog's units.
func (s *Service) ListUnits(ctx context.Context, c Caller) ([]Unit, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.catalog.ListUnits(ctx)
}

// MatchUnit finds the unit a term names, or reports false.
func (s *Service) MatchUnit(ctx context.Context, c Caller, term string) (Unit, bool, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Unit{}, false, err
	}
	return s.catalog.MatchUnit(ctx, term)
}

// Bound is the Service for callers whose lookups carry a context and no Caller:
// the line builder, the shopping service's catalog reads, a page's Catalog
// interface. Each call asks who for the Caller of its context and goes through
// the Service, so the membership check runs on every call. A who that finds no
// identity returns the zero Caller, which the Service refuses with ErrNotMember:
// a Bound with nobody to speak for fails closed.
type Bound struct {
	s   *Service
	who func(ctx context.Context) Caller
}

// Bind returns the Service speaking for the Caller who names from a call's
// context. A web adapter passes caller.FromContext; the MCP adapter builds the
// Caller from the credential on the context.
func (s *Service) Bind(who func(ctx context.Context) Caller) *Bound {
	return &Bound{s: s, who: who}
}

// ListIngredients is Service.ListIngredients.
func (b *Bound) ListIngredients(ctx context.Context) ([]Ingredient, error) {
	return b.s.ListIngredients(ctx, b.who(ctx))
}

// ListUnreviewedIngredients is Service.ListUnreviewedIngredients.
func (b *Bound) ListUnreviewedIngredients(ctx context.Context) ([]Ingredient, error) {
	return b.s.ListUnreviewedIngredients(ctx, b.who(ctx))
}

// GetIngredient is Service.GetIngredient.
func (b *Bound) GetIngredient(ctx context.Context, id int64) (Ingredient, error) {
	return b.s.GetIngredient(ctx, b.who(ctx), id)
}

// MatchIngredient is Service.MatchIngredient.
func (b *Bound) MatchIngredient(ctx context.Context, term string) (Ingredient, bool, error) {
	return b.s.MatchIngredient(ctx, b.who(ctx), term)
}

// CreateIngredient is Service.CreateIngredient.
func (b *Bound) CreateIngredient(ctx context.Context, in Ingredient) (Ingredient, error) {
	return b.s.CreateIngredient(ctx, b.who(ctx), in)
}

// UpdateIngredient is Service.UpdateIngredient.
func (b *Bound) UpdateIngredient(ctx context.Context, in Ingredient) error {
	return b.s.UpdateIngredient(ctx, b.who(ctx), in)
}

// SuggestIngredient is Service.SuggestIngredient.
func (b *Bound) SuggestIngredient(ctx context.Context, id int64) error {
	return b.s.SuggestIngredient(ctx, b.who(ctx), id)
}

// UnsuggestIngredient is Service.UnsuggestIngredient.
func (b *Bound) UnsuggestIngredient(ctx context.Context, id int64) error {
	return b.s.UnsuggestIngredient(ctx, b.who(ctx), id)
}

// MergeIngredients is Service.MergeIngredients.
func (b *Bound) MergeIngredients(ctx context.Context, winnerID, loserID int64) (Ingredient, error) {
	return b.s.MergeIngredients(ctx, b.who(ctx), winnerID, loserID)
}

// IngredientUsage is Service.IngredientUsage.
func (b *Bound) IngredientUsage(ctx context.Context, id int64) (int64, error) {
	return b.s.IngredientUsage(ctx, b.who(ctx), id)
}

// ListEquipment is Service.ListEquipment.
func (b *Bound) ListEquipment(ctx context.Context) ([]Equipment, error) {
	return b.s.ListEquipment(ctx, b.who(ctx))
}

// GetEquipment is Service.GetEquipment.
func (b *Bound) GetEquipment(ctx context.Context, id int64) (Equipment, error) {
	return b.s.GetEquipment(ctx, b.who(ctx), id)
}

// MatchEquipment is Service.MatchEquipment.
func (b *Bound) MatchEquipment(ctx context.Context, term string) (Equipment, bool, error) {
	return b.s.MatchEquipment(ctx, b.who(ctx), term)
}

// CreateEquipment is Service.CreateEquipment.
func (b *Bound) CreateEquipment(ctx context.Context, in Equipment) (Equipment, error) {
	return b.s.CreateEquipment(ctx, b.who(ctx), in)
}

// ListUnits is Service.ListUnits.
func (b *Bound) ListUnits(ctx context.Context) ([]Unit, error) {
	return b.s.ListUnits(ctx, b.who(ctx))
}

// MatchUnit is Service.MatchUnit.
func (b *Bound) MatchUnit(ctx context.Context, term string) (Unit, bool, error) {
	return b.s.MatchUnit(ctx, b.who(ctx), term)
}
