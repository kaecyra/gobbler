// Package ingest turns a URL or pasted text into a recipe draft, and saves a
// draft once the user has confirmed it (ADR-00006).
//
// The pipeline is acquire, extract, parse, score, draft. Every step before the
// review screen is rule-based and deterministic: the same input and the same
// ParserVersion produce the same Draft. The one LLM step ahead of the rules is a
// photo's transcription (ADR-00053); its output is the stored source, and the
// rules structure it. A line or field the rules cannot read
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
	"net/http"
	"strings"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/netguard"
	"github.com/kaecyra/gobbler/internal/parse"
	"github.com/kaecyra/gobbler/internal/store"
)

// ParserVersion identifies the rules that produce a Draft. Bump it with any
// change to extraction, parsing or scoring that can change a draft, so a draft
// can be told apart from one made by older rules.
const ParserVersion = "21"

// Deps are the collaborators a Pipeline needs. Each is an interface sized to
// what this package calls, so tests substitute at the boundary.
type Deps struct {
	// HTTP fetches pages. Nil means a netguard client, which never connects to
	// a private address; the per-request timeout comes from the configuration
	// either way.
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
	// guarded is the client used when Deps.HTTP is nil.
	guarded *http.Client
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
	return &Pipeline{cfg: cfg, deps: d, guarded: netguard.NewClient(netguard.Options{Redirects: netguard.MaxRedirects, AllowLoopback: cfg.AllowLoopbackFetch})}, nil
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
	return p.FromContent(ctx, page, finalURL)
}

// FromContent drafts the recipe on a page already in hand: page is its content
// and sourceURL the address it was served from. FromURL is a fetch followed by
// this call, so a draft made from a fetched page and one made from the same
// bytes held elsewhere (a saved recipe's source snapshot, ADR-00048) cannot
// differ. It fetches nothing; the draft's Source is page, as given.
func (p *Pipeline) FromContent(ctx context.Context, page []byte, sourceURL string) (Draft, error) {
	res, err := extract.Extract(page, sourceURL)
	if err != nil {
		return Draft{}, fmt.Errorf("extract recipe from %s: %w", sourceURL, err)
	}
	return p.draftWithFallback(ctx, newSource(store.SnapshotHTML, sourceURL, page), res)
}

// FromText drafts a recipe from pasted text, which is taken as given.
func (p *Pipeline) FromText(ctx context.Context, text string) (Draft, error) {
	if strings.TrimSpace(text) == "" {
		return Draft{}, fmt.Errorf("pasted text: %w", parse.ErrEmptyText)
	}
	res := extract.Result{Method: extract.MethodText, Text: text}
	return p.draftWithFallback(ctx, newSource(store.SnapshotText, "", []byte(text)), res)
}

// FromOCR drafts a recipe from a photo. text is what tesseract read from
// image. With no provider configured, text is the source snapshot, stored as
// SnapshotOCR, and goes through the same sectioning, parsing, scoring and
// fallback as pasted text.
//
// With a provider, a vision model first transcribes the photo, and the
// transcript is the source snapshot instead; the rule parser structures it
// unchanged, and text is kept beside it as the check on its numbers: an
// ingredient line whose number text never read falls below the threshold, so
// the gap-fill fallback shows it the photo (ADR-00053). A transcription that
// fails, times out or comes back blank is logged and the draft is made from text
// alone; so is a photo over maxVisionImageBytes.
//
// The image is held on the draft's Source in memory only, so that a draft below
// the threshold, or one the user asks to retry, can show the photo to a
// vision-capable provider (ADR-00006); it is never saved.
func (p *Pipeline) FromOCR(ctx context.Context, text string, image []byte, imageMIME string) (Draft, error) {
	if strings.TrimSpace(text) == "" {
		return Draft{}, fmt.Errorf("photo text: %w", parse.ErrEmptyText)
	}
	content, check := text, ""
	if transcript, ok := p.transcribe(ctx, image, imageMIME); ok {
		content, check = transcript, text
	}
	return p.fromPhotoSource(ctx, content, check, image, imageMIME)
}

// fromPhotoSource drafts a photo's recipe from its stored source: content is
// the ocr snapshot and check the ocr_check snapshot, empty when there is none.
// It is what FromOCR runs after reading the photo, and the entry a replay of
// the two snapshots uses, so a replay cannot differ from the original draft
// (ADR-00048, ADR-00053). image may be empty.
func (p *Pipeline) fromPhotoSource(ctx context.Context, content, check string, image []byte, imageMIME string) (Draft, error) {
	res := extract.Result{Method: extract.MethodText, Text: content}
	src := newSource(store.SnapshotOCR, "", []byte(content))
	src.Image, src.ImageMIME, src.CheckText = image, imageMIME, check
	return p.draftWithFallback(ctx, src, res)
}

// transcribe asks the provider for the photo's text. It reports false when
// there is no provider or nothing to send, which is the normal no-LLM path and
// needs no log, and when the photo is too large or the provider could not read
// the page, which it logs. The degraded draft from the tesseract text is still
// reviewable, so none of these is an error (error-handling: fall back only when
// the degraded result is useful).
func (p *Pipeline) transcribe(ctx context.Context, image []byte, imageMIME string) (string, bool) {
	if p.deps.Provider == nil || len(image) == 0 {
		return "", false
	}
	log := p.Fallback().log()
	if len(image) > maxVisionImageBytes {
		log.WarnContext(ctx, "photo not transcribed, using the OCR text only",
			"reason", "image_over_provider_limit", "image_bytes", len(image), "limit_bytes", maxVisionImageBytes)
		return "", false
	}
	transcript, err := p.deps.Provider.Transcribe(ctx, TranscribeRequest{Image: image, ImageMIME: imageMIME})
	if err == nil && strings.TrimSpace(transcript) == "" {
		err = errors.New("the transcript is empty")
	}
	if err != nil {
		log.WarnContext(ctx, "photo transcription failed, using the OCR text only", "error", err.Error())
		return "", false
	}
	return transcript, true
}
