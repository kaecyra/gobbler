package ingest

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
)

// fakeProvider records every request and answers with a fixed result.
type fakeProvider struct {
	mu    sync.Mutex
	calls []Request
	resp  Structured
	err   error
	// onCall runs inside Structure, to break the world mid-call.
	onCall func()
}

func (f *fakeProvider) Structure(_ context.Context, req Request) (Structured, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if f.onCall != nil {
		f.onCall()
	}
	return f.resp, f.err
}

func (f *fakeProvider) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.calls) }

// messyText scores below the test threshold: the title is a guess and no line
// is readable.
const messyText = "stew\n\n?? a good amount of glorp #@!\n\nstir it\n"

// headingless is readable but its sections are guessed, so it scores below
// strictConfig's threshold while still being a saveable recipe.
const headingless = "Toast\n\n2 slices bread\n1 tbsp butter\n\nToast the bread and butter it.\n"

func strictConfig() config.Ingestion {
	cfg := testConfig()
	cfg.ConfidenceThreshold = 0.99
	return cfg
}

func llmAnswer() Structured {
	return Structured{
		Name:     "Glorp Stew",
		Author:   "Ada",
		Yield:    "4 servings",
		PrepTime: "PT10M",
		Components: []StructuredComponent{{
			Ingredients: []string{"2 cups all-purpose flour", "1 tsp dragonfruit zest"},
			Steps:       []string{"Stir it all together.", "Simmer for 20 minutes."},
		}},
	}
}

