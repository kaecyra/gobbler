package recipes

import (
	"database/sql"

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

// Card, MadeEntry, MadeIt, Note, Info, Summary, Revision, Thumbnail,
// SearchQuery, Proposal, ProposalRef and Provenance are the store's values.
type (
	Card        = store.Card
	MadeEntry   = store.MadeEntry
	MadeIt      = store.MadeIt
	Note        = store.Note
	Info        = store.Info
	Summary     = store.Summary
	Revision    = store.Revision
	Thumbnail   = store.Thumbnail
	SearchQuery = store.SearchQuery
	Proposal    = store.Proposal
	ProposalRef = store.ProposalRef
	Provenance  = store.Provenance
	Credential  = store.Credential
	DraftInput  = store.DraftInput
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
)

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
