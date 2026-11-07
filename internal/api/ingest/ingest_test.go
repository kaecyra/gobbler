package ingest_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/api/ingest"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	core "github.com/kaecyra/gobbler/internal/ingest"
)

var (
	smiths  = household.Household{ID: 1, Name: "Smiths"}
	joneses = household.Household{ID: 2, Name: "Joneses"}
)

// members is the membership lookup: ann is a Smith, jo a Jones.
type members map[string]household.Household

func (m members) ForEmail(_ context.Context, email string) (household.Household, error) {
	if h, ok := m[email]; ok {
		return h, nil
	}
	return household.Household{}, household.ErrNotFound
}

// countingCatalog counts the pipeline's catalog reads and records the household
// each ran in, so a test can tell whether the pipeline was reached and as whom.
type countingCatalog struct {
	reads atomic.Int32
	seen  atomic.Int64
}

func (c *countingCatalog) note(ctx context.Context) {
	c.reads.Add(1)
	if h, ok := household.FromContext(ctx); ok {
		c.seen.Store(h.ID)
	}
}

func (c *countingCatalog) MatchIngredient(ctx context.Context, _ string) (catalog.Ingredient, bool, error) {
	c.note(ctx)
	return catalog.Ingredient{}, false, nil
}
func (c *countingCatalog) MatchUnit(ctx context.Context, _ string) (catalog.Unit, bool, error) {
	c.note(ctx)
	return catalog.Unit{}, false, nil
}
func (c *countingCatalog) MatchEquipment(ctx context.Context, _ string) (catalog.Equipment, bool, error) {
	c.note(ctx)
	return catalog.Equipment{}, false, nil
}
func (c *countingCatalog) ListEquipment(ctx context.Context) ([]catalog.Equipment, error) {
	c.note(ctx)
	return nil, nil
}

// countingProvider counts how often the LLM is asked.
type countingProvider struct{ asked atomic.Int32 }

func (p *countingProvider) Structure(context.Context, core.Request) (core.Structured, error) {
	p.asked.Add(1)
	return core.Structured{}, errors.New("provider stub")
}

type fixture struct {
	svc      *ingest.Service
	cat      *countingCatalog
	provider *countingProvider
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	cat, prov := &countingCatalog{}, &countingProvider{}
	svc, err := ingest.New(
		config.Ingestion{ConfidenceThreshold: 0.7, FetchTimeout: time.Second, UserAgent: "t"},
		ingest.Deps{Catalog: cat, Provider: prov, Members: members{"ann@example.com": smiths, "jo@example.com": joneses}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{svc: svc, cat: cat, provider: prov}
}

const recipeText = "Toast\n\nIngredients\n- 2 slices bread\n\nInstructions\n1. Toast the bread.\n"

// Every operation refuses a Caller that is not a verified member before the
// pipeline runs: nothing is fetched, read from the catalog or sent to the LLM
// (ADR-00018, ADR-00043).
func TestEveryOperationRefusesACallerThatIsNotAMember(t *testing.T) {
	ctx := context.Background()
	callers := map[string]ingest.Caller{
		"no household":              {Person: "ann@example.com"},
		"no person":                 {Household: smiths},
		"nobody":                    {},
		"person of another house":   {Person: "jo@example.com", Household: smiths},
		"claims a house not theirs": {Person: "ann@example.com", Household: joneses},
		"stranger":                  {Person: "nobody@example.com", Household: smiths},
	}
	for name, c := range callers {
		ops := map[string]func(f fixture) error{
			"FromURL":  func(f fixture) error { _, err := f.svc.FromURL(ctx, c, "https://example.test/x"); return err },
			"FromText": func(f fixture) error { _, err := f.svc.FromText(ctx, c, recipeText); return err },
			"FromOCR": func(f fixture) error {
				_, err := f.svc.FromOCR(ctx, c, recipeText, []byte("img"), "image/png")
				return err
			},
			"Retry": func(f fixture) error { _, err := f.svc.Retry(ctx, c, ingest.Draft{}); return err },
		}
		for op, run := range ops {
			t.Run(name+"/"+op, func(t *testing.T) {
				f := newFixture(t)
				if err := run(f); !errors.Is(err, ingest.ErrNotMember) {
					t.Fatalf("err = %v, want ErrNotMember", err)
				}
				if n := f.cat.reads.Load(); n != 0 {
					t.Errorf("the catalog was read %d times for a refused caller", n)
				}
				if n := f.provider.asked.Load(); n != 0 {
					t.Errorf("the LLM was asked %d times for a refused caller", n)
				}
			})
		}
	}
}

// A personal key or a session (a person), and a household key (no person), are
// admitted, and the pipeline runs in the household the membership check found.
func TestAdmittedCallersRunInTheirHousehold(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]ingest.Caller{
		"person":        {Person: "ann@example.com", Household: smiths},
		"household key": {Household: smiths, HouseholdKey: true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			d, err := f.svc.FromText(ctx, c, recipeText)
			if err != nil {
				t.Fatal(err)
			}
			if d.Name.Value == "" {
				t.Error("no draft was made")
			}
			if got := f.cat.seen.Load(); got != smiths.ID {
				t.Errorf("the pipeline read the catalog in household %d, want %d", got, smiths.ID)
			}
		})
	}
}