func (e *env) pipelineWith(t *testing.T, cfg config.Ingestion, prov Provider) *Pipeline {
	t.Helper()
	p, err := New(cfg, Deps{Catalog: e.catalog, Provider: prov})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func TestBelowThresholdInputCallsProviderOnceAndDraftReflectsAnswer(t *testing.T) {
	e := newEnv(t)
	fp := &fakeProvider{resp: llmAnswer()}
	p := e.pipelineWith(t, testConfig(), fp)

	d, err := p.FromText(e.ctx, messyText)
	if err != nil {
		t.Fatal(err)
	}
	if fp.count() != 1 {
		t.Fatalf("provider calls = %d, want 1", fp.count())
	}
	req := fp.calls[0]
	if req.Text != messyText {
		t.Errorf("request text = %q, want the source text", req.Text)
	}
	if req.Gaps.Empty() || !reflect.DeepEqual(req.RuleDraft, Draft{}) {
		t.Errorf("request must carry the gaps and not the whole rule-based draft: %+v", req)
	}
	if !d.UsedLLM || d.Name.Value != "stew" || d.Name.FromLLM {
		t.Errorf("name = %+v, used LLM = %v; want the rule name kept, it met the threshold", d.Name, d.UsedLLM)
	}
	if !d.Author.FromLLM || !d.Prep.FromLLM || d.Prep.Value == 0 || !d.Yield.FromLLM {
		t.Errorf("author/prep/yield not marked or read: %+v %+v %+v", d.Author, d.Prep, d.Yield)
	}
	// The rules read no ingredient line, so the LLM supplies the components.
	if len(d.Components) != 1 || len(d.Components[0].Lines) != 2 || len(d.Components[0].Steps) != 2 {
		t.Fatalf("components = %+v", d.Components)
	}
	for _, l := range d.Components[0].Lines {
		if !l.FromLLM {
			t.Errorf("line %q not marked FromLLM", l.Raw)
		}
	}
	for _, s := range d.Components[0].Steps {
		if !s.FromLLM {
			t.Errorf("step %q not marked FromLLM", s.Text)
		}
	}
	if d.Components[0].Steps[1].Duration == 0 {
		t.Error("an LLM step must go through the same duration reading as any other")
	}
	if d.Source.SHA256 == "" || string(d.Source.Content) != messyText {
		t.Error("the source snapshot must be the ingested input, unchanged")
	}
}

func TestThresholdFromConfigurationDecidesTheCall(t *testing.T) {
	e := newEnv(t)
	good := string(readFixture(t, "garlic_spaghetti.txt"))

	cfg := testConfig()
	cfg.ConfidenceThreshold = 0
	fp := &fakeProvider{resp: llmAnswer()}
	if _, err := e.pipelineWith(t, cfg, fp).FromText(e.ctx, messyText); err != nil {
		t.Fatal(err)
	}
	if fp.count() != 0 {
		t.Errorf("threshold 0: provider calls = %d, want 0", fp.count())
	}

	cfg.ConfidenceThreshold = 1
	fp = &fakeProvider{resp: llmAnswer()}
	d, err := e.pipelineWith(t, cfg, fp).FromText(e.ctx, good)
	if err != nil {
		t.Fatal(err)
	}
	if d.Overall >= 1 && fp.count() != 0 {
		t.Errorf("draft scored %v with threshold 1: provider calls = %d", d.Overall, fp.count())
	}
	if d.Overall < 1 && fp.count() != 1 {
		t.Errorf("draft scored %v below threshold 1: provider calls = %d, want 1", d.Overall, fp.count())
	}
}

func TestHighScoringDraftAtDefaultThresholdMakesNoProviderCall(t *testing.T) { // ADR-00006
	e := newEnv(t)
	good := string(readFixture(t, "garlic_spaghetti.txt"))
	cfg := testConfig() // the default threshold, 0.7
	fp := &fakeProvider{resp: llmAnswer()}

	d, err := e.pipelineWith(t, cfg, fp).FromText(e.ctx, good)
	if err != nil {
		t.Fatal(err)
	}
	if d.Overall < cfg.ConfidenceThreshold || d.BelowThreshold {
		t.Fatalf("precondition: draft scored %v against threshold %v (below=%v), want at or above", d.Overall, cfg.ConfidenceThreshold, d.BelowThreshold)
	}
	if fp.count() != 0 {
		t.Errorf("provider calls = %d, want 0 for a draft above the threshold", fp.count())
	}
	if d.UsedLLM || d.LLMError != "" || d.Name.FromLLM {
		t.Errorf("draft must be purely rule-based: used LLM = %v, error = %q, name = %+v", d.UsedLLM, d.LLMError, d.Name)
	}
}

func TestProviderFailureKeepsRuleDraftAndRecordsTheError(t *testing.T) {
	e := newEnv(t)
	rule, err := e.pipelineWith(t, testConfig(), nil).FromText(e.ctx, messyText)
	if err != nil {
		t.Fatal(err)
	}
	for name, resp := range map[string]*fakeProvider{
		"timeout":        {err: context.DeadlineExceeded},
		"invalid answer": {err: ErrInvalidStructured},
		"empty answer":   {resp: Structured{}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := e.pipelineWith(t, testConfig(), resp).FromText(e.ctx, messyText)
			if err != nil {
				t.Fatalf("a provider failure must not fail the ingest: %v", err)
			}
			if got.LLMError == "" {
				t.Error("the failure must be recorded on the draft")
			}
			if got.UsedLLM || got.Name.FromLLM {
				t.Error("no partial LLM draft")
			}
			if got.Overall != rule.Overall || got.Name != rule.Name || len(got.Components) != len(rule.Components) {
				t.Errorf("rule-based draft changed: overall %v vs %v", got.Overall, rule.Overall)
			}
			if _, err := Confirm(got); err != nil && !errors.Is(err, ErrIncompleteDraft) {
				t.Errorf("Confirm: %v", err)
			}
		})
	}
}

func TestProviderFailureDraftStaysSaveable(t *testing.T) {
	e := newEnv(t)
	fp := &fakeProvider{err: context.DeadlineExceeded}
	d, err := e.pipelineWith(t, strictConfig(), fp).FromText(e.ctx, headingless)
	if err != nil {
		t.Fatal(err)
	}
	if fp.count() != 1 || d.LLMError == "" {
		t.Fatalf("provider calls = %d, LLMError = %q; want one call and an error", fp.count(), d.LLMError)
	}
	if _, err := Confirm(d); err != nil {
		t.Errorf("draft with an LLM error must stay confirmable: %v", err)
	}
}

