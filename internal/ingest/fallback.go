package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/parse"
	"github.com/kaecyra/gobbler/internal/store"
)

var (
	// ErrNoProvider is returned by RetryWithLLM when no LLM provider is
	// configured.
	ErrNoProvider = errors.New("no LLM provider is configured")
	// ErrLLMFailed marks a failure of the LLM step itself: the provider
	// errored or timed out, or its answer does not match the schema. Any other
	// error from Fallback.Retry is the pipeline breaking (the catalog could not
	// be read) and is not an LLM failure.
	ErrLLMFailed = errors.New("LLM provider failed")
	// ErrUnknownProvider is returned by SelectProvider for a configured
	// provider name it has no factory for.
	ErrUnknownProvider = errors.New("unknown LLM provider")
)

// llmFieldConfidence is the confidence given to a field the LLM supplied. The
// rules did not read it, so there is no rule confidence to report; the draft
// marks it FromLLM instead, and the review screen shows that marker rather than
// a score (ADR-00006 obligation 4).
const llmFieldConfidence = 1.0

// maxLLMSourceBytes bounds the page markup sent to a provider when a page has
// no readable main text and the HTML itself is the only source.
const maxLLMSourceBytes = 256 << 10

// ProviderFactory builds a provider from configuration.
type ProviderFactory func(ctx context.Context, cfg config.LLM, log *slog.Logger) (Provider, error)

// SelectProvider builds the provider cfg names from factories, keyed by
// provider name. Which provider and which models is configuration alone
// (ADR-00007 obligation 3); this package knows no SDK, so the caller passes
// the factories. It returns a nil Provider and no error when no provider is
// configured, and an error wrapping ErrUnknownProvider for a name with no
// factory.
func SelectProvider(ctx context.Context, cfg config.LLM, log *slog.Logger, factories map[string]ProviderFactory) (Provider, error) {
	if !cfg.Configured() {
		return nil, nil
	}
	f, ok := factories[cfg.Provider]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownProvider, cfg.Provider)
	}
	p, err := f(ctx, cfg, log)
	if err != nil {
		return nil, fmt.Errorf("build LLM provider %q: %w", cfg.Provider, err)
	}
	return p, nil
}

// autoFallback asks the provider to improve a draft that scored below the
// threshold. The rule-based draft is the safe state: when the provider fails,
// d comes back unchanged except for LLMError, which says why, and the failure
// is never folded into the score (error-handling: an error is not a
// low-confidence draft).
//
// A pipeline failure (the catalog could not be read) is returned as an error,
// as it is on the rule-based path; only ErrLLMFailed keeps the rule draft.
func (f *Fallback) autoFallback(ctx context.Context, d Draft) (Draft, error) {
	if !d.BelowThreshold || f.deps.Provider == nil {
		return d, nil
	}
	out, err := f.Retry(ctx, d)
	if err != nil && !errors.Is(err, ErrLLMFailed) {
		return Draft{}, err
	}
	if err != nil {
		f.log().WarnContext(ctx, "llm fallback failed, keeping the rule-based draft", "error", err.Error())
		d.LLMError = err.Error()
		return d, nil
	}
	return out, nil
}

func (f *Fallback) log() *slog.Logger {
	if f.deps.Logger != nil {
		return f.deps.Logger
	}
	return slog.Default()
}

// Fallback is the LLM step of the pipeline. It is a type of its own because
// it is the one step that takes a Draft; Pipeline never does, so the guarantee
// that no Pipeline method can store a draft stays checkable by its signature.
// It holds no per-request state and is safe for concurrent use.
type Fallback struct {
	cfg  config.Ingestion
	deps Deps
}

// Fallback returns the pipeline's LLM step, over the same configuration,
// catalog and provider. With no provider configured its Retry returns
// ErrNoProvider.
func (p *Pipeline) Fallback() *Fallback { return &Fallback{cfg: p.cfg, deps: p.deps} }

// Configured reports whether a provider is available.
func (f *Fallback) Configured() bool { return f.deps.Provider != nil }

