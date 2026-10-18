// Package ingest turns a URL or pasted text into a recipe draft, and saves a
// draft once the user has confirmed it (ADR-00006).
//
// The pipeline is acquire, extract, parse, score, draft. Every step before the
// review screen is rule-based and deterministic: the same input and the same
// ParserVersion produce the same Draft. A line or field the rules cannot read
// is low confidence on the Draft, never an error; an error means the pipeline
// itself broke (the fetch failed, the catalog could not be read).
//
// Nothing is saved by the pipeline. Saver.Save accepts only a Confirmed draft,
// which Confirm produces from a draft the review screen has approved, so no
// code path stores an ingested recipe without review. Save writes the recipe
// and its unaltered source snapshot through the store API (ADR-00015) and
// creates the ingredients and equipment the draft proposed, unreviewed, only
// then (ADR-00008).
//
// The LLM provider interface (ADR-00007) is declared here and consumed by the
// fallback (fallback.go): a draft below the threshold, or one the user asks to
// retry, goes to the provider, and a failure leaves the rule-based draft in
// place with the error recorded on it. This package depends on the interface and
// never on a provider SDK.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/parse"
	"github.com/kaecyra/gobbler/internal/store"
)

// ParserVersion identifies the rules that produce a Draft. Bump it with any
// change to extraction, parsing or scoring that can change a draft, so a draft
// can be told apart from one made by older rules.
const ParserVersion = "9"

// Deps are the collaborators a Pipeline needs. Each is an interface sized to
// what this package calls, so tests substitute at the boundary.
type Deps struct {
	// HTTP fetches pages. Nil means a plain http.Client; the per-request
	// timeout comes from the configuration either way.
	HTTP HTTPClient
	// Catalog resolves ingredient names, units and equipment.
	Catalog CatalogReader
	// Provider is the LLM provider for the fallback. Nil means none is
	// configured: a draft below the threshold is then only flagged, and
	// Fallback.Retry returns ErrNoProvider.
	Provider Provider
	// Logger receives the fallback's warnings; nil means slog.Default().
	Logger *slog.Logger
}

// Pipeline acquires, extracts, parses and scores input into drafts. It holds
// no per-request state and is safe for concurrent use.
type Pipeline struct {
	cfg  config.Ingestion
	deps Deps
}

// New returns a Pipeline. It rejects configuration the pipeline cannot work
// with, so a bad value fails at start-up rather than on the first request.
func New(cfg config.Ingestion, d Deps) (*Pipeline, error) {
	if d.Catalog == nil {
		return nil, errors.New("ingest: a catalog is required to match ingredients")
	}
	if cfg.FetchTimeout <= 0 {
		return nil, fmt.Errorf("ingest: fetch timeout must be positive, got %s", cfg.FetchTimeout)
	}
	if strings.TrimSpace(cfg.UserAgent) == "" {
		return nil, errors.New("ingest: user agent must not be empty")
	}
	if cfg.ConfidenceThreshold < 0 || cfg.ConfidenceThreshold > 1 {
		return nil, fmt.Errorf("ingest: confidence threshold must be in [0, 1], got %v", cfg.ConfidenceThreshold)
	}
	return &Pipeline{cfg: cfg, deps: d}, nil
}

// draftWithFallback assembles the rule-based draft and, only when it scored
// below the threshold and a provider is configured, asks the LLM to fill its gaps
// (ADR-00006, ADR-00020).
func (p *Pipeline) draftWithFallback(ctx context.Context, src Source, res extract.Result) (Draft, error) {
	d, err := p.assemble(ctx, src, res)
	if err != nil {
		return Draft{}, err
	}
	return p.Fallback().autoFallback(ctx, d)
}

// FromURL fetches the page at rawURL and drafts the recipe on it. A failed or
// timed-out fetch is an error wrapping ErrFetch.
func (p *Pipeline) FromURL(ctx context.Context, rawURL string) (Draft, error) {
	page, finalURL, err := p.fetch(ctx, rawURL)
	if err != nil {
		return Draft{}, err
	}
	res, err := extract.Extract(page, finalURL)
	if err != nil {
		return Draft{}, fmt.Errorf("extract recipe from %s: %w", finalURL, err)
	}
	return p.draftWithFallback(ctx, newSource(store.SnapshotHTML, finalURL, page), res)
}

// FromText drafts a recipe from pasted text, which is taken as given.
func (p *Pipeline) FromText(ctx context.Context, text string) (Draft, error) {
	if strings.TrimSpace(text) == "" {
		return Draft{}, fmt.Errorf("pasted text: %w", parse.ErrEmptyText)
	}
	res := extract.Result{Method: extract.MethodText, Text: text}
	return p.draftWithFallback(ctx, newSource(store.SnapshotText, "", []byte(text)), res)
}

// FromOCR drafts a recipe from a photo. text is what OCR read from image; it
// is the source snapshot, stored as SnapshotOCR, and goes through the same
// sectioning, parsing, scoring and fallback as pasted text. The image is held
// on the draft's Source in memory only, so that a draft below the threshold, or
// one the user asks to retry, can show the photo to a vision-capable provider
// (ADR-00006); it is never saved.
func (p *Pipeline) FromOCR(ctx context.Context, text string, image []byte, imageMIME string) (Draft, error) {
	if strings.TrimSpace(text) == "" {
		return Draft{}, fmt.Errorf("photo text: %w", parse.ErrEmptyText)
	}
	res := extract.Result{Method: extract.MethodText, Text: text}
	src := newSource(store.SnapshotOCR, "", []byte(text))
	src.Image, src.ImageMIME = image, imageMIME
	return p.draftWithFallback(ctx, src, res)
}