func TestRetryCallsProviderRegardlessOfScore(t *testing.T) {
	e := newEnv(t)
	good := string(readFixture(t, "garlic_spaghetti.txt"))
	fp := &fakeProvider{resp: llmAnswer()}
	p := e.pipelineWith(t, testConfig(), fp)
	rule, err := p.FromText(e.ctx, good)
	if err != nil {
		t.Fatal(err)
	}
	if rule.BelowThreshold || fp.count() != 0 {
		t.Fatalf("precondition: high-confidence draft, no calls (below=%v calls=%d)", rule.BelowThreshold, fp.count())
	}
	got, err := p.Fallback().Retry(e.ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	if fp.count() != 1 || !got.UsedLLM {
		t.Errorf("calls = %d, used LLM = %v", fp.count(), got.UsedLLM)
	}
}

func TestRetryErrors(t *testing.T) {
	e := newEnv(t)
	rule, err := e.pipelineWith(t, testConfig(), nil).FromText(e.ctx, messyText)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.pipelineWith(t, testConfig(), nil).Fallback().Retry(e.ctx, rule); !errors.Is(err, ErrNoProvider) {
		t.Errorf("no provider: err = %v, want ErrNoProvider", err)
	}
	fp := &fakeProvider{err: context.DeadlineExceeded}
	got, err := e.pipelineWith(t, testConfig(), fp).Fallback().Retry(e.ctx, rule)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the provider's error wrapped", err)
	}
	if got.UsedLLM || got.Name.Value != "" {
		t.Errorf("an error must come with no draft: %+v", got)
	}
}

func TestLLMProposedIngredientIsProposedNotCreated(t *testing.T) { // ADR-00008 obligation 3
	e := newEnv(t)
	fp := &fakeProvider{resp: llmAnswer()}
	d, err := e.pipelineWith(t, testConfig(), fp).FromText(e.ctx, messyText)
	if err != nil {
		t.Fatal(err)
	}
	var proposed bool
	for _, p := range d.Proposed {
		if p.Name == "dragonfruit zest" {
			proposed = true
		}
	}
	if !proposed {
		t.Fatalf("proposed = %+v, want dragonfruit zest", d.Proposed)
	}
	if _, ok, err := e.catalog.MatchIngredient(e.ctx, "dragonfruit zest"); err != nil || ok {
		t.Errorf("catalog has the proposed ingredient before any confirmed save (ok=%v err=%v)", ok, err)
	}
}

func TestEmptyLLMFieldsKeepRuleValues(t *testing.T) {
	e := newEnv(t)
	text := headingless
	ans := Structured{Name: "Better Pancakes", Components: []StructuredComponent{{Ingredients: []string{"2 eggs"}, Steps: []string{"Cook."}}}}
	d, err := e.pipelineWith(t, strictConfig(), &fakeProvider{resp: ans}).FromText(e.ctx, text)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name.Value != "Better Pancakes" || d.Author.FromLLM || d.Description.FromLLM {
		t.Errorf("name %+v author %+v: unstated LLM fields must not be marked", d.Name, d.Author)
	}
}