// The rule-based path runs first: a clean draft never reaches the provider, and
// Retry is the only way to ask for one (ADR-00006).
func TestTheProviderIsNotAskedForACleanDraft(t *testing.T) {
	f := newFixture(t)
	c := ingest.Caller{Person: "ann@example.com", Household: smiths}
	d, err := f.svc.FromText(context.Background(), c, recipeText)
	if err != nil {
		t.Fatal(err)
	}
	if d.BelowThreshold {
		t.Skip("the sample draft scored below the threshold")
	}
	if n := f.provider.asked.Load(); n != 0 {
		t.Errorf("the LLM was asked %d times before anyone pressed retry", n)
	}
	if _, err := f.svc.Retry(context.Background(), c, d); !errors.Is(err, ingest.ErrLLMFailed) {
		t.Errorf("Retry err = %v, want the provider's failure (ErrLLMFailed)", err)
	}
	if n := f.provider.asked.Load(); n != 1 {
		t.Errorf("the LLM was asked %d times after retry, want 1", n)
	}
}

// Bound names the Caller from each call's context, and a context with nobody
// on it is refused.
func TestBoundNamesTheCallerFromTheContext(t *testing.T) {
	f := newFixture(t)
	ann := ingest.Caller{Person: "ann@example.com", Household: smiths}
	b := f.svc.Bind(func(ctx context.Context) ingest.Caller {
		if v, ok := ctx.Value(whoKey{}).(ingest.Caller); ok {
			return v
		}
		return ingest.Caller{}
	})
	if _, err := b.FromText(context.WithValue(context.Background(), whoKey{}, ann), recipeText); err != nil {
		t.Errorf("a context with a member: %v", err)
	}
	if _, err := b.FromText(context.Background(), recipeText); !errors.Is(err, ingest.ErrNotMember) {
		t.Errorf("a context with nobody: err = %v, want ErrNotMember", err)
	}
	if _, err := b.Retry(context.Background(), ingest.Draft{}); !errors.Is(err, ingest.ErrNotMember) {
		t.Errorf("Retry with nobody: err = %v, want ErrNotMember", err)
	}
}

type whoKey struct{}

func TestNewRequiresMembersAndACatalog(t *testing.T) {
	cfg := config.Ingestion{ConfidenceThreshold: 0.7, FetchTimeout: time.Second, UserAgent: "t"}
	if _, err := ingest.New(cfg, ingest.Deps{Catalog: &countingCatalog{}}); err == nil {
		t.Error("New accepted no members")
	}
	if _, err := ingest.New(cfg, ingest.Deps{Members: members{}}); err == nil {
		t.Error("New accepted no catalog")
	}
}
