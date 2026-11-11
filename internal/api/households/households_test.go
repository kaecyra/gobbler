package households_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kaecyra/gobbler/internal/api/households"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
)

const (
	annEmail  = "ann@example.com"
	annaEmail = "anna@example.com"
	joEmail   = "jo@example.com"
	rootEmail = "root@example.com"
	newEmail  = "new@example.com"
)

type fixture struct {
	svc      *households.Service
	store    *households.Store
	ann, jo  households.Caller
	root     households.Caller
	smiths   households.Household
	joneses  households.Household
	stranger households.Caller
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	hs := households.NewStore(d)
	smiths, err := hs.Create(ctx, "Smiths", annEmail)
	if err != nil {
		t.Fatal(err)
	}
	joneses, err := hs.Create(ctx, "Joneses", joEmail)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := households.New(households.Deps{Households: hs, Operators: config.Operator{Emails: []string{rootEmail}}})
	if err != nil {
		t.Fatal(err)
	}
	return fixture{
		svc: svc, store: hs, smiths: smiths, joneses: joneses,
		ann:      households.Caller{Person: annEmail, Household: smiths},
		jo:       households.Caller{Person: joEmail, Household: joneses},
		root:     households.Caller{Person: rootEmail},
		stranger: households.Caller{Person: "nobody@example.com", Household: smiths},
	}
}

func TestNewRequiresTheStore(t *testing.T) {
	if _, err := households.New(households.Deps{}); err == nil {
		t.Fatal("New without a store = nil, want an error")
	}
}

// Every member operation, as a function of the Caller it is asked for.
func memberOps(f fixture) map[string]func(c households.Caller) error {
	ctx := context.Background()
	return map[string]func(c households.Caller) error{
		"Members": func(c households.Caller) error { _, err := f.svc.Members(ctx, c); return err },
		"Pending": func(c households.Caller) error { _, err := f.svc.Pending(ctx, c); return err },
		"Invite":  func(c households.Caller) error { _, err := f.svc.Invite(ctx, c, newEmail); return err },
		"Revoke":  func(c households.Caller) error { return f.svc.Revoke(ctx, c, "x") },
		"Resend":  func(c households.Caller) error { return f.svc.Resend(ctx, c, "x") },
		"Remove":  func(c households.Caller) error { return f.svc.Remove(ctx, c, joEmail) },
		"Leave":   func(c households.Caller) error { return f.svc.Leave(ctx, c) },
		"Delete":  func(c households.Caller) error { return f.svc.Delete(ctx, c, "Smiths") },
		"SetHouseStyle": func(c households.Caller) error {
			return f.svc.SetHouseStyle(ctx, c, "Weights in grams.")
		},
	}
}

func TestAMemberOperationRefusesACallerThatIsNoMember(t *testing.T) {
	f := newFixture(t)
	cases := map[string]households.Caller{
		"a person of another household":        f.stranger,
		"a person who names another household": {Person: joEmail, Household: f.smiths},
		"no person":                            {Household: f.smiths},
		"no household":                         {Person: annEmail},
		"a household key":                      {Household: f.smiths, HouseholdKey: true},
		"a household key that names a person":  {Person: annEmail, Household: f.smiths, HouseholdKey: true},
	}
	for opName, op := range memberOps(f) {
		for who, c := range cases {
			if err := op(c); !errors.Is(err, households.ErrNotMember) {
				t.Errorf("%s as %s = %v, want ErrNotMember", opName, who, err)
			}
		}
	}
	// Nothing was changed by the refused calls.
	ms, err := f.store.Members(context.Background(), f.smiths.ID)
	if err != nil || len(ms) != 1 {
		t.Fatalf("Smiths members after refusals = %v, %v, want just Ann", ms, err)
	}
	ms, err = f.store.Members(context.Background(), f.joneses.ID)
	if err != nil || len(ms) != 1 {
		t.Fatalf("Joneses members after refusals = %v, %v, want just Jo", ms, err)
	}
}

func TestAMemberInvitesAndRevokes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	code, err := f.svc.Invite(ctx, f.ann, newEmail)
	if err != nil || code == "" {
		t.Fatalf("Invite = %q, %v", code, err)
	}
	pending, err := f.svc.Pending(ctx, f.ann)
	if err != nil || len(pending) != 1 || pending[0].Email != newEmail || pending[0].HouseholdID != f.smiths.ID {
		t.Fatalf("Pending = %+v, %v, want the invitation into the Smiths", pending, err)
	}
	if other, err := f.svc.Pending(ctx, f.jo); err != nil || len(other) != 0 {
		t.Fatalf("Joneses Pending = %+v, %v, want none", other, err)
	}
	if err := f.svc.Revoke(ctx, f.ann, code); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if pending, _ := f.svc.Pending(ctx, f.ann); len(pending) != 0 {
		t.Fatalf("Pending after Revoke = %+v, want none", pending)
	}
}

func TestInvitingAnInvalidAddressIsInvalid(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Invite(context.Background(), f.ann, "not an email"); !errors.Is(err, households.ErrInvalid) {
		t.Fatalf("Invite = %v, want ErrInvalid", err)
	}
}

