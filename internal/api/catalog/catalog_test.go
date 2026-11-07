package catalog_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	apicatalog "github.com/kaecyra/gobbler/internal/api/catalog"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
)

type fixture struct {
	svc     *apicatalog.Service
	store   *catalog.Store
	members *household.Store
	ann     apicatalog.Caller // member of the Smiths
	jo      apicatalog.Caller // member of the Joneses
	smiths  household.Household
	pepper  apicatalog.Ingredient // a Smiths overlay entry
}

func newFixture(t *testing.T) *fixture {
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
	hs := household.New(d)
	smiths, err := hs.Create(ctx, "Smiths", "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	joneses, err := hs.Create(ctx, "Joneses", "jo@example.com")
	if err != nil {
		t.Fatal(err)
	}
	st := apicatalog.NewStore(d)
	svc, err := apicatalog.New(apicatalog.Deps{Catalog: st, Members: hs, Usage: apicatalog.NewUsage(st)})
	if err != nil {
		t.Fatal(err)
	}
	pepper, err := st.CreateIngredient(household.WithContext(ctx, smiths), apicatalog.Ingredient{Name: "zz smiths pepper", Aisle: "spices"})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{
		svc: svc, store: st, members: hs, smiths: smiths, pepper: pepper,
		ann: apicatalog.Caller{Person: "ann@example.com", Household: smiths},
		jo:  apicatalog.Caller{Person: "jo@example.com", Household: joneses},
	}
}

func TestNewRefusesAMissingCollaborator(t *testing.T) {
	if _, err := apicatalog.New(apicatalog.Deps{}); err == nil {
		t.Fatal("New with no collaborators succeeded")
	}
}

// The errors are the domain's own values, so the status table matches either
// name.
func TestErrorsAreTheDomainsOwn(t *testing.T) {
	for name, pair := range map[string][2]error{
		"ErrNotFound": {apicatalog.ErrNotFound, catalog.ErrNotFound},
		"ErrConflict": {apicatalog.ErrConflict, catalog.ErrConflict},
		"ErrInvalid":  {apicatalog.ErrInvalid, catalog.ErrInvalid},
		"ErrCentral":  {apicatalog.ErrCentral, catalog.ErrCentral},
	} {
		if !errors.Is(pair[0], pair[1]) {
			t.Errorf("%s is not the domain's error", name)
		}
	}
}

// Nobody who is not a verified member of a household gets an operation done,
// whatever the context carries (ADR-00018).
func TestEveryOperationRefusesANonMember(t *testing.T) {
	f := newFixture(t)
	// The context names the Smiths: the Caller, not the context, decides.
	ctx := household.WithContext(context.Background(), f.smiths)
	id := f.pepper.ID
	ops := map[string]func(c apicatalog.Caller) error{
		"ListIngredients":           func(c apicatalog.Caller) error { _, err := f.svc.ListIngredients(ctx, c); return err },
		"ListUnreviewedIngredients": func(c apicatalog.Caller) error { _, err := f.svc.ListUnreviewedIngredients(ctx, c); return err },
		"GetIngredient":             func(c apicatalog.Caller) error { _, err := f.svc.GetIngredient(ctx, c, id); return err },
		"MatchIngredient": func(c apicatalog.Caller) error {
			_, _, err := f.svc.MatchIngredient(ctx, c, "zz smiths pepper")
			return err
		},
		"CreateIngredient": func(c apicatalog.Caller) error {
			_, err := f.svc.CreateIngredient(ctx, c, apicatalog.Ingredient{Name: "zz intruder", Aisle: "x"})
			return err
		},
		"UpdateIngredient": func(c apicatalog.Caller) error {
			return f.svc.UpdateIngredient(ctx, c, apicatalog.Ingredient{ID: id, Name: "zz renamed", Aisle: "x"})
		},
		"SuggestIngredient":   func(c apicatalog.Caller) error { return f.svc.SuggestIngredient(ctx, c, id) },
		"UnsuggestIngredient": func(c apicatalog.Caller) error { return f.svc.UnsuggestIngredient(ctx, c, id) },
		"MergeIngredients": func(c apicatalog.Caller) error {
			_, err := f.svc.MergeIngredients(ctx, c, id, id+1)
			return err
		},
		"IngredientUsage": func(c apicatalog.Caller) error { _, err := f.svc.IngredientUsage(ctx, c, id); return err },
		"ListEquipment":   func(c apicatalog.Caller) error { _, err := f.svc.ListEquipment(ctx, c); return err },
		"GetEquipment":    func(c apicatalog.Caller) error { _, err := f.svc.GetEquipment(ctx, c, 1); return err },
		"MatchEquipment":  func(c apicatalog.Caller) error { _, _, err := f.svc.MatchEquipment(ctx, c, "pan"); return err },
		"CreateEquipment": func(c apicatalog.Caller) error {
			_, err := f.svc.CreateEquipment(ctx, c, apicatalog.Equipment{Name: "zz intruder pan"})
			return err
		},
		"ListUnits": func(c apicatalog.Caller) error { _, err := f.svc.ListUnits(ctx, c); return err },
		"MatchUnit": func(c apicatalog.Caller) error { _, _, err := f.svc.MatchUnit(ctx, c, "cup"); return err },
	}
	callers := map[string]apicatalog.Caller{
		"zero caller":     {},
		"no household":    {Person: "ann@example.com"},
		"no person":       {Household: f.ann.Household},
		"household no id": {Person: "ann@example.com", Household: household.Household{Name: "Smiths"}},
		"wrong household": {Person: "ann@example.com", Household: f.jo.Household},
	}
	for op, call := range ops {
		for who, c := range callers {
			if err := call(c); !errors.Is(err, apicatalog.ErrNotMember) {
				t.Errorf("%s as %s: err = %v, want ErrNotMember", op, who, err)
			}
		}
	}
	// A refused write wrote nothing.
	if _, ok, err := f.store.MatchIngredient(ctx, "zz intruder"); err != nil || ok {
		t.Errorf("a refused CreateIngredient must not write: found = %v, err = %v", ok, err)
	}
	got, err := f.store.GetIngredient(ctx, id)
	if err != nil || got.Name != f.pepper.Name {
		t.Errorf("a refused UpdateIngredient must not write: %+v, err = %v", got, err)
	}
}

// A household credential has no person; the adapter verified it, and it names
// the household (ADR-00031). It is admitted to that household and no other.
func TestAHouseholdKeyIsAdmittedToItsHousehold(t *testing.T) {
	f := newFixture(t)
	key := apicatalog.Caller{Household: f.smiths, HouseholdKey: true}
	got, err := f.svc.GetIngredient(context.Background(), key, f.pepper.ID)
	if err != nil || got.Name != f.pepper.Name {
		t.Fatalf("GetIngredient with a household key = %+v, err = %v", got, err)
	}
	if _, err := f.svc.GetIngredient(context.Background(), apicatalog.Caller{HouseholdKey: true}, f.pepper.ID); !errors.Is(err, apicatalog.ErrNotMember) {
		t.Errorf("a household key naming no household: err = %v, want ErrNotMember", err)
	}
}

// A member of another household cannot see the overlay entry: it is not found,
// and it does not show in the list or in matching.
func TestAnotherHouseholdSeesNoOverlayEntry(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.GetIngredient(ctx, f.jo, f.pepper.ID); !errors.Is(err, apicatalog.ErrNotFound) {
		t.Errorf("GetIngredient as jo: err = %v, want ErrNotFound", err)
	}
	if err := f.svc.UpdateIngredient(ctx, f.jo, apicatalog.Ingredient{ID: f.pepper.ID, Name: "zz stolen", Aisle: "x"}); !errors.Is(err, apicatalog.ErrNotFound) {
		t.Errorf("UpdateIngredient as jo: err = %v, want ErrNotFound", err)
	}
	if _, ok, err := f.svc.MatchIngredient(ctx, f.jo, "zz smiths pepper"); err != nil || ok {
		t.Errorf("MatchIngredient as jo: found = %v, err = %v, want no match", ok, err)
	}
	all, err := f.svc.ListIngredients(ctx, f.jo)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range all {
		if i.ID == f.pepper.ID {
			t.Errorf("jo lists the Smiths' %q", i.Name)
		}
	}
}

func TestIngredientsRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	made, err := f.svc.CreateIngredient(ctx, f.ann, apicatalog.Ingredient{Name: "zz smiths sumac", Aisle: "spices", Aliases: []string{"zz sumak"}})
	if err != nil {
		t.Fatal(err)
	}
	if m, ok, err := f.svc.MatchIngredient(ctx, f.ann, "zz sumak"); err != nil || !ok || m.ID != made.ID {
		t.Fatalf("MatchIngredient = %+v, %v, %v, want the new entry", m, ok, err)
	}
	made.Reviewed = true
	if err := f.svc.UpdateIngredient(ctx, f.ann, made); err != nil {
		t.Fatal(err)
	}
	unreviewed, err := f.svc.ListUnreviewedIngredients(ctx, f.ann)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range unreviewed {
		if i.ID == made.ID {
			t.Errorf("%q is still unreviewed", i.Name)
		}
	}
	if err := f.svc.SuggestIngredient(ctx, f.ann, made.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := f.svc.GetIngredient(ctx, f.ann, made.ID); err != nil || !got.Suggested {
		t.Fatalf("after Suggest: %+v, err = %v", got, err)
	}
	if err := f.svc.UnsuggestIngredient(ctx, f.ann, made.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateIngredient(ctx, f.ann, apicatalog.Ingredient{Name: "zz smiths sumac", Aisle: "spices"}); !errors.Is(err, apicatalog.ErrConflict) {
		t.Errorf("duplicate name: err = %v, want ErrConflict", err)
	}
	if _, err := f.svc.CreateIngredient(ctx, f.ann, apicatalog.Ingredient{Name: "zz no aisle"}); !errors.Is(err, apicatalog.ErrInvalid) {
		t.Errorf("no aisle: err = %v, want ErrInvalid", err)
	}
	if _, err := f.svc.GetIngredient(ctx, f.ann, 1<<40); !errors.Is(err, apicatalog.ErrNotFound) {
		t.Errorf("unknown id: err = %v, want ErrNotFound", err)
	}
}

func TestMergeAndUsage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dup, err := f.svc.CreateIngredient(ctx, f.ann, apicatalog.Ingredient{Name: "zz smiths peppercorn", Aisle: "spices"})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := f.svc.IngredientUsage(ctx, f.ann, dup.ID); err != nil || n != 0 {
		t.Errorf("IngredientUsage = %d, %v, want 0", n, err)
	}
	if _, err := f.svc.MergeIngredients(ctx, f.ann, f.pepper.ID, dup.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.GetIngredient(ctx, f.ann, dup.ID); !errors.Is(err, apicatalog.ErrNotFound) {
		t.Errorf("merged-away ingredient: err = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.IngredientUsage(ctx, f.jo, f.pepper.ID); !errors.Is(err, apicatalog.ErrNotFound) {
		t.Errorf("IngredientUsage as jo: err = %v, want ErrNotFound", err)
	}
}

func TestIngredientUsageNeedsACounter(t *testing.T) {
	f := newFixture(t)
	svc, err := apicatalog.New(apicatalog.Deps{Catalog: f.store, Members: f.members})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IngredientUsage(context.Background(), f.ann, f.pepper.ID); err == nil {
		t.Fatal("IngredientUsage with no counter succeeded")
	}
}

func TestEquipmentAndUnits(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	eq, err := f.svc.CreateEquipment(ctx, f.ann, apicatalog.Equipment{Name: "zz smiths skillet"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.svc.GetEquipment(ctx, f.jo, eq.ID); err != nil || got.Name != eq.Name {
		t.Errorf("GetEquipment = %+v, %v (equipment is one catalog for every household)", got, err)
	}
	if m, ok, err := f.svc.MatchEquipment(ctx, f.ann, "zz smiths skillet"); err != nil || !ok || m.ID != eq.ID {
		t.Errorf("MatchEquipment = %+v, %v, %v", m, ok, err)
	}
	if _, err := f.svc.GetEquipment(ctx, f.ann, 1<<40); !errors.Is(err, apicatalog.ErrNotFound) {
		t.Errorf("unknown equipment: err = %v, want ErrNotFound", err)
	}
	all, err := f.svc.ListEquipment(ctx, f.ann)
	if err != nil || len(all) == 0 {
		t.Errorf("ListEquipment = %d entries, %v", len(all), err)
	}
	units, err := f.svc.ListUnits(ctx, f.ann)
	if err != nil || len(units) == 0 {
		t.Fatalf("ListUnits = %d entries, %v", len(units), err)
	}
	if u, ok, err := f.svc.MatchUnit(ctx, f.ann, "cup"); err != nil || !ok || u.Unit.Name == "" {
		t.Errorf("MatchUnit(cup) = %+v, %v, %v", u, ok, err)
	}
}

// Bound sends every call through the Service as the Caller its context names,
// and a context that names nobody is refused.
func TestBoundSpeaksForTheCallerOfTheContext(t *testing.T) {
	f := newFixture(t)
	type key struct{}
	who := func(ctx context.Context) apicatalog.Caller {
		c, _ := ctx.Value(key{}).(apicatalog.Caller)
		return c
	}
	b := f.svc.Bind(who)
	as := func(c apicatalog.Caller) context.Context { return context.WithValue(context.Background(), key{}, c) }

	if got, err := b.GetIngredient(as(f.ann), f.pepper.ID); err != nil || got.Name != f.pepper.Name {
		t.Fatalf("Bound as ann = %+v, %v", got, err)
	}
	if _, err := b.GetIngredient(as(f.jo), f.pepper.ID); !errors.Is(err, apicatalog.ErrNotFound) {
		t.Errorf("Bound as jo: err = %v, want ErrNotFound", err)
	}
	// Nobody on the context: the zero Caller, refused.
	if _, err := b.GetIngredient(context.Background(), f.pepper.ID); !errors.Is(err, apicatalog.ErrNotMember) {
		t.Errorf("Bound with nobody: err = %v, want ErrNotMember", err)
	}
	// The context's household is not the Caller's: the Caller decides.
	ctx := household.WithContext(context.Background(), f.smiths)
	if _, err := b.ListIngredients(ctx); !errors.Is(err, apicatalog.ErrNotMember) {
		t.Errorf("Bound with a household on the context but no caller: err = %v, want ErrNotMember", err)
	}
}

// operatorService is the service with "root@example.com" configured as the
// operator. That address is a member of no household.
func operatorService(t *testing.T, f *fixture) (*apicatalog.Service, apicatalog.Caller) {
	t.Helper()
	svc, err := apicatalog.New(apicatalog.Deps{Catalog: f.store, Members: f.members, Operators: config.Operator{Emails: []string{"root@example.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	return svc, apicatalog.Caller{Person: "root@example.com"}
}

// The operator reaches across households and need not be in one: the check is
// the configured operators, not membership (ADR-00043).
func TestAnOperatorWhoIsNoMemberIsAdmitted(t *testing.T) {
	f := newFixture(t)
	svc, root := operatorService(t, f)
	ctx := context.Background()
	if err := f.store.SuggestIngredient(household.WithContext(ctx, f.smiths), f.pepper.ID); err != nil {
		t.Fatal(err)
	}
	listed, err := svc.OperatorListSuggestedIngredients(ctx, root)
	if err != nil || len(listed) != 1 || listed[0].ID != f.pepper.ID {
		t.Fatalf("OperatorListSuggestedIngredients = %+v, %v, want the Smiths' suggestion", listed, err)
	}
	if p, err := svc.OperatorPreviewPromotion(ctx, root, f.pepper.ID); err != nil || p.Ingredient.ID != f.pepper.ID {
		t.Fatalf("OperatorPreviewPromotion = %+v, %v", p, err)
	}
	if _, err := svc.OperatorPromoteIngredient(ctx, root, f.pepper.ID); err != nil {
		t.Fatalf("OperatorPromoteIngredient: %v", err)
	}
	eq, err := f.store.CreateEquipment(ctx, apicatalog.Equipment{Name: "zz operator tin"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.OperatorGetEquipment(ctx, root, eq.ID)
	if err != nil {
		t.Fatal(err)
	}
	got.Sized = true
	if err := svc.OperatorUpdateEquipment(ctx, root, got); err != nil {
		t.Fatal(err)
	}
	if all, err := svc.OperatorListEquipment(ctx, root); err != nil || len(all) == 0 {
		t.Fatalf("OperatorListEquipment = %d, %v", len(all), err)
	}
	if again, _ := f.store.GetEquipment(ctx, eq.ID); !again.Sized {
		t.Error("OperatorUpdateEquipment did not save")
	}
	// A missing entry is not found, not a refusal.
	if _, err := svc.OperatorPreviewPromotion(ctx, root, 1<<40); !errors.Is(err, apicatalog.ErrNotFound) {
		t.Errorf("preview of nothing: err = %v, want ErrNotFound", err)
	}
}

// A household member who is not an operator is refused, whatever household the
// context names, and nothing is written.
func TestANonOperatorMemberIsRefusedTheOperatorOperations(t *testing.T) {
	f := newFixture(t)
	svc, _ := operatorService(t, f)
	ctx := household.WithContext(context.Background(), f.smiths)
	if err := f.store.SuggestIngredient(ctx, f.pepper.ID); err != nil {
		t.Fatal(err)
	}
	eq, err := f.store.CreateEquipment(ctx, apicatalog.Equipment{Name: "zz member tin"})
	if err != nil {
		t.Fatal(err)
	}
	id := f.pepper.ID
	ops := map[string]func(c apicatalog.Caller) error{
		"OperatorListEquipment":   func(c apicatalog.Caller) error { _, err := svc.OperatorListEquipment(ctx, c); return err },
		"OperatorGetEquipment":    func(c apicatalog.Caller) error { _, err := svc.OperatorGetEquipment(ctx, c, eq.ID); return err },
		"OperatorUpdateEquipment": func(c apicatalog.Caller) error { eq.Sized = true; return svc.OperatorUpdateEquipment(ctx, c, eq) },
		"OperatorListSuggestedIngredients": func(c apicatalog.Caller) error {
			_, err := svc.OperatorListSuggestedIngredients(ctx, c)
			return err
		},
		"OperatorPreviewPromotion": func(c apicatalog.Caller) error { _, err := svc.OperatorPreviewPromotion(ctx, c, id); return err },
		"OperatorPromoteIngredient": func(c apicatalog.Caller) error {
			_, err := svc.OperatorPromoteIngredient(ctx, c, id)
			return err
		},
	}
	callers := map[string]apicatalog.Caller{
		"member":        f.ann,
		"household key": {Household: f.smiths, HouseholdKey: true},
		"no person":     {},
	}
	for op, call := range ops {
		for who, c := range callers {
			if err := call(c); !errors.Is(err, apicatalog.ErrNotMember) {
				t.Errorf("%s as %s: err = %v, want ErrNotMember", op, who, err)
			}
		}
	}
	if got, err := f.store.GetIngredient(ctx, id); err != nil || got.Central {
		t.Errorf("a refused promotion promoted the entry: %+v, %v", got, err)
	}
	if got, _ := f.store.GetEquipment(ctx, eq.ID); got.Sized {
		t.Error("a refused update changed the equipment")
	}
	// With no operators configured nobody is one.
	if _, err := f.svc.OperatorListEquipment(ctx, apicatalog.Caller{Person: "root@example.com"}); !errors.Is(err, apicatalog.ErrNotMember) {
		t.Errorf("service with no operators: err = %v, want ErrNotMember", err)
	}
}

// countingMembers counts the membership lookups the service makes.
type countingMembers struct {
	apicatalog.Members
	n atomic.Int32
}

func (c *countingMembers) ForEmail(ctx context.Context, email string) (household.Household, error) {
	c.n.Add(1)
	return c.Members.ForEmail(ctx, email)
}

// A request that reads many ingredients (the line builder reads one per line)
// is admitted once. A context without WithAdmission checks every call, and the
// remembered admission serves only the Caller it was made for.
func TestAdmissionIsCheckedOncePerRequest(t *testing.T) {
	f := newFixture(t)
	counter := &countingMembers{Members: f.members}
	svc, err := apicatalog.New(apicatalog.Deps{Catalog: f.store, Members: counter})
	if err != nil {
		t.Fatal(err)
	}
	read := func(ctx context.Context, c apicatalog.Caller, n int) error {
		for range n {
			if _, err := svc.GetIngredient(ctx, c, f.pepper.ID); err != nil {
				return err
			}
		}
		return nil
	}

	counter.n.Store(0)
	if err := read(apicatalog.WithAdmission(context.Background()), f.ann, 5); err != nil {
		t.Fatal(err)
	}
	if got := counter.n.Load(); got != 1 {
		t.Errorf("5 reads in one request made %d membership lookups, want 1", got)
	}

	counter.n.Store(0)
	if err := read(context.Background(), f.ann, 5); err != nil {
		t.Fatal(err)
	}
	if got := counter.n.Load(); got != 5 {
		t.Errorf("5 reads with no admission memo made %d lookups, want 5 (fail closed)", got)
	}

	// Another Caller on the same request is not served from ann's admission.
	ctx := apicatalog.WithAdmission(context.Background())
	if err := read(ctx, f.ann, 1); err != nil {
		t.Fatal(err)
	}
	if err := read(ctx, f.jo, 1); !errors.Is(err, apicatalog.ErrNotFound) {
		t.Errorf("jo after ann on one request: err = %v, want ErrNotFound (jo's own household)", err)
	}
	// A refused Caller is never remembered, and does not unseat the admitted one.
	stranger := apicatalog.Caller{Person: "ann@example.com", Household: f.jo.Household}
	for range 2 {
		if err := read(ctx, stranger, 1); !errors.Is(err, apicatalog.ErrNotMember) {
			t.Errorf("a person claiming another household: err = %v, want ErrNotMember", err)
		}
	}
	if err := read(ctx, f.ann, 1); err != nil {
		t.Errorf("ann after a refusal: %v", err)
	}
}
