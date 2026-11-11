// Package ingest is the ingestion service: every operation that reads a
// recipe from a URL, pasted text or a photograph into a draft, and the retry
// that asks the LLM to read a draft again, written once for the web screens
// and the MCP tools (ADR-00043). It is in-process. It is not a network API, it
// speaks neither HTTP nor JSON and it reads no cookie or header. It follows the
// pattern internal/api/catalog and internal/api/households set: a method takes
// the context, then the caller.Caller, then its own arguments; it refuses a
// Caller that is not a member of the household with ErrNotMember before
// touching the pipeline (ADR-00018); and it returns the domain's errors, which
// the web turns into statuses in internal/web/layout (statusTable) and the MCP
// tools into tool errors. The Caller is the same whichever credential
// authenticated: a session, a household key, a personal key or an OAuth grant
// (ADR-00031).
//
// The pipeline itself (acquire, extract, parse, score, draft, and the LLM
// fallback) stays in internal/ingest, which this package wraps. The order it
// enforces is untouched here: the rule-based path always runs first, and the
// provider is asked only for a draft that scored below the threshold or when
// a person presses retry. What the LLM returns is a Draft like any other, and
// reaches a recipe only through the review screen (ADR-00006, ADR-00034). The
// package re-exports the types, errors and pure helpers its callers need as
// aliases, so internal/web and internal/mcp import this package and not
// internal/ingest; the ingest-through-api rule in .golangci.yml fails make
// lint when they do.
//
// The pipeline reads the household's catalog overlay from the context. The
// membership check supplies it: the household the pipeline reads is the one
// the check found, never the Caller's claim or the context's.
//
// Bound serves the callers whose seams carry only a context: the web handler's
// Drafter, PhotoDrafter and Retrier, the MCP endpoint's Drafter and the
// re-import's URLDrafter. Each call names the Caller from its context and goes
// through the Service.
//
// Preview drafts without holding (ADR-00048): from a URL through the same
// pipeline call an import makes, or from a saved recipe's original source
// snapshot through the pipeline's FromContent, which is what a URL draft runs
// after its fetch. It writes nothing.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/api/caller"
	"github.com/kaecyra/gobbler/internal/config"
	domain "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/ingest/ocr"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// Caller is who an operation is for (caller.Caller).
type Caller = caller.Caller

// Members is the membership lookup the service enforces (caller.Members).
type Members = caller.Members

// The ingestion values the callers of the service work with.
type (
	// Draft is a recipe as the pipeline read it, awaiting review
	// (ingest.Draft).
	Draft = domain.Draft
	// Field is one value of a Draft with the confidence it was read with
	// (ingest.Field).
	Field[T any] = domain.Field[T]
	// Component is a group of lines and steps in a Draft (ingest.Component).
	Component = domain.Component
	// Line is an ingredient line of a Draft (ingest.Line).
	Line = domain.Line
	// Step is an instruction of a Draft (ingest.Step).
	Step = domain.Step
	// Equipment is a piece of equipment a Draft names (ingest.Equipment).
	Equipment = domain.Equipment
	// AuthorNote is a note the author wrote on the page (ingest.AuthorNote).
	AuthorNote = domain.AuthorNote
	// ProposedIngredient is a catalog entry a draft would add
	// (ingest.ProposedIngredient).
	ProposedIngredient = domain.ProposedIngredient
	// Reimport is what a draft made again from its source carries
	// (ingest.Reimport).
	Reimport = domain.Reimport
	// Confirmed is a draft the review screen approved (ingest.Confirmed). Only
	// Confirm makes one.
	Confirmed = domain.Confirmed
	// Saved is what saving a confirmed draft produced (ingest.Saved).
	Saved = domain.Saved
	// PictureChoice is the picture a person chose for a draft
	// (ingest.PictureChoice).
	PictureChoice = domain.PictureChoice
	// UnknownUnitError is a unit the catalog does not have
	// (ingest.UnknownUnitError).
	UnknownUnitError = domain.UnknownUnitError
	// URLDrafter drafts a recipe from a URL (ingest.URLDrafter). *Bound
	// satisfies it.
	URLDrafter = domain.URLDrafter
	// Provider is the LLM provider interface (ingest.Provider, ADR-00007).
	Provider = domain.Provider
	// DescriptionSource reads a YouTube video's description
	// (ingest.DescriptionSource, ADR-00054).
	DescriptionSource = domain.DescriptionSource
	// OCRRunner reads the text out of a photograph by running tesseract
	// (ocr.Runner). The zero value uses the defaults.
	OCRRunner = ocr.Runner
	// Photos holds the photos behind OCR drafts, in memory only
	// (ingest.Photos).
	Photos = domain.Photos
	// CatalogReader is what the pipeline reads from the catalog
	// (ingest.CatalogReader). *catalog.Store satisfies it; every call runs in
	// the household the Caller was admitted to.
	CatalogReader = domain.CatalogReader
)

