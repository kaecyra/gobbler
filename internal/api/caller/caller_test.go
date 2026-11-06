package caller_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kaecyra/gobbler/internal/api/caller"
	"github.com/kaecyra/gobbler/internal/household"
)

type members map[string]household.Household

func (m members) ForEmail(_ context.Context, email string) (household.Household, error) {
	if h, ok := m[email]; ok {
		return h, nil
	}
	return household.Household{}, household.ErrNotFound
}

type failing struct{}

func (failing) ForEmail(context.Context, string) (household.Household, error) {
	return household.Household{}, errors.New("db down")
}

func TestEnter(t *testing.T) {
	smiths := household.Household{ID: 1, Name: "Smiths"}
	joneses := household.Household{ID: 2, Name: "Joneses"}
	m := members{"ann@example.com": smiths}

	tests := []struct {
		name    string
		m       caller.Members
		c       caller.Caller
		wantNot bool // want ErrNotMember
		wantErr bool
	}{
		{"member", m, caller.Caller{Person: "ann@example.com", Household: smiths}, false, false},
		{"member claiming another household", m, caller.Caller{Person: "ann@example.com", Household: joneses}, true, true},
		{"person in no household", m, caller.Caller{Person: "bob@example.com", Household: smiths}, true, true},
		{"no household", m, caller.Caller{Person: "ann@example.com"}, true, true},
		{"no person", m, caller.Caller{Household: smiths}, true, true},
		{"zero caller", m, caller.Caller{}, true, true},
		{"household key admitted by its household alone", m, caller.Caller{Household: joneses, HouseholdKey: true}, false, false},
		{"household key with no household", m, caller.Caller{HouseholdKey: true}, true, true},
		{"lookup failure is not a refusal", failing{}, caller.Caller{Person: "ann@example.com", Household: smiths}, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, err := caller.Enter(context.Background(), tc.m, tc.c)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if got := errors.Is(err, caller.ErrNotMember); got != tc.wantNot {
				t.Fatalf("ErrNotMember = %v, want %v (err %v)", got, tc.wantNot, err)
			}
			if err == nil {
				if h, ok := household.FromContext(ctx); !ok || h.ID != tc.c.Household.ID {
					t.Fatalf("context household = %v %v, want %d", h, ok, tc.c.Household.ID)
				}
			}
		})
	}
}
