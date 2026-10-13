// Package sharing sends a recipe, by copy, to another household (ADR-00018,
// Sharing). Only recipes are shared. The sender names a member of the other
// household by email and learns nothing about whether that email belongs to
// one; the receiving household sees the share as pending, and any of its
// members accepts or declines it. Accepting makes an independent recipe in the
// recipient household, with the ingredient lines matched against the
// recipient's catalog: the sender's overlay is never exposed.
//
// The SQL and the stored form of a share are the recipe store's
// (internal/store); this package decides who a share goes to and resolves the
// ingredients it names in the accepting household's catalog.
package sharing

import (
	"context"
	"errors"
	"fmt"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/shopping"
	"github.com/kaecyra/gobbler/internal/store"
)

// Recipes is what Service calls on the recipe store; *store.Store satisfies it.
type Recipes interface {
	Info(ctx context.Context, id int64) (store.Info, error)
	SendShare(ctx context.Context, id, recipientID int64) (int64, error)
	PendingShares(ctx context.Context) ([]store.ShareOffer, error)
	PendingShare(ctx context.Context, id int64) (store.ShareOffer, error)
	AcceptShare(ctx context.Context, id int64, ingredients map[string]int64) (int64, error)
	DeclineShare(ctx context.Context, id int64) error
}

// Households finds the household of an email; *household.Store satisfies it.
type Households interface {
	ForEmail(ctx context.Context, email string) (household.Household, error)
}

// Catalog is what Service calls on the ingredient catalog, in the scope of the
// household on the context; *catalog.Store satisfies it.
type Catalog interface {
	MatchIngredient(ctx context.Context, term string) (catalog.Ingredient, bool, error)
	CreateIngredient(ctx context.Context, in catalog.Ingredient) (catalog.Ingredient, error)
}

// Service sends, lists, accepts and declines recipe shares. It acts for the
// household on the request context and for no other.
type Service struct {
	recipes    Recipes
	households Households
	catalog    Catalog
}

// New returns a Service.
func New(r Recipes, h Households, c Catalog) *Service {
	return &Service{recipes: r, households: h, catalog: c}
}

// Send sends a copy of recipe id of the request's household to the household
// that the member with the given email belongs to. It returns the same nil
// whether or not the email is a member of any household, and whether or not it
// belongs to the sender's own household, so sending reveals who is a member
// nowhere (ADR-00018); only an address that is not an email, or a recipe the
// sender does not have, is an error. The sender's own household is not a
// recipient: nothing is sent to it.
func (s *Service) Send(ctx context.Context, id int64, recipientEmail string) error {
	email, err := household.NormalizeEmail(recipientEmail)
	if err != nil {
		return err
	}
	// The recipe is checked before the recipient, so a recipe that is not the
	// sender's fails the same way for every address.
	if _, err := s.recipes.Info(ctx, id); err != nil {
		return fmt.Errorf("share recipe %d: %w", id, err)
	}
	to, err := s.households.ForEmail(ctx, email)
	if errors.Is(err, household.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find the household of the recipient: %w", err)
	}
	if own, ok := household.FromContext(ctx); ok && own.ID == to.ID {
		return nil
	}
	if _, err := s.recipes.SendShare(ctx, id, to.ID); err != nil {
		return fmt.Errorf("share recipe %d: %w", id, err)
	}
	return nil
}

// Inbox lists the shares waiting for the request's household.
func (s *Service) Inbox(ctx context.Context) ([]store.ShareOffer, error) {
	return s.recipes.PendingShares(ctx)
}

// Offer returns one pending share of the request's household, or
// store.ErrNotFound.
func (s *Service) Offer(ctx context.Context, id int64) (store.ShareOffer, error) {
	return s.recipes.PendingShare(ctx, id)
}

// Accept makes share id a recipe of the request's household and returns the
// recipe's id. Each ingredient that was an entry of the sender's overlay is
// matched against this household's catalog, its own overlay first and the
// central layer second; one that matches neither is added to this household's
// overlay as unreviewed. The sender's entry itself, its aliases and its aisle
// go nowhere. The catalog entries are created before the recipe, as ingestion
// does, and stay if the recipe cannot be stored.
func (s *Service) Accept(ctx context.Context, id int64) (int64, error) {
	offer, err := s.recipes.PendingShare(ctx, id)
	if err != nil {
		return 0, err
	}
	ids := make(map[string]int64, len(offer.Ingredients))
	for _, name := range offer.Ingredients {
		ing, ok, err := s.catalog.MatchIngredient(ctx, name)
		if err != nil {
			return 0, fmt.Errorf("match shared ingredient %q: %w", name, err)
		}
		if !ok {
			if ing, err = s.catalog.CreateIngredient(ctx, catalog.Ingredient{Name: name, Aisle: shopping.Uncategorised, Reviewed: false}); err != nil {
				return 0, fmt.Errorf("add shared ingredient %q to the catalog: %w", name, err)
			}
		}
		ids[name] = ing.ID
	}
	return s.recipes.AcceptShare(ctx, id, ids)
}

// Decline declines share id; nothing enters the household's recipes.
func (s *Service) Decline(ctx context.Context, id int64) error {
	return s.recipes.DeclineShare(ctx, id)
}
