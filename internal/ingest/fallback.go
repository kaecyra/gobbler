package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
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

// Retry asks the provider to fill the gaps in d's rule-based reading, whatever
// d scored (the user asked): the lines and fields below the confidence
// threshold or missing (ADR-00020 decision 2). It returns a new draft in which
// exactly those are replaced by the answer and marked FromLLM; every other value
// is the rule-based one. With nothing to fill it returns d unchanged and makes
// no call. On any failure (no provider, a timeout, an answer that does not match
// the schema or the gaps) it returns an error and no draft; the caller keeps d.
// Provider and schema failures wrap ErrLLMFailed; a catalog read failure does
// not. Nothing is saved.
func (f *Fallback) Retry(ctx context.Context, d Draft) (Draft, error) {
	if f.deps.Provider == nil {
		return Draft{}, ErrNoProvider
	}
	gaps := findGaps(d, f.cfg.ConfidenceThreshold)
	if gaps.Empty() {
		f.log().InfoContext(ctx, "llm fallback skipped, the rule-based draft has no gaps")
		return d, nil
	}
	text, err := llmSourceText(d.Source)
	if err != nil {
		return Draft{}, err
	}
	image, imageMIME := f.visionImage(ctx, d.Source)
	s, err := f.deps.Provider.Structure(ctx, Request{Text: text, Image: image, ImageMIME: imageMIME, Gaps: gaps})
	if err != nil {
		return Draft{}, fmt.Errorf("%w: %w", ErrLLMFailed, err)
	}
	return f.fillGaps(ctx, d, gaps, s)
}

// findGaps lists what the rules left weak in d against threshold: a scalar
// field with no value or below the threshold, equipment when none was found,
// every ingredient line below the threshold, and the components when there is
// no ingredient line to replace, and the steps whose graph is the default. A
// value at or above the threshold is never a gap, so the steps of a draft with
// no lines that met it are kept.
func findGaps(d Draft, threshold float64) Gaps {
	weak := func(confidence float64) bool { return confidence == 0 || confidence < threshold }
	g := Gaps{
		Name:        weak(d.Name.Confidence),
		Author:      weak(d.Author.Confidence),
		Description: weak(d.Description.Confidence),
		// A bare yield is stated as servings alone, so servings count too.
		Yield:     weak(max(d.Yield.Confidence, d.Servings.Confidence)),
		Prep:      weak(d.Prep.Confidence),
		Cook:      weak(d.Cook.Confidence),
		Total:     weak(d.Total.Confidence),
		Equipment: len(d.Equipment) == 0,
	}
	var lines int
	for ci, c := range d.Components {
		for li, l := range c.Lines {
			lines++
			if l.Confidence < threshold {
				g.Lines = append(g.Lines, GapLine{Component: ci, Line: li, Raw: l.Raw})
			}
		}
	}
	g.Components = lines == 0
	if !g.Components {
		// With the components replaced the steps are renumbered, so the graph is
		// built by the rules over the result instead (see fillGaps).
		g.Steps, g.StepTexts = stepGaps(d.Components)
	}
	return g
}

// maxVisionImageBytes is the largest photo attached to a provider request.
// Anthropic allows 10 MB per image base64-encoded on the Claude API directly
// (5 MB on Bedrock and Google Cloud), https://platform.claude.com/docs/en/build-with-claude/vision;
// base64 inflates by 4/3, so 7.5 MB of raw bytes is the strictest limit that
// holds. Gemini's inline limit is 20 MB for the whole request, which is looser:
// https://ai.google.dev/gemini-api/docs/image-understanding. Checked against
// those pages when this was written.
const maxVisionImageBytes = 7_500_000

