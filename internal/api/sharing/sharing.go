// Package sharing is the recipe-sharing service: sending a recipe copy to
// another household and accepting or declining the offers a household
// received, written once for the web handlers to call (ADR-00043). It is
// in-process, speaks neither HTTP nor JSON and reads no cookie or header. It
// follows the pattern internal/api/shopping set: a method takes the context,
// then the caller.Caller, then its own arguments; it refuses a Caller that is
// not a member of the household with ErrNotMember before touching a row
// (ADR-00018); and it returns the domain's errors, which internal/web/layout
// (statusTable) maps for the web.
//
// The rules of a share (who it goes to, how the ingredients are matched in the
// accepting household's catalog) stay in internal/sharing, which this package
// wraps. The package re-exports the types and errors its callers need as
// aliases, so internal/web imports this package and not internal/sharing or
// internal/store. A depguard rule in .golangci.yml fails make lint when
// internal/web or internal/mcp imports internal/sharing.
package sharing

import (
	"context"
	"errors"

	"github.com/kaecyra/gobbler/internal/api/caller"
	"github.com/kaecyra/gobbler/internal/household"
	domain "github.com/kaecyra/gobbler/internal/sharing"
	"github.com/kaecyra/gobbler/internal/store"
)

// Caller is who an operation is for (caller.Caller).
type Caller = caller.Caller

// Members is the membership lookup the service enforces (caller.Members).
type Members = caller.Members

// ShareOffer is a share in the recipient household's inbox (store.ShareOffer).
type ShareOffer = store.ShareOffer

// Recipes is what the service calls on the recipe store (sharing.Recipes).
type Recipes = domain.Recipes

// Households finds the household of an email (sharing.Households).
type Households = domain.Households

// Catalog is what the service calls on the ingredient catalog (sharing.Catalog).
type Catalog = domain.Catalog

// The errors a service method returns, for callers to branch on with errors.Is.
var (
	// ErrNotFound is a recipe or a share the household does not have
	// (store.ErrNotFound).
	ErrNotFound = store.ErrNotFound
	// ErrInvalid is a recipient address that is not an email
	// (household.ErrInvalid).
	ErrInvalid = household.ErrInvalid
	// ErrNotMember is returned for a Caller that is not a verified member of a
	// household (caller.ErrNotMember).
	ErrNotMember = caller.ErrNotMember
)

// Deps is what the service is built from.
type Deps struct {
	Recipes    Recipes
	Households Households
	Catalog    Catalog
	Members    Members
}

// Service sends, lists, accepts and declines recipe shares for a Caller. It is
// safe for concurrent use.
type Service struct {
	shares  *domain.Service
	members Members
}

// New returns a Service. All four dependencies are required; a missing one is
// an error here, not a nil dereference on the first request.
func New(d Deps) (*Service, error) {
	if d.Recipes == nil || d.Households == nil || d.Catalog == nil || d.Members == nil {
		return nil, errors.New("sharing service: recipes, households, catalog and members are required")
	}
	return &Service{shares: domain.New(d.Recipes, d.Households, d.Catalog), members: d.Members}, nil
}

// enter checks the Caller and returns a context that runs in its household
// (caller.Enter). It is the first line of every method.
func (s *Service) enter(ctx context.Context, c Caller) (context.Context, error) {
	return caller.Enter(ctx, s.members, c)
}

// Send sends a copy of recipe id of the Caller's household to the household of
// the member with the given email (sharing.Service.Send). It answers every
// well-formed address alike, so sending reveals who is a member nowhere.
func (s *Service) Send(ctx context.Context, c Caller, id int64, recipientEmail string) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	return s.shares.Send(ctx, id, recipientEmail)
}

// Inbox lists the shares waiting for the Caller's household.
func (s *Service) Inbox(ctx context.Context, c Caller) ([]ShareOffer, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.shares.Inbox(ctx)
}

// Accept makes share id a recipe of the Caller's household and returns the
// recipe's id (sharing.Service.Accept).
func (s *Service) Accept(ctx context.Context, c Caller, id int64) (int64, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return 0, err
	}
	return s.shares.Accept(ctx, id)
}

// Decline declines share id; nothing enters the household's recipes.
func (s *Service) Decline(ctx context.Context, c Caller, id int64) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	return s.shares.Decline(ctx, id)
}
