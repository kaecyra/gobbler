// Package caller is the verified identity every internal/api service takes as
// its second argument, and the membership check each service runs first
// (ADR-00043, ADR-00018). It is written once so that shopping, recipes and the
// resources after them cannot disagree on who may act in a household.
package caller

import (
	"context"
	"errors"
	"fmt"

	"github.com/kaecyra/gobbler/internal/household"
)

// ErrNotMember is returned for a Caller that is not a verified member of a
// household.
var ErrNotMember = errors.New("not a member of a household")

// Caller is who an operation is for: a verified person and the household they
// claim to act in (ADR-00018). Adapters build it only from an identity they
// have verified, which the service does not do; the service checks that the
// person is a member of the household (Members) on every operation.
type Caller struct {
	// Person is the verified email of the person.
	Person string
	// Household is the household the person is a member of.
	Household household.Household
	// HouseholdKey says the Caller is a household credential, an API key that
	// belongs to the household and not to a person (ADR-00031). Person is then
	// empty, and the adapter has verified the credential's household instead.
	HouseholdKey bool
}

// Members is the membership lookup the services enforce (ADR-00018): the
// household an email belongs to. *household.Store satisfies it, and it is the
// same lookup the membership middleware uses.
type Members interface {
	ForEmail(ctx context.Context, email string) (household.Household, error)
}

// Enter checks c and returns a context that runs in c's household. A person
// must be a member of the household, checked against m; the household comes
// from that lookup, not from the Caller or the context. A household credential
// has no person to look up: the adapter verified the credential, and it names
// the household. It is the first line of every service method.
func Enter(ctx context.Context, m Members, c Caller) (context.Context, error) {
	if c.Household.ID == 0 {
		return nil, ErrNotMember
	}
	if c.HouseholdKey {
		return household.WithContext(ctx, c.Household), nil
	}
	if c.Person == "" {
		return nil, ErrNotMember
	}
	h, err := m.ForEmail(ctx, c.Person)
	switch {
	case errors.Is(err, household.ErrNotFound), errors.Is(err, household.ErrInvalid):
		return nil, fmt.Errorf("%s is in no household: %w", c.Person, ErrNotMember)
	case err != nil:
		return nil, fmt.Errorf("look up the household of %s: %w", c.Person, err)
	case h.ID != c.Household.ID:
		return nil, fmt.Errorf("%s is not a member of household %d: %w", c.Person, c.Household.ID, ErrNotMember)
	}
	return household.WithContext(ctx, h), nil
}