// The picture choices a draft can carry (ingest.PictureUnchosen and the
// others).
const (
	PictureUnchosen = domain.PictureUnchosen
	PictureNone     = domain.PictureNone
	PictureKeep     = domain.PictureKeep
)

// The note issues the review screen explains (ingest.IssueNoteTooLong and the
// others).
const (
	IssueNoteTooLong     = domain.IssueNoteTooLong
	IssueNoteNutrition   = domain.IssueNoteNutrition
	IssueNotePublication = domain.IssueNotePublication
	IssueNoteMealPlan    = domain.IssueNoteMealPlan
)

// PasteOnlyError is the refusal of a paste-only link, carrying the sentence
// for the person (ingest.PasteOnlyError).
type PasteOnlyError = domain.PasteOnlyError

// The errors a service method returns, and the ones the callers of the drafts
// branch on, for errors.Is. They are the domain's own values, so errors.Is
// matches either name.
var (
	// ErrFetch is a page that could not be fetched (ingest.ErrFetch).
	ErrFetch = domain.ErrFetch
	// ErrPasteOnly is a link gobbler declines to fetch because its recipe can
	// only be pasted (ingest.ErrPasteOnly). It also wraps ErrFetch.
	ErrPasteOnly = domain.ErrPasteOnly
	// ErrDraftGone is a draft that expired or was discarded
	// (ingest.ErrDraftGone).
	ErrDraftGone = domain.ErrDraftGone
	// ErrNoProvider is a retry with no LLM provider configured
	// (ingest.ErrNoProvider).
	ErrNoProvider = domain.ErrNoProvider
	// ErrLLMFailed is a provider that failed or answered badly
	// (ingest.ErrLLMFailed).
	ErrLLMFailed = domain.ErrLLMFailed
	// ErrIncompleteDraft is a draft that cannot be confirmed yet
	// (ingest.ErrIncompleteDraft).
	ErrIncompleteDraft = domain.ErrIncompleteDraft
	// ErrBadPictureChoice is a picture that is not one of the draft's
	// candidates (ingest.ErrBadPictureChoice).
	ErrBadPictureChoice = domain.ErrBadPictureChoice
	// ErrNoSourceURL is a re-import of a recipe with no source address
	// (ingest.ErrNoSourceURL).
	ErrNoSourceURL = domain.ErrNoSourceURL
	// ErrEmptyInput is a page with nothing in it to read (extract.ErrEmptyInput).
	ErrEmptyInput = extract.ErrEmptyInput
	// ErrNoText is a photograph OCR found no text in (ocr.ErrNoText).
	ErrNoText = ocr.ErrNoText
	// ErrOCRFailed is OCR that ran and failed on the photograph (ocr.ErrFailed).
	ErrOCRFailed = ocr.ErrFailed
	// ErrNotMember is returned for a Caller that is not a verified member of a
	// household (caller.ErrNotMember).
	ErrNotMember = caller.ErrNotMember
)

// ErrNoPageSnapshot is a preview of a saved recipe that was not imported from
// a web page, so it holds no page to read again (ADR-00048).
var ErrNoPageSnapshot = errors.New("this recipe was not imported from a web page, so there is no page to read again")

// ErrPreviewSource is a preview given both a URL and a recipe, or neither.
var ErrPreviewSource = errors.New("a preview reads a url or a saved recipe, not both and not neither")

// Confirm turns a draft the review screen approved into a Confirmed one
// (ingest.Confirm). It is pure and touches no household data.
func Confirm(d Draft) (Confirmed, error) { return domain.Confirm(d) }

// ProposedFor lists the catalog entries a draft's lines and equipment would
// add (ingest.ProposedFor). It is pure.
func ProposedFor(comps []Component, prior []ProposedIngredient) []ProposedIngredient {
	return domain.ProposedFor(comps, prior)
}

// SourceHost names the host of a recipe's source address (ingest.SourceHost).
func SourceHost(sourceURL string) string { return domain.SourceHost(sourceURL) }

// NewPhotos returns an empty photo cache (ingest.NewPhotos).
func NewPhotos(now func() time.Time) *Photos { return domain.NewPhotos(now) }

// Pipeline is what the service calls to draft a recipe. *ingest.Pipeline
// satisfies it.
type Pipeline interface {
	FromURL(ctx context.Context, rawURL string) (Draft, error)
	FromContent(ctx context.Context, page []byte, sourceURL string) (Draft, error)
	FromText(ctx context.Context, text string) (Draft, error)
	FromOCR(ctx context.Context, text string, image []byte, imageMIME string) (Draft, error)
}