func TestSelectProviderBuildsTheConfiguredChoice(t *testing.T) {
	claudeFake, geminiFake := &fakeProvider{}, &fakeProvider{}
	var gotCfg config.LLM
	factories := map[string]ProviderFactory{
		config.ProviderClaude: func(_ context.Context, cfg config.LLM, _ *slog.Logger) (Provider, error) {
			gotCfg = cfg
			return claudeFake, nil
		},
		config.ProviderGemini: func(_ context.Context, cfg config.LLM, _ *slog.Logger) (Provider, error) {
			gotCfg = cfg
			return geminiFake, nil
		},
		"broken": func(context.Context, config.LLM, *slog.Logger) (Provider, error) {
			return nil, errors.New("missing key")
		},
	}
	ctx := context.Background()
	for _, tc := range []struct {
		cfg  config.LLM
		want Provider
	}{
		{config.LLM{Provider: config.ProviderClaude, TextModel: "m1"}, claudeFake},
		{config.LLM{Provider: config.ProviderGemini, TextModel: "m2"}, geminiFake},
	} {
		got, err := SelectProvider(ctx, tc.cfg, nil, factories)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %v, %v", tc.cfg.Provider, got, err)
		}
		if gotCfg.TextModel != tc.cfg.TextModel {
			t.Errorf("%s: factory got model %q, want %q", tc.cfg.Provider, gotCfg.TextModel, tc.cfg.TextModel)
		}
	}
	if p, err := SelectProvider(ctx, config.LLM{}, nil, factories); p != nil || err != nil {
		t.Errorf("unconfigured: got %v, %v; want nil, nil", p, err)
	}
	if _, err := SelectProvider(ctx, config.LLM{Provider: "gpt"}, nil, factories); !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("unknown: err = %v", err)
	}
	if _, err := SelectProvider(ctx, config.LLM{Provider: "broken"}, nil, factories); err == nil {
		t.Error("a factory error must surface")
	}
}

// flakyCatalog reads through to the real catalog until broken.
type flakyCatalog struct {
	CatalogReader
	broken atomic.Bool
}

func (c *flakyCatalog) MatchIngredient(ctx context.Context, term string) (catalog.Ingredient, bool, error) {
	if c.broken.Load() {
		return catalog.Ingredient{}, false, errCatalogDown
	}
	return c.CatalogReader.MatchIngredient(ctx, term)
}

var errCatalogDown = errors.New("catalog down")

func flakyPipeline(t *testing.T, e *env) (*Pipeline, *flakyCatalog) {
	t.Helper()
	cat := &flakyCatalog{CatalogReader: e.catalog}
	fp := &fakeProvider{resp: llmAnswer(), onCall: func() { cat.broken.Store(true) }}
	p, err := New(testConfig(), Deps{Catalog: cat, Provider: fp})
	if err != nil {
		t.Fatal(err)
	}
	return p, cat
}

func TestCatalogFailureAfterTheLLMAnswersIsAPipelineErrorOnTheAutomaticPath(t *testing.T) {
	e := newEnv(t)
	p, _ := flakyPipeline(t, e)
	_, err := p.FromText(e.ctx, messyText)
	if !errors.Is(err, errCatalogDown) || errors.Is(err, ErrLLMFailed) {
		t.Fatalf("err = %v, want the catalog failure and not an LLM failure", err)
	}
}

func TestCatalogFailureAfterTheLLMAnswersIsAPipelineErrorOnRetry(t *testing.T) {
	e := newEnv(t)
	p, _ := flakyPipeline(t, e)
	rule, err := e.pipelineWith(t, testConfig(), nil).FromText(e.ctx, messyText)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Fallback().Retry(e.ctx, rule)
	if !errors.Is(err, errCatalogDown) || errors.Is(err, ErrLLMFailed) {
		t.Fatalf("err = %v, want the catalog failure and not an LLM failure", err)
	}
}

func TestProviderAndSchemaFailuresAreLLMFailures(t *testing.T) {
	e := newEnv(t)
	rule, err := e.pipelineWith(t, testConfig(), nil).FromText(e.ctx, messyText)
	if err != nil {
		t.Fatal(err)
	}
	for name, fp := range map[string]*fakeProvider{
		"provider error": {err: context.DeadlineExceeded},
		"empty answer":   {resp: Structured{}},
	} {
		if _, err := e.pipelineWith(t, testConfig(), fp).Fallback().Retry(e.ctx, rule); !errors.Is(err, ErrLLMFailed) {
			t.Errorf("%s: err = %v, want ErrLLMFailed", name, err)
		}
	}
}
