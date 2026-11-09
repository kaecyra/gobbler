package recipes

import (
	"database/sql"
	"log/slog"

	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/picture"
	"github.com/kaecyra/gobbler/internal/store"
)

// The types below are aliases of the domain types the callers of the service
// work with, so a value the service returns is the domain's own value and the
// adapters never import the domain package (see the package doc).

// Store is the recipe store the service is built over (store.Store). The
// router holds one so that it names no domain package; every operation on it
// goes through a Service.
type Store = store.Store

// Pictures is the picture service (picture.Service), the one the picture
// routes, the review screen and the recipe screens share.
type Pictures = picture.Service

// Candidate is a picture candidate of a draft (picture.Candidate).
type Candidate = picture.Candidate

// The store values the callers of the service work with.
type (
	// Card is a recipe as the library lists it (store.Card).
	Card = store.Card
	// MadeEntry is one made-it entry of the recently made list (store.MadeEntry).
	MadeEntry = store.MadeEntry
	// MadeIt is a made-it entry of a recipe (store.MadeIt).
	MadeIt = store.MadeIt
	// Note is a note on a recipe (store.Note).
	Note = store.Note
	// Info is a recipe's version and bookkeeping (store.Info).
	Info = store.Info
	// Summary is a recipe as listed in a search (store.Summary).
	Summary = store.Summary
	// Revision is one saved version of a recipe (store.Revision).
	Revision = store.Revision
	// Thumbnail is the stored preview image of a recipe's video (store.Thumbnail).
	Thumbnail = store.Thumbnail
	// SearchQuery selects recipes (store.SearchQuery).
	SearchQuery = store.SearchQuery
	// Proposal is a held edit, draft or re-import (store.Proposal).
	Proposal = store.Proposal
	// ProposalRef names the proposal behind a revision (store.ProposalRef).
	ProposalRef = store.ProposalRef
	// Provenance says who proposed a document and how (store.Provenance).
	Provenance = store.Provenance
	// Credential is the credential behind a proposal (store.Credential).
	Credential = store.Credential
	// DraftInput is a draft to hold (store.DraftInput).
	DraftInput = store.DraftInput
	// ProposalState is where a proposal is in its life (store.ProposalState).
	ProposalState = store.ProposalState
	// ProposalKind says what a proposal is for (store.ProposalKind).
	ProposalKind = store.ProposalKind
	// SnapshotKind is the kind of a source snapshot (store.SnapshotKind).
	SnapshotKind = store.SnapshotKind
)

// The kinds and states callers branch on.
const (
	ProposalEdit       = store.ProposalEdit
	ProposalPending    = store.ProposalPending
	ProposalAccepted   = store.ProposalAccepted
	ProposalRejected   = store.ProposalRejected
	ProposalSuperseded = store.ProposalSuperseded
	ProposalExpired    = store.ProposalExpired
	SnapshotOCR        = store.SnapshotOCR
)

// The errors a service method returns, for callers to branch on with
// errors.Is.
var (
	ErrNotFound         = store.ErrNotFound
	ErrInvalid          = store.ErrInvalid
	ErrUnknownReference = store.ErrUnknownReference
	ErrStaleVersion     = store.ErrStaleVersion
	ErrStaleProposal    = store.ErrStaleProposal
	ErrNotPending       = store.ErrNotPending
	ErrDraftLimit       = store.ErrDraftLimit
	ErrNoIdentity       = store.ErrNoIdentity
	ErrNoHousehold      = store.ErrNoHousehold
	ErrOnShoppingList   = store.ErrOnShoppingList
	// ErrDraftGone names no pending draft; ErrDraftChanged is a revision made
	// from a draft that has changed since; ErrInvalidRevision is a revision
	// that cannot be a draft (ADR-00048).
	ErrDraftGone       = ingest.ErrDraftGone
	ErrDraftChanged    = ingest.ErrDraftChanged
	ErrInvalidRevision = ingest.ErrInvalidRevision
)

// An agent's revision of a held draft, and a pending draft as an agent lists
// it (ADR-00048).
type (
	// DraftRevision is an agent's whole recipe for a held draft (ingest.Revision).
	DraftRevision = ingest.Revision
	// RevisedComponent is a component of a revision (ingest.RevisedComponent).
	RevisedComponent = ingest.RevisedComponent
	// RevisedLine is an ingredient line of a revision (ingest.RevisedLine).
	RevisedLine = ingest.RevisedLine
	// RevisedStep is a step of a revision (ingest.RevisedStep).
	RevisedStep = ingest.RevisedStep
	// RevisedNote is an author note of a revision (ingest.RevisedNote).
	RevisedNote = ingest.RevisedNote
	// RevisedEquipment is equipment of a revision (ingest.RevisedEquipment).
	RevisedEquipment = ingest.RevisedEquipment
	// DraftSummary is a pending draft as an agent lists it (store.DraftSummary).
	DraftSummary = store.DraftSummary
	// EndedDraft is a draft just confirmed or discarded (store.EndedDraft,
	// ADR-00049).
	EndedDraft = store.EndedDraft
	// SourcedRecipe is a recipe named by the address it was imported from
	// (store.SourcedRecipe).
	SourcedRecipe = store.SourcedRecipe
)

// SameSource is what the household already holds from one source address: its
// saved recipes and its pending drafts, re-imports included, whose address is
// the same once fragments are dropped (store.SourceAddress, ADR-00048).
type SameSource struct {
	Recipes []SourcedRecipe
	Drafts  []DraftSummary
}

// Reaper finds and deletes the media objects of a recipe and of the drafts
// that name it (picture.Reaper). With it the service removes a deleted
// recipe's picture, and the candidates of a re-import that a new proposal or
// the deletion ends (ADR-00039, ADR-00042).
type Reaper interface {
	store.PictureReaper
	store.ObjectRemover
}

// NewStore returns the recipe store over d, which must already be migrated.
// The router builds it here so that it, too, names no domain package.
func NewStore(d *sql.DB) *Store { return store.New(d) }

// IdleOrder orders cards by how long ago they were last made, never-made
// first (store.IdleOrder).
func IdleOrder(cards []Card) []Card { return store.IdleOrder(cards) }

// MediaReaper is the picture package's reaper (picture.Reaper): it finds and
// deletes the media objects of a recipe and, for the household delete, of a
// household.
type MediaReaper = picture.Reaper

// NewReaper returns a reaper over the media store. log may be nil.
func NewReaper(m media.Store, log *slog.Logger) *MediaReaper { return picture.NewReaper(m, log) }

// NewPictures returns the picture service over d and the media store m, which
// reads recipe sources from rec. allowLoopback lets candidate downloads reach
// a loopback address, for the dev bypass only; userAgent is what they send
// (picture.New, picture.NewFetcher).
func NewPictures(d *sql.DB, m media.Store, rec *Store, allowLoopback bool, userAgent string, log *slog.Logger) *Pictures {
	return picture.New(d, m, rec, picture.NewFetcher(allowLoopback), log).WithUserAgent(userAgent)
}