// Fallback is what the service calls to retry a draft with the LLM.
// *ingest.Fallback satisfies it.
type Fallback interface {
	Retry(ctx context.Context, d Draft) (Draft, error)
	Configured(ctx context.Context, d Draft) bool
}

// Originals is what a preview reads of a saved recipe: its original source
// snapshot with the draft that held it, and its first version for a snapshot
// no draft held. *store.Store satisfies it; every call runs in the household
// the Caller was admitted to.
type Originals interface {
	FirstSnapshot(ctx context.Context, id int64, kind store.SnapshotKind) (store.FirstSource, bool, error)
	GetVersion(ctx context.Context, id, version int64) (recipe.Recipe, error)
}

// Deps is what the service is built from.
type Deps struct {
	// Catalog resolves ingredient names, units and equipment for the pipeline.
	// Required.
	Catalog CatalogReader
	// Members is the membership lookup. Required.
	Members Members
	// Provider is the LLM provider of the fallback. Optional: without one a
	// draft below the threshold is only flagged, and Retry is ErrNoProvider.
	Provider Provider
	// Descriptions reads a YouTube video's description (ADR-00054). Optional:
	// without it a YouTube link is ingested as any page.
	Descriptions DescriptionSource
	// Logger receives the fallback's warnings; nil means slog.Default().
	Logger *slog.Logger
	// Originals is read by a preview of a saved recipe. Optional: without it
	// such a preview is an error, and every other operation works.
	Originals Originals
}

// Service is the ingestion operations. It is safe for concurrent use.
type Service struct {
	pipeline  Pipeline
	fallback  Fallback
	members   Members
	originals Originals
}

// New returns a Service over a pipeline built from cfg and d. Catalog and
// Members are required, and cfg must be one the pipeline can work with; a bad
// value is an error here, not on the first request.
func New(cfg config.Ingestion, d Deps) (*Service, error) {
	if d.Members == nil {
		return nil, errors.New("ingest service: members are required")
	}
	p, err := domain.New(cfg, domain.Deps{Catalog: d.Catalog, Provider: d.Provider, Descriptions: d.Descriptions, Logger: d.Logger})
	if err != nil {
		return nil, err
	}
	return &Service{pipeline: p, fallback: p.Fallback(), members: d.Members, originals: d.Originals}, nil
}

// enter checks the Caller and returns a context that runs in its household
// (caller.Enter). It is the first line of every method.
func (s *Service) enter(ctx context.Context, c Caller) (context.Context, error) {
	return caller.Enter(ctx, s.members, c)
}

// FromURL fetches the page at rawURL and drafts the recipe on it. A failed or
// timed-out fetch wraps ErrFetch.
func (s *Service) FromURL(ctx context.Context, c Caller, rawURL string) (Draft, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Draft{}, err
	}
	return s.pipeline.FromURL(ctx, rawURL)
}

// FromText drafts a recipe from pasted text.
func (s *Service) FromText(ctx context.Context, c Caller, text string) (Draft, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Draft{}, err
	}
	return s.pipeline.FromText(ctx, text)
}

// FromOCR drafts a recipe from a photograph and the text tesseract read off it.
// With a provider configured a vision model transcribes the photo and the
// transcript is the source (ADR-00053). The image is held on the draft in memory
// only, for the vision fallback.
func (s *Service) FromOCR(ctx context.Context, c Caller, text string, image []byte, imageMIME string) (Draft, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Draft{}, err
	}
	return s.pipeline.FromOCR(ctx, text, image, imageMIME)
}

// Retry asks the LLM to read d's source again, whatever d scored. It is only
// for the review screen's retry button, and returns a draft for review; it
// saves nothing. It is ErrNoProvider when no provider is configured.
func (s *Service) Retry(ctx context.Context, c Caller, d Draft) (Draft, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Draft{}, err
	}
	return s.fallback.Retry(ctx, d)
}

// PreviewSource is what a preview reads: a page's URL, or a saved recipe's id.
// Exactly one is set.
type PreviewSource struct {
	URL      string
	RecipeID int64
}

// Preview is a draft that is not held. SnapshotParserVersion is the parser
// version stored with the snapshot a saved recipe's preview read, empty for a
// URL and when the snapshot recorded none; Draft.ParserVersion is the current
// one.
type Preview struct {
	Draft                 Draft
	SnapshotParserVersion string
}