// visionImage returns the photo to attach to a provider request, or nothing
// when the source has none or it is over maxVisionImageBytes. An oversized
// photo is dropped rather than sent, and the provider reads the OCR text alone;
// that is logged because the draft is then less well informed than the user
// may expect.
func (f *Fallback) visionImage(ctx context.Context, src Source) ([]byte, string) {
	if len(src.Image) == 0 {
		return nil, ""
	}
	if len(src.Image) > maxVisionImageBytes {
		f.log().WarnContext(ctx, "photo not sent to the LLM provider, using the OCR text only",
			"reason", "image_over_provider_limit", "image_bytes", len(src.Image), "limit_bytes", maxVisionImageBytes)
		return nil, ""
	}
	return src.Image, src.ImageMIME
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

// fillGaps merges a provider's answer into the rule-based draft, replacing
// only what gaps names; the rest of the answer is ignored. Replacements go
// through the same line parser, catalog resolution and scoring as any other
// input. rule is not modified.
func (f *Fallback) fillGaps(ctx context.Context, rule Draft, gaps Gaps, s Structured) (Draft, error) {
	if strings.TrimSpace(s.Name) == "" || len(s.Components) == 0 {
		return Draft{}, fmt.Errorf("%w: answer %w", ErrLLMFailed, ErrInvalidStructured)
	}
	var answered []string
	for _, sc := range s.Components {
		answered = append(answered, sc.Ingredients...)
	}
	if len(gaps.Lines) > 0 && len(answered) != len(gaps.Lines) {
		return Draft{}, fmt.Errorf("%w: answer %w: %d ingredient lines for %d gaps",
			ErrLLMFailed, ErrInvalidStructured, len(answered), len(gaps.Lines))
	}

	d := rule
	d.ParserVersion = ParserVersion
	d.Threshold = f.cfg.ConfidenceThreshold
	d.UsedLLM = true
	d.Issues = nil
	d.Components = make([]Component, len(rule.Components))
	for i, c := range rule.Components {
		c.Lines = append([]Line(nil), c.Lines...)
		c.Steps = append([]Step(nil), c.Steps...)
		d.Components[i] = c
	}
	a := &assembler{rs: newResolver(ctx, f.deps.Catalog), d: &d}

	if gaps.Name {
		d.Name = orRule(fromLLM(stated(s.Name, llmFieldConfidence)), rule.Name)
	}
	if gaps.Author {
		d.Author = orRule(fromLLM(stated(s.Author, llmFieldConfidence)), rule.Author)
	}
	if gaps.Description {
		d.Description = orRule(fromLLM(stated(s.Description, llmFieldConfidence)), rule.Description)
	}
	if gaps.Yield {
		answer := Draft{}
		(&assembler{rs: a.rs, d: &answer}).yield([]string{s.Yield})
		if answer.Yield.Value.Unit != "" || !answer.Servings.Value.IsAbsent() {
			d.Yield, d.Servings = answer.Yield, answer.Servings
			d.Yield.FromLLM = answer.Yield.Value.Unit != ""
			d.Servings.FromLLM = !d.Servings.Value.IsAbsent()
		}
	}
	if gaps.Prep {
		d.Prep = orRule(llmDuration(s.PrepTime), rule.Prep)
	}
	if gaps.Cook {
		d.Cook = orRule(llmDuration(s.CookTime), rule.Cook)
	}
	if gaps.Total {
		d.Total = orRule(llmDuration(s.TotalTime), rule.Total)
	}

	if gaps.Components {
		d.Components = nil
		kept, keptFrom := keptSteps(rule, s, f.cfg.ConfidenceThreshold)
		for i, sc := range s.Components {
			c := Component{Name: strings.TrimSpace(sc.Name), FromLLM: true}
			for _, raw := range sc.Ingredients {
				if l, ok := a.line(raw); ok {
					l.FromLLM = true
					c.Lines = append(c.Lines, l)
				}
			}
			c.Steps = kept[i]
			if len(c.Steps) == 0 {
				for _, text := range sc.Steps {
					if text = strings.TrimSpace(text); text != "" {
						st := step(text, llmFieldConfidence)
						st.FromLLM = true
						c.Steps = append(c.Steps, st)
					}
				}
			}
			d.Components = append(d.Components, c)
		}
		// The user's graphs are translated first, so a parallel step after one
		// copies dependencies that are valid for the new numbering.
		renumberUserGraphs(d.Components, kept, keptFrom)
		// Kept and supplied steps alike go through the rules, so the graph is
		// the same one a rule-based draft of these steps would have.
		inferGraph(d.Components)
	}
	if len(gaps.Steps) > 0 {
		if err := applyStepGraph(d.Components, gaps.Steps, s.StepGraph); err != nil {
			return Draft{}, fmt.Errorf("%w: answer %w", ErrLLMFailed, err)
		}
	}
	for i, g := range gaps.Lines {
		// A blank replacement is the provider declining; the rule line stays.
		if l, ok := a.line(answered[i]); ok {
			l.FromLLM = true
			d.Components[g.Component].Lines[g.Line] = l
		}
	}

	// Equipment: what the rules found, then the LLM's list when there was none,
	// then catalog names in any text the LLM supplied.
	set := newEquipmentSet()
	for _, e := range rule.Equipment {
		set.add(e)
	}
	if gaps.Equipment {
		for _, name := range s.Equipment {
			a.named(set, name, true)
		}
	}
	a.scanEquipment(set)
	d.Equipment = set.items
	if a.rs.err != nil {
		return Draft{}, fmt.Errorf("read catalog while drafting: %w", a.rs.err)
	}

	d.Proposed = ProposedFor(d.Components, nil)
	d.score(a.requiredStructure())
	return d, nil
}

// keptSteps are the rule steps that met the threshold, placed under the
// answer's components: by index when the answer has as many components as the
// rules found, else all in the first (the answer's grouping is the weaker
// evidence of where a step belongs). Indexed by answer component. from gives,
// for each kept step, the number it had in rule.
func keptSteps(rule Draft, s Structured, threshold float64) (out map[int][]Step, from map[int][]int) {
	out, from = map[int][]Step{}, map[int][]int{}
	n := 0
	byIndex := len(s.Components) == len(rule.Components)
	for i, c := range rule.Components {
		for _, st := range c.Steps {
			n++
			if st.Confidence < threshold {
				continue
			}
			at := 0
			if byIndex {
				at = i
			}
			out[at], from[at] = append(out[at], st), append(from[at], n)
		}
	}
	return out, from
}

// renumberUserGraphs puts the graphs the user set back on the kept steps of
// comps, which inferGraph left alone. The steps are numbered afresh, because
// weak steps were dropped and the answer's grouping may have moved the rest, so
// each dependency is translated from the step's old number to its new one. A
// dependency on a step that was dropped is dropped with it.
func renumberUserGraphs(comps []Component, kept map[int][]Step, from map[int][]int) {
	newNumber := map[int]int{}
	n := 0
	for ci, c := range comps {
		for si := range c.Steps {
			n++
			if si < len(kept[ci]) {
				newNumber[from[ci][si]] = n
			}
		}
	}
	for ci := range comps {
		for si := range comps[ci].Steps {
			st := &comps[ci].Steps[si]
			if !st.GraphByUser {
				continue
			}
			var deps []int
			for _, d := range st.DependsOn {
				if nd, ok := newNumber[d]; ok {
					deps = append(deps, nd)
				}
			}
			slices.Sort(deps)
			st.DependsOn = slices.Compact(deps)
		}
	}
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