func TestAMemberJoinsAndLeaves(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	code, err := f.svc.Invite(ctx, f.ann, newEmail)
	if err != nil {
		t.Fatal(err)
	}
	newcomer := households.Caller{Person: newEmail}
	inv, err := f.svc.InvitationFor(ctx, newcomer, code)
	if err != nil || inv.HouseholdName != "Smiths" || inv.Kind != households.KindJoin {
		t.Fatalf("InvitationFor = %+v, %v, want the Smiths' join invitation", inv, err)
	}
	h, err := f.svc.Accept(ctx, newcomer, code, "")
	if err != nil || h.ID != f.smiths.ID {
		t.Fatalf("Accept = %+v, %v, want the Smiths", h, err)
	}
	if ms, err := f.svc.Members(ctx, f.ann); err != nil || len(ms) != 2 {
		t.Fatalf("Members = %v, %v, want Ann and the newcomer", ms, err)
	}
	member := households.Caller{Person: newEmail, Household: h}
	if err := f.svc.Leave(ctx, member); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if err := f.svc.Leave(ctx, member); !errors.Is(err, households.ErrNotMember) {
		t.Fatalf("Leave after leaving = %v, want ErrNotMember", err)
	}
}

func TestOnlyTheAddresseeSeesAnInvitation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	code, err := f.svc.Invite(ctx, f.ann, newEmail)
	if err != nil {
		t.Fatal(err)
	}
	other := households.Caller{Person: "else@example.com"}
	if _, err := f.svc.InvitationFor(ctx, other, code); !errors.Is(err, households.ErrNotFound) {
		t.Errorf("InvitationFor someone else = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.Accept(ctx, other, code, ""); !errors.Is(err, households.ErrNotFound) {
		t.Errorf("Accept as someone else = %v, want ErrNotFound", err)
	}
}

func TestTheInvitationOperationsNeedAPerson(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for who, c := range map[string]households.Caller{
		"no person":                           {},
		"a household key":                     {Household: f.smiths, HouseholdKey: true},
		"a household key that names a person": {Person: newEmail, Household: f.smiths, HouseholdKey: true},
	} {
		if _, err := f.svc.InvitationFor(ctx, c, "x"); !errors.Is(err, households.ErrNotMember) {
			t.Errorf("InvitationFor as %s = %v, want ErrNotMember", who, err)
		}
		if _, err := f.svc.Accept(ctx, c, "x", ""); !errors.Is(err, households.ErrNotMember) {
			t.Errorf("Accept as %s = %v, want ErrNotMember", who, err)
		}
	}
}

func TestAMemberRemovesAnotherAndDeletes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	c2, err := f.svc.Invite(ctx, f.ann, annaEmail)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Accept(ctx, households.Caller{Person: annaEmail}, c2, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Remove(ctx, f.ann, annaEmail); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := f.svc.Delete(ctx, f.ann, "wrong"); !errors.Is(err, households.ErrConfirmation) {
		t.Fatalf("Delete with the wrong name = %v, want ErrConfirmation", err)
	}
	if err := f.svc.Delete(ctx, f.ann, "Smiths"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := f.svc.Members(ctx, f.ann); !errors.Is(err, households.ErrNotMember) {
		t.Fatalf("Members after the household was deleted = %v, want ErrNotMember", err)
	}
}

func TestAnOperatorWhoIsNoMemberInvitesAndRevokes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	code, err := f.svc.OperatorInvite(ctx, f.root, newEmail)
	if err != nil || code == "" {
		t.Fatalf("OperatorInvite = %q, %v", code, err)
	}
	pending, err := f.svc.OperatorPending(ctx, f.root)
	if err != nil || len(pending) != 1 || pending[0].Email != newEmail || pending[0].Kind != households.KindHousehold {
		t.Fatalf("OperatorPending = %+v, %v, want the household invitation", pending, err)
	}
	if err := f.svc.OperatorRevoke(ctx, f.root, code); err != nil {
		t.Fatalf("OperatorRevoke: %v", err)
	}
	if pending, _ := f.svc.OperatorPending(ctx, f.root); len(pending) != 0 {
		t.Fatalf("OperatorPending after revoke = %+v, want none", pending)
	}
}

func TestANonOperatorIsRefusedTheOperatorOperations(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ops := map[string]func(c households.Caller) error{
		"OperatorPending": func(c households.Caller) error { _, err := f.svc.OperatorPending(ctx, c); return err },
		"OperatorInvite":  func(c households.Caller) error { _, err := f.svc.OperatorInvite(ctx, c, newEmail); return err },
		"OperatorRevoke":  func(c households.Caller) error { return f.svc.OperatorRevoke(ctx, c, "x") },
		"OperatorResend":  func(c households.Caller) error { return f.svc.OperatorResend(ctx, c, "x") },
	}
	for name, op := range ops {
		for who, c := range map[string]households.Caller{
			"a member":                               f.ann,
			"no person":                              {Household: f.smiths},
			"a household key":                        {Household: f.smiths, HouseholdKey: true},
			"a household key that names an operator": {Person: rootEmail, Household: f.smiths, HouseholdKey: true},
		} {
			if err := op(c); !errors.Is(err, households.ErrNotMember) {
				t.Errorf("%s as %s = %v, want ErrNotMember", name, who, err)
			}
		}
	}
}

func TestWithoutOperatorsNobodyIsOperator(t *testing.T) {
	f := newFixture(t)
	svc, err := households.New(households.Deps{Households: f.store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.OperatorInvite(context.Background(), f.root, newEmail); !errors.Is(err, households.ErrNotMember) {
		t.Fatalf("OperatorInvite without operators = %v, want ErrNotMember", err)
	}
}

func TestAnOperatorHasNoMemberRightsFromBeingOne(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Members(context.Background(), households.Caller{Person: rootEmail, Household: f.smiths}); !errors.Is(err, households.ErrNotMember) {
		t.Fatalf("Members as an operator who is no member = %v, want ErrNotMember", err)
	}
}