// Preview drafts what an import would hold, and holds nothing: no proposal,
// no snapshot, no picture candidate (ADR-00048). From a URL it is FromURL.
// From a saved recipe it reads the recipe's first HTML snapshot, the original
// (ADR-00042), and drafts it with the address it was served from (sourceOf),
// fetching nothing; a recipe with no HTML snapshot is ErrNoPageSnapshot. The
// rule-based path runs first and the LLM only below the threshold, as for an
// import (ADR-00006).
func (s *Service) Preview(ctx context.Context, c Caller, src PreviewSource) (Preview, error) {
	rawURL := strings.TrimSpace(src.URL)
	if (rawURL == "") == (src.RecipeID == 0) {
		return Preview{}, ErrPreviewSource
	}
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Preview{}, err
	}
	if rawURL != "" {
		d, err := s.pipeline.FromURL(ctx, rawURL)
		if err != nil {
			return Preview{}, err
		}
		return Preview{Draft: d}, nil
	}
	if s.originals == nil {
		return Preview{}, errors.New("ingest service: previewing a saved recipe needs Originals")
	}
	first, found, err := s.originals.FirstSnapshot(ctx, src.RecipeID, store.SnapshotHTML)
	if err != nil {
		return Preview{}, fmt.Errorf("read the original page of recipe %d: %w", src.RecipeID, err)
	}
	if !found {
		return Preview{}, fmt.Errorf("preview recipe %d: %w", src.RecipeID, ErrNoPageSnapshot)
	}
	addr, err := s.sourceOf(ctx, src.RecipeID, first)
	if err != nil {
		return Preview{}, err
	}
	d, err := s.pipeline.FromContent(ctx, first.Snapshot.Content, addr)
	if err != nil {
		return Preview{}, err
	}
	return Preview{Draft: d, SnapshotParserVersion: first.Snapshot.ParserVersion}, nil
}

// sourceOf is the address recipe id's original page was served from. A
// snapshot does not record it; the draft whose source the snapshot was does,
// and an accepted draft is never pruned (ADR-00035). That covers a recipe
// typed in and given its first page by a later re-import, whose first version
// has no address or another one. A snapshot no draft held was stored before
// drafts were proposals (migration 00027), when a snapshot was written only
// with the recipe it was imported into, so the recipe's first version is the
// version of that time and carries the address the import saved. A share's
// copy of a recipe is held by no draft either, and its first version carries
// the address the shared recipe had when it was copied.
func (s *Service) sourceOf(ctx context.Context, id int64, first store.FirstSource) (string, error) {
	if first.DraftDocument != nil {
		held, err := domain.DecodeDraft(first.DraftDocument, first.Snapshot)
		if err != nil {
			return "", fmt.Errorf("read the draft that held the original page of recipe %d: %w", id, err)
		}
		return held.Source.URL, nil
	}
	r, err := s.originals.GetVersion(ctx, id, 1)
	if err != nil {
		return "", fmt.Errorf("read the first version of recipe %d: %w", id, err)
	}
	return r.SourceURL, nil
}

// Configured reports whether a retry of d can work now: the LLM is configured
// for the task d would be sent as. It reads configuration, not household data,
// so it takes no Caller.
func (s *Service) Configured(ctx context.Context, d Draft) bool { return s.fallback.Configured(ctx, d) }

// Bound is the Service for callers whose seams carry a context and no Caller.
// Each call asks who for the Caller of its context and goes through the
// Service, so the membership check runs on every call. A who that finds no
// identity returns the zero Caller, which the Service refuses with
// ErrNotMember: a Bound with nobody to speak for fails closed.
type Bound struct {
	s   *Service
	who func(ctx context.Context) Caller
}

// Bind returns the Service speaking for the Caller who names from a call's
// context. A web adapter passes caller.FromContext; the MCP adapter builds the
// Caller from the credential on the context.
func (s *Service) Bind(who func(ctx context.Context) Caller) *Bound {
	return &Bound{s: s, who: who}
}

// FromURL is Service.FromURL.
func (b *Bound) FromURL(ctx context.Context, rawURL string) (Draft, error) {
	return b.s.FromURL(ctx, b.who(ctx), rawURL)
}

// FromText is Service.FromText.
func (b *Bound) FromText(ctx context.Context, text string) (Draft, error) {
	return b.s.FromText(ctx, b.who(ctx), text)
}

// FromOCR is Service.FromOCR.
func (b *Bound) FromOCR(ctx context.Context, text string, image []byte, imageMIME string) (Draft, error) {
	return b.s.FromOCR(ctx, b.who(ctx), text, image, imageMIME)
}

// Retry is Service.Retry.
func (b *Bound) Retry(ctx context.Context, d Draft) (Draft, error) {
	return b.s.Retry(ctx, b.who(ctx), d)
}

// Configured is Service.Configured.
func (b *Bound) Configured(ctx context.Context, d Draft) bool { return b.s.Configured(ctx, d) }

// Preview is Service.Preview.
func (b *Bound) Preview(ctx context.Context, src PreviewSource) (Preview, error) {
	return b.s.Preview(ctx, b.who(ctx), src)
}