// Retry asks the provider to structure d's source, whatever d scored
// (the user asked). On success it returns a new draft built from the answer
// with the same Source and every LLM-supplied value marked FromLLM. On any
// failure (no provider, a timeout, an answer that does not match the schema)
// it returns an error and no draft; the caller keeps d. Provider and schema
// failures wrap ErrLLMFailed; a catalog read failure does not. Nothing is saved.
func (f *Fallback) Retry(ctx context.Context, d Draft) (Draft, error) {
	if f.deps.Provider == nil {
		return Draft{}, ErrNoProvider
	}
	text, err := llmSourceText(d.Source)
	if err != nil {
		return Draft{}, err
	}
	s, err := f.deps.Provider.Structure(ctx, Request{Text: text, RuleDraft: d})
	if err != nil {
		return Draft{}, fmt.Errorf("%w: %w", ErrLLMFailed, err)
	}
	return f.fromStructured(ctx, d, s)
}

// llmSourceText is the text a provider is asked to read: pasted text as given,
// a page's main text, or the markup itself for a page whose recipe is only in
// structured data.
func llmSourceText(src Source) (string, error) {
	if src.Kind != store.SnapshotHTML {
		return string(src.Content), nil
	}
	res, err := extract.Extract(src.Content, src.URL)
	if err == nil && res.Text != "" {
		return res.Text, nil
	}
	if len(src.Content) > maxLLMSourceBytes {
		return string(src.Content[:maxLLMSourceBytes]), nil
	}
	return string(src.Content), nil
}

// fromStructured reads a provider's answer through the same line parser,
// catalog resolution and scoring as any other input. Fields the answer leaves
// empty keep the rule-based value, unmarked.
func (f *Fallback) fromStructured(ctx context.Context, rule Draft, s Structured) (Draft, error) {
	if strings.TrimSpace(s.Name) == "" || len(s.Components) == 0 {
		return Draft{}, fmt.Errorf("%w: answer %w", ErrLLMFailed, ErrInvalidStructured)
	}
	d := Draft{
		ParserVersion: ParserVersion,
		Source:        rule.Source,
		Method:        rule.Method,
		Threshold:     f.cfg.ConfidenceThreshold,
		UsedLLM:       true,
	}
	a := &assembler{rs: newResolver(ctx, f.deps.Catalog), d: &d}

	d.Name = fromLLM(stated(s.Name, llmFieldConfidence))
	d.Author = orRule(fromLLM(stated(s.Author, llmFieldConfidence)), rule.Author)
	d.Description = orRule(fromLLM(stated(s.Description, llmFieldConfidence)), rule.Description)
	a.yield([]string{s.Yield})
	if d.Yield.Value.Unit == "" {
		d.Yield, d.Servings = rule.Yield, rule.Servings
	} else {
		d.Yield.FromLLM = true
		d.Servings.FromLLM = !d.Servings.Value.IsAbsent()
	}
	d.Calories = rule.Calories
	d.Prep = orRule(llmDuration(s.PrepTime), rule.Prep)
	d.Cook = orRule(llmDuration(s.CookTime), rule.Cook)
	d.Total = orRule(llmDuration(s.TotalTime), rule.Total)

	for _, sc := range s.Components {
		c := Component{Name: strings.TrimSpace(sc.Name), FromLLM: true}
		for _, raw := range sc.Ingredients {
			if l, ok := a.line(raw); ok {
				l.FromLLM = true
				c.Lines = append(c.Lines, l)
			}
		}
		for _, text := range sc.Steps {
			if text = strings.TrimSpace(text); text != "" {
				st := step(text, llmFieldConfidence)
				st.FromLLM = true
				c.Steps = append(c.Steps, st)
			}
		}
		d.Components = append(d.Components, c)
	}
	if a.rs.err != nil {
		return Draft{}, fmt.Errorf("read catalog while drafting: %w", a.rs.err)
	}

	d.Proposed = proposedIngredients(d.Components)
	d.ProposedUnits = a.proposedUnits
	d.score(a.requiredStructure())
	return d, nil
}

func fromLLM[T any](f Field[T]) Field[T] {
	f.FromLLM = f.Confidence > 0
	return f
}

// orRule keeps the rule-based value for a field the LLM left empty.
func orRule[T any](llm, rule Field[T]) Field[T] {
	if llm.Confidence == 0 {
		return rule
	}
	return llm
}

func llmDuration(s string) Field[time.Duration] {
	r := parse.Duration(s)
	if !r.Found {
		return Field[time.Duration]{}
	}
	return fromLLM(duration(r.Max))
}
