package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// A proposal is a whole document awaiting a decision, stored outside the live
// recipe tables (ADR-00035): an ingestion draft of a new recipe, or a proposed
// new version of an existing one. No query over live recipes, history or search
// can see one. The one duty the document tables share with recipes is keeping
// their catalog ingredient ids valid, which RegisterMergeHooks covers.

// ProposalKind says what a proposal proposes.
type ProposalKind string

// Proposal kinds.
const (
	// ProposalDraft is an ingestion draft: of a new recipe, or, when its
	// document carries a "reimport" marker, a re-import that replaces a saved
	// recipe (ADR-00042; see reimport.go). Its document is ingestion's own and
	// opaque here, except for the ingredient ids it names and that marker (see
	// proposal_doc.go). It carries a source snapshot.
	ProposalDraft ProposalKind = "draft"
	// ProposalEdit is a proposed new version of an existing recipe, made
	// against one version of it. Its document is the revision document format.
	ProposalEdit ProposalKind = "edit"
)

// ProposalState is where a proposal is in its life. Only a pending proposal
// changes state.
type ProposalState string

// Proposal states.
const (
	ProposalPending    ProposalState = "pending"
	ProposalAccepted   ProposalState = "accepted"
	ProposalRejected   ProposalState = "rejected"
	ProposalSuperseded ProposalState = "superseded"
	ProposalExpired    ProposalState = "expired"
)

// Bounds on proposals (ADR-00035).
const (
	// MaxPendingDrafts is how many drafts of new recipes one household may
	// have waiting. A draft past it is refused, never traded for an old one.
	MaxPendingDrafts = 20
	// PendingLifetime is how long a proposal may stay pending.
	PendingLifetime = 365 * 24 * time.Hour
	// FinishedLifetime is how long a rejected, superseded or expired proposal
	// is kept after it reached that state. An accepted one is never pruned.
	FinishedLifetime = 60 * 24 * time.Hour
)

var (
	// ErrDraftLimit is returned by ProposeDraft when the household already has
	// MaxPendingDrafts drafts waiting. Its text is shown to the person or
	// agent that asked.
	ErrDraftLimit = fmt.Errorf("%d drafts are waiting for review; confirm or discard some before adding another", MaxPendingDrafts)
	// ErrNotPending is returned when a proposal that is no longer pending (it
	// was confirmed, rejected, superseded or expired) is asked to change.
	ErrNotPending = errors.New("the proposal is no longer pending")
	// ErrSnapshotAttached is returned when a source snapshot that already
	// belongs to a recipe is attached or deleted.
	ErrSnapshotAttached = errors.New("source snapshot already belongs to a recipe")
)

// Credential is the snapshot of the credential that made a proposal, taken as
// the credential write logs take it (migration 00026): a revoked credential's
// id is given to a later one, so the id alone does not name who proposed.
type Credential struct {
	// Kind is the credential's kind ("api_key", "oauth_grant") and ID names it
	// among that kind's credentials.
	Kind string
	ID   int64
	// Ref is a key's prefix or a grant's client id, and Owner the email that
	// owns it, or "household:<id>" for a household key.
	Ref       string
	Owner     string
	CreatedAt time.Time
}

// Provenance says who proposed a document and how.
type Provenance struct {
	// Person is who the proposal is attributed to: the signed-in person, or
	// the owner of the credential an agent used.
	Person string
	// Credential is the credential that made it; nil when a person did.
	Credential *Credential
	// Tool is the MCP tool that made it; empty when a person did.
	Tool string
	// Reason is what an agent said about it; empty if it said nothing.
	Reason string
}

// Proposal is one stored proposal.
type Proposal struct {
	// ID is the proposal's row id. The draft's picture candidates are kept
	// under it (ADR-00039); the token is what a person or agent holds.
	ID int64
	// Token names the proposal on the review screens. It cannot be guessed.
	Token string
	Kind  ProposalKind
	State ProposalState
	// RecipeID and BaseVersion are the recipe an edit changes and the version
	// it was made against; zero for a draft.
	RecipeID, BaseVersion int64
	// ResultRecipeID is the recipe a confirmed draft created; zero otherwise.
	ResultRecipeID int64
	// Document is the stored document: a draft's, opaque to the store apart from
	// the ingredient ids and the re-import marker it names, or an edit's revision
	// document.
	Document []byte
	// Edit is the recipe an edit proposes; nil for a draft.
	Edit *recipe.Recipe
	// Reimport is set for a draft that replaces a saved recipe instead of
	// creating one (ADR-00042); nil for a draft of a new recipe and for an edit.
	Reimport *Reimport
	// Snapshot is a draft's source; nil for an edit, and for a draft whose
	// snapshot is gone.
	Snapshot *Snapshot
	Provenance
	CreatedAt, StateAt time.Time
}

// DraftInput is a draft to hold for review.
type DraftInput struct {
	// Document is the draft in ingestion's format. Its source's content is not
	// in it: Source is.
	Document []byte
	// Source is the input the draft was read from, stored as an unattached
	// source snapshot that confirming attaches to the new recipe.
	Source SnapshotInput
	Provenance
}

func newToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate proposal token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func (p Provenance) validate() error {
	if p.Person == "" {
		return fmt.Errorf("%w: a proposal needs the person it is attributed to", ErrInvalid)
	}
	return nil
}

// ProposeDraft stores a draft of a new recipe as a pending proposal, with its
// source as a snapshot that has no recipe yet, and returns its token. A
// household holds at most MaxPendingDrafts pending drafts: the next is
// ErrDraftLimit, and nothing is dropped to make room. Edit proposals are not
// counted, nor are re-imports.
//
// A document with a "reimport" marker is a re-import of a saved recipe
// (ADR-00042; see reimport.go). It is not bounded; it is refused with
// ErrStaleVersion when the recipe is not at the marker's version, and it
// supersedes the recipe's pending edit and pending re-import, as ProposeEdit
// does.
func (s *Store) ProposeDraft(ctx context.Context, in DraftInput) (string, error) {
	hid, err := scope(ctx)
	if err != nil {
		return "", err
	}
	if err := in.validate(); err != nil {
		return "", err
	}
	if !in.Source.Kind.Valid() {
		return "", fmt.Errorf("%w: snapshot kind %q", ErrInvalid, in.Source.Kind)
	}
	if !json.Valid(in.Document) {
		return "", fmt.Errorf("%w: the draft document is not JSON", ErrInvalid)
	}
	ids, err := draftIngredientIDs(in.Document)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	re, err := draftReimport(in.Document)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	token, err := newToken()
	if err != nil {
		return "", err
	}
	var keys []string
	err = s.withTx(ctx, func(q *storedb.Queries) error {
		now := s.stamp()
		if re == nil {
			held, err := q.CountPendingDrafts(ctx, hid)
			if err != nil {
				return readErr("pending drafts", err)
			}
			if held >= MaxPendingDrafts {
				return ErrDraftLimit
			}
		} else {
			var err error
			if keys, err = s.supersedeForReimport(ctx, q, hid, *re, now); err != nil {
				return err
			}
		}
		sum := sha256.Sum256(in.Source.Content)
		snapID, err := q.InsertUnattachedSnapshot(ctx, storedb.InsertUnattachedSnapshotParams{
			HouseholdID: hid, Kind: string(in.Source.Kind), Content: in.Source.Content, Sha256: hex.EncodeToString(sum[:]),
			CreatedAt: now, CreatedBy: in.Person, ParserVersion: in.Source.ParserVersion,
		})
		if err != nil {
			return fmt.Errorf("write %s snapshot of draft: %w", in.Source.Kind, err)
		}
		return s.insertProposal(ctx, q, hid, token, ProposalDraft, 0, 0, sql.NullInt64{Int64: snapID, Valid: true}, in.Document, ids, in.Provenance, now)
	})
	if err != nil {
		return "", err
	}
	s.removeObjects(ctx, keys)
	return token, nil
}

// supersedeForReimport checks that the recipe is at the version the re-import
// started from and ends what was waiting to change it.
func (s *Store) supersedeForReimport(ctx context.Context, q *storedb.Queries, hid int64, re Reimport, now string) ([]string, error) {
	if err := lockRecipe(ctx, q, hid, re.RecipeID); err != nil {
		return nil, err
	}
	cur, err := loadInfo(ctx, q, hid, re.RecipeID)
	if err != nil {
		return nil, err
	}
	if cur.Version != re.BaseVersion {
		return nil, fmt.Errorf("%w: recipe %d is at version %d, not %d", ErrStaleVersion, re.RecipeID, cur.Version, re.BaseVersion)
	}
	return s.endPendingProposals(ctx, q, hid, re.RecipeID, now)
}

// PendingDrafts is how many drafts of new recipes the household on ctx has
// waiting for review. It lets a caller refuse early, before spending work on a
// draft that ProposeDraft would turn away; ProposeDraft's own check, made in the
// transaction that writes the draft, is the authority.
func (s *Store) PendingDrafts(ctx context.Context) (int, error) {
	hid, err := scope(ctx)
	if err != nil {
		return 0, err
	}
	var n int64
	err = s.read(ctx, func(q *storedb.Queries) error {
		var err error
		n, err = q.CountPendingDrafts(ctx, hid)
		return err
	})
	if err != nil {
		return 0, readErr("pending drafts", err)
	}
	return int(n), nil
}

// ProposeEdit stores r as a pending proposal to replace recipe id, made
// against baseVersion, and returns its token. A recipe has at most one pending
// proposal: the one it had, an edit or a re-import, is kept with its state set
// to superseded.
// A baseVersion that is not the recipe's current version is ErrStaleVersion
// and writes nothing. r is validated as a whole recipe; a new step carries a
// document-local label, as in any other write (recipe.StepID).
func (s *Store) ProposeEdit(ctx context.Context, id, baseVersion int64, r recipe.Recipe, by Provenance) (string, error) {
	hid, err := scope(ctx)
	if err != nil {
		return "", err
	}
	if err := by.validate(); err != nil {
		return "", err
	}
	if err := r.Validate(); err != nil {
		return "", fmt.Errorf("validate recipe: %w", err)
	}
	token, err := newToken()
	if err != nil {
		return "", err
	}
	var keys []string
	err = s.withTx(ctx, func(q *storedb.Queries) error {
		if err := lockRecipe(ctx, q, hid, id); err != nil {
			return err
		}
		cur, err := loadInfo(ctx, q, hid, id)
		if err != nil {
			return err
		}
		if cur.Version != baseVersion {
			return fmt.Errorf("%w: recipe %d is at version %d, not %d", ErrStaleVersion, id, cur.Version, baseVersion)
		}
		units, err := loadUnits(ctx, q)
		if err != nil {
			return err
		}
		body, err := encodeDocument(r, units)
		if err != nil {
			return fmt.Errorf("proposal for recipe %d: %w", id, err)
		}
		d, err := decodeDocument(body)
		if err != nil {
			return err
		}
		now := s.stamp()
		if keys, err = s.endPendingProposals(ctx, q, hid, id, now); err != nil {
			return err
		}
		return s.insertProposal(ctx, q, hid, token, ProposalEdit, id, baseVersion, sql.NullInt64{}, body, d.ingredientIDs(), by, now)
	})
	if err != nil {
		return "", err
	}
	s.removeObjects(ctx, keys)
	return token, nil
}

func (s *Store) insertProposal(ctx context.Context, q *storedb.Queries, hid int64, token string, kind ProposalKind, recipeID, baseVersion int64, snapshot sql.NullInt64, doc []byte, ingredients []int64, by Provenance, now string) error {
	if err := checkIngredientsVisible(ctx, q, hid, ingredients); err != nil {
		return err
	}
	var cred Credential
	if by.Credential != nil {
		cred = *by.Credential
	}
	var credCreated string
	if !cred.CreatedAt.IsZero() {
		credCreated = formatTime(cred.CreatedAt)
	}
	p := storedb.InsertProposalParams{
		HouseholdID: hid, Token: token, Kind: string(kind), SnapshotID: snapshot, Document: string(doc), Reason: by.Reason,
		ProposedBy: by.Person, CredentialKind: cred.Kind, CredentialID: cred.ID, CredentialRef: cred.Ref,
		CredentialOwner: cred.Owner, CredentialCreatedAt: credCreated, Tool: by.Tool, CreatedAt: now, StateAt: now,
	}
	if recipeID != 0 {
		p.RecipeID, p.BaseVersion = nullID(recipeID), nullID(baseVersion)
	}
	id, err := q.InsertProposal(ctx, p)
	if err != nil {
		return wrapWrite("write proposal", err)
	}
	return indexProposal(ctx, q, hid, id, ingredients)
}

func indexProposal(ctx context.Context, q *storedb.Queries, hid, id int64, ingredients []int64) error {
	for _, ing := range ingredients {
		err := q.InsertProposalIngredient(ctx, storedb.InsertProposalIngredientParams{HouseholdID: hid, ProposalID: id, IngredientID: ing})
		if err != nil {
			return wrapWrite(fmt.Sprintf("index ingredient %d of proposal %d", ing, id), err)
		}
	}
	return nil
}

// checkIngredientsVisible refuses a document that names an ingredient the
// household cannot use, as writing it as a recipe line would (ADR-00018).
func checkIngredientsVisible(ctx context.Context, q *storedb.Queries, hid int64, ids []int64) error {
	for _, ing := range ids {
		ok, err := q.IngredientVisible(ctx, storedb.IngredientVisibleParams{ID: ing, Scope: sql.NullInt64{Int64: hid, Valid: true}})
		if err != nil {
			return readErr(fmt.Sprintf("ingredient %d", ing), err)
		}
		if !ok {
			return fmt.Errorf("write proposal (ingredient id %d): %w", ing, ErrUnknownReference)
		}
	}
	return nil
}

// GetProposal returns the proposal with token, in whatever state it is in, or
// ErrNotFound. A proposal of another household is ErrNotFound too.
func (s *Store) GetProposal(ctx context.Context, token string) (Proposal, error) {
	hid, err := scope(ctx)
	if err != nil {
		return Proposal{}, err
	}
	var p Proposal
	err = s.read(ctx, func(q *storedb.Queries) error {
		var err error
		p, err = loadProposal(ctx, q, hid, token)
		return err
	})
	if err != nil {
		return Proposal{}, err
	}
	return p, nil
}

// PendingEdit returns the pending proposal to change recipe id, if it has one.
func (s *Store) PendingEdit(ctx context.Context, id int64) (Proposal, bool, error) {
	hid, err := scope(ctx)
	if err != nil {
		return Proposal{}, false, err
	}
	var p Proposal
	var found bool
	err = s.read(ctx, func(q *storedb.Queries) error {
		token, err := q.GetPendingEditProposal(ctx, storedb.GetPendingEditProposalParams{RecipeID: nullID(id), HouseholdID: hid})
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return readErr("pending proposal", err)
		}
		found = true
		p, err = loadProposal(ctx, q, hid, token)
		return err
	})
	return p, found, err
}

func loadProposal(ctx context.Context, q *storedb.Queries, hid int64, token string) (Proposal, error) {
	row, err := q.GetProposal(ctx, storedb.GetProposalParams{Token: token, HouseholdID: hid})
	if errors.Is(err, sql.ErrNoRows) {
		return Proposal{}, fmt.Errorf("%w: proposal", ErrNotFound)
	}
	if err != nil {
		return Proposal{}, readErr("proposal", err)
	}
	p := Proposal{
		ID: row.ID, Token: row.Token, Kind: ProposalKind(row.Kind), State: ProposalState(row.State),
		RecipeID: row.RecipeID.Int64, BaseVersion: row.BaseVersion.Int64, ResultRecipeID: row.ResultRecipeID.Int64,
		Document:   []byte(row.Document),
		Provenance: Provenance{Person: row.ProposedBy, Tool: row.Tool, Reason: row.Reason},
	}
	if p.CreatedAt, err = parseTime(row.CreatedAt); err != nil {
		return Proposal{}, err
	}
	if p.StateAt, err = parseTime(row.StateAt); err != nil {
		return Proposal{}, err
	}
	if row.CredentialKind != "" {
		c := &Credential{Kind: row.CredentialKind, ID: row.CredentialID, Ref: row.CredentialRef, Owner: row.CredentialOwner}
		if row.CredentialCreatedAt != "" {
			if c.CreatedAt, err = parseTime(row.CredentialCreatedAt); err != nil {
				return Proposal{}, err
			}
		}
		p.Credential = c
	}
	if p.Kind == ProposalDraft {
		if p.Reimport, err = draftReimport(p.Document); err != nil {
			return Proposal{}, fmt.Errorf("%w: proposal %s: %w", ErrCorrupt, token, err)
		}
	}
	if p.Kind == ProposalEdit {
		units, err := loadUnits(ctx, q)
		if err != nil {
			return Proposal{}, err
		}
		d, err := decodeDocument(p.Document)
		if err != nil {
			return Proposal{}, fmt.Errorf("proposal for recipe %d: %w", p.RecipeID, err)
		}
		r, err := d.toRecipe(units)
		if err != nil {
			return Proposal{}, fmt.Errorf("proposal for recipe %d: %w", p.RecipeID, err)
		}
		p.Edit = &r
	}
	if row.SnapshotID.Valid {
		snap, err := q.GetSnapshotByID(ctx, storedb.GetSnapshotByIDParams{ID: row.SnapshotID.Int64, HouseholdID: hid})
		if err != nil {
			return Proposal{}, readErr(fmt.Sprintf("snapshot of proposal %s", token), err)
		}
		sn := Snapshot{Kind: SnapshotKind(snap.Kind), Content: snap.Content, SHA256: snap.Sha256, CreatedBy: snap.CreatedBy, ParserVersion: snap.ParserVersion}
		if sn.CreatedAt, err = parseTime(snap.CreatedAt); err != nil {
			return Proposal{}, err
		}
		p.Snapshot = &sn
	}
	return p, nil
}

// ReplaceDraft stores doc as the document of the pending draft with token: the
// draft as the reviewer left it, or as the LLM read it again. The source
// snapshot does not change. A draft that is not pending is ErrNotPending.
func (s *Store) ReplaceDraft(ctx context.Context, token string, doc []byte) error {
	return s.replaceDraft(ctx, token, doc, "", sql.NullString{})
}

// ReviseDraft is ReplaceDraft for an agent's revision (ADR-00048): it stores
// doc only while the held document's DraftRevision is still revision, and
// stores reason on the proposal with it. A draft whose document changed since
// the agent read it is ErrStaleVersion and is left as it is, so an agent never
// overwrites a person's edit.
func (s *Store) ReviseDraft(ctx context.Context, token string, doc []byte, revision, reason string) error {
	if revision == "" {
		return fmt.Errorf("%w: a revision needs the draft revision it was made from", ErrInvalid)
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: a revision needs a reason", ErrInvalid)
	}
	return s.replaceDraft(ctx, token, doc, revision, sql.NullString{String: reason, Valid: true})
}

// DraftRevision names the content of a held draft's document. It changes
// whenever the document does, and nothing is stored for it (ADR-00048).
func DraftRevision(doc []byte) string {
	sum := sha256.Sum256(doc)
	return hex.EncodeToString(sum[:16])
}

// replaceDraft is the one replace of a draft's document. A non-empty expect is
// the DraftRevision the held document must still have; a valid reason replaces
// the proposal's reason, and a null one keeps it.
func (s *Store) replaceDraft(ctx context.Context, token string, doc []byte, expect string, reason sql.NullString) error {
	hid, err := scope(ctx)
	if err != nil {
		return err
	}
	if !json.Valid(doc) {
		return fmt.Errorf("%w: the draft document is not JSON", ErrInvalid)
	}
	ids, err := draftIngredientIDs(doc)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	replacing, err := draftReimport(doc)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return s.withTx(ctx, func(q *storedb.Queries) error {
		row, err := q.GetProposal(ctx, storedb.GetProposalParams{Token: token, HouseholdID: hid})
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: proposal", ErrNotFound)
		}
		if err != nil {
			return readErr("proposal", err)
		}
		if ProposalKind(row.Kind) != ProposalDraft {
			return fmt.Errorf("%w: proposal is not a draft", ErrInvalid)
		}
		if expect != "" && DraftRevision([]byte(row.Document)) != expect {
			return fmt.Errorf("%w: the draft changed since revision %s was read", ErrStaleVersion, expect)
		}
		held, err := draftReimport([]byte(row.Document))
		if err != nil {
			return fmt.Errorf("%w: %w", ErrCorrupt, err)
		}
		if !sameReimport(held, replacing) {
			return fmt.Errorf("%w: a draft cannot change the recipe it re-imports", ErrInvalid)
		}
		n, err := q.UpdateDraftDocument(ctx, storedb.UpdateDraftDocumentParams{Document: string(doc), Reason: reason, ID: row.ID, HouseholdID: hid})
		if err != nil {
			return fmt.Errorf("replace draft: %w", err)
		}
		if n == 0 {
			return ErrNotPending
		}
		if err := checkIngredientsVisible(ctx, q, hid, ids); err != nil {
			return err
		}
		if err := q.DeleteProposalIngredients(ctx, row.ID); err != nil {
			return fmt.Errorf("replace draft index: %w", err)
		}
		return indexProposal(ctx, q, hid, row.ID, ids)
	})
}

// DraftSummary is a pending draft as an agent lists it (ADR-00048).
type DraftSummary struct {
	Token string
	// Name and SourceURL are the draft's name and the address it was read
	// from; either may be empty.
	Name, SourceURL string
	// Reimport is set for a re-import of a saved recipe (ADR-00042).
	Reimport bool
	// Reason is what the agent that last revised the draft said; empty when no
	// agent has revised it (ReviseDraft is the only write of a draft's reason).
	Reason    string
	CreatedAt time.Time
}

// PendingDraftList lists the household's pending ingestion drafts, re-imports
// included, oldest first.
func (s *Store) PendingDraftList(ctx context.Context) ([]DraftSummary, error) {
	hid, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	var out []DraftSummary
	err = s.read(ctx, func(q *storedb.Queries) error {
		rows, err := q.ListPendingDrafts(ctx, hid)
		if err != nil {
			return readErr("pending drafts", err)
		}
		for _, r := range rows {
			at, err := parseTime(r.CreatedAt)
			if err != nil {
				return err
			}
			out = append(out, DraftSummary{Token: r.Token, Name: r.Name, SourceURL: r.SourceUrl, Reimport: r.Reimport != 0, Reason: r.Reason, CreatedAt: at})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func sameReimport(a, b *Reimport) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// RejectProposal turns the pending proposal with token down. It is kept for
// FinishedLifetime, with its state set to rejected. A proposal that is not
// pending is ErrNotPending.
func (s *Store) RejectProposal(ctx context.Context, token string) error {
	hid, err := scope(ctx)
	if err != nil {
		return err
	}
	return s.withTx(ctx, func(q *storedb.Queries) error {
		row, err := q.GetProposal(ctx, storedb.GetProposalParams{Token: token, HouseholdID: hid})
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: proposal", ErrNotFound)
		}
		if err != nil {
			return readErr("proposal", err)
		}
		n, err := q.SetProposalState(ctx, storedb.SetProposalStateParams{State: string(ProposalRejected), StateAt: s.stamp(), ID: row.ID, HouseholdID: hid})
		if err != nil {
			return fmt.Errorf("reject proposal: %w", err)
		}
		if n == 0 {
			return ErrNotPending
		}
		return nil
	})
}

// CreateFromDraft stores r as a new recipe at version 1 and marks the pending
// draft with token accepted, recording the recipe on it. The draft's source
// snapshot is attached to the new recipe in the same transaction: either all of
// it happens or none. A draft that is not pending, so one confirmed twice or
// racing itself, is ErrNotPending and writes nothing.
func (s *Store) CreateFromDraft(ctx context.Context, token string, r recipe.Recipe) (int64, error) {
	who, err := identity(ctx)
	if err != nil {
		return 0, err
	}
	hid, err := scope(ctx)
	if err != nil {
		return 0, err
	}
	if err := r.Validate(); err != nil {
		return 0, fmt.Errorf("validate recipe: %w", err)
	}
	var id int64
	err = s.withTx(ctx, func(q *storedb.Queries) error {
		row, err := q.GetProposal(ctx, storedb.GetProposalParams{Token: token, HouseholdID: hid})
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: proposal", ErrNotFound)
		}
		if err != nil {
			return readErr("proposal", err)
		}
		if ProposalKind(row.Kind) != ProposalDraft {
			return fmt.Errorf("%w: proposal is not a draft of a new recipe", ErrInvalid)
		}
		if re, err := draftReimport([]byte(row.Document)); err != nil {
			return fmt.Errorf("%w: %w", ErrCorrupt, err)
		} else if re != nil {
			return fmt.Errorf("%w: proposal is a re-import, saved with AcceptReimport", ErrInvalid)
		}
		if ProposalState(row.State) != ProposalPending {
			return ErrNotPending
		}
		if id, err = s.insertRecipe(ctx, q, hid, r, nil, who); err != nil {
			return err
		}
		// A recipe never exists without its source (Saver.Save): a draft with no
		// snapshot is a broken row, not a draft to confirm without one.
		if !row.SnapshotID.Valid {
			return fmt.Errorf("%w: draft has no source snapshot to attach", ErrCorrupt)
		}
		if err := attachSnapshot(ctx, q, hid, row.SnapshotID.Int64, id); err != nil {
			return err
		}
		n, err := q.AcceptDraft(ctx, storedb.AcceptDraftParams{ResultRecipeID: nullID(id), StateAt: s.stamp(), ID: row.ID, HouseholdID: hid})
		if err != nil {
			return fmt.Errorf("accept draft: %w", err)
		}
		if n != 1 {
			return ErrNotPending
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// attachSnapshot gives the snapshot with id, which must have no recipe, to
// recipe recipeID. It is done once: a snapshot that already belongs to a recipe
// is ErrSnapshotAttached and is left as it was. Nothing here touches the
// snapshot's content.
func attachSnapshot(ctx context.Context, q *storedb.Queries, hid, id, recipeID int64) error {
	n, err := q.AttachSnapshot(ctx, storedb.AttachSnapshotParams{RecipeID: nullID(recipeID), ID: id, HouseholdID: hid})
	if err != nil {
		return wrapWrite(fmt.Sprintf("attach snapshot %d to recipe %d", id, recipeID), err)
	}
	if n != 1 {
		return fmt.Errorf("%w: snapshot %d", ErrSnapshotAttached, id)
	}
	return nil
}

// deleteUnattachedSnapshot removes the snapshot with id of household hid if no
// recipe holds it.
// One that does is ErrSnapshotAttached and stays: it goes only with its recipe.
func deleteUnattachedSnapshot(ctx context.Context, q *storedb.Queries, hid, id int64) error {
	n, err := q.DeleteUnattachedSnapshot(ctx, storedb.DeleteUnattachedSnapshotParams{ID: id, HouseholdID: hid})
	if err != nil {
		return fmt.Errorf("delete snapshot %d: %w", id, err)
	}
	if n != 1 {
		return fmt.Errorf("%w: snapshot %d", ErrSnapshotAttached, id)
	}
	return nil
}

// SweepResult says what Sweep changed.
type SweepResult struct {
	// Expired proposals were pending for PendingLifetime.
	Expired int
	// Deleted proposals had been rejected, superseded or expired for
	// FinishedLifetime; their unattached snapshots went with them.
	Deleted int
}

// Sweep ends the life of the proposals that have outlived it, for every
// household: a pending proposal PendingLifetime after it was made becomes
// expired (as of that moment, whenever the sweep runs), and a rejected,
// superseded or expired one FinishedLifetime after it reached that state is
// deleted with the source snapshot no recipe holds. An accepted proposal is
// never touched. It is idempotent and meant to run at start-up and then
// periodically.
func (s *Store) Sweep(ctx context.Context) (SweepResult, error) {
	return s.SweepWithPictures(ctx, nil)
}

// ObjectRemover deletes media objects by key once a delete has committed.
// *picture.Reaper satisfies it; this package cannot import that one.
type ObjectRemover interface {
	Remove(ctx context.Context, keys []string)
}

// SweepWithPictures is Sweep that also deletes the picture candidates of the
// drafts it prunes (ADR-00039): their rows go with the proposal, so the keys
// are read in the sweep's transaction before the delete and the objects removed
// after it commits. A rolled-back sweep keeps its objects. r nil leaves them.
func (s *Store) SweepWithPictures(ctx context.Context, r ObjectRemover) (SweepResult, error) {
	var res SweepResult
	var keys []string
	err := s.withTx(ctx, func(q *storedb.Queries) error {
		now := s.now()
		pending, err := q.ListPendingProposalAges(ctx)
		if err != nil {
			return readErr("pending proposals", err)
		}
		for _, p := range pending {
			made, err := parseTime(p.CreatedAt)
			if err != nil {
				return err
			}
			if due := made.Add(PendingLifetime); !now.Before(due) {
				if _, err := q.ExpireProposal(ctx, storedb.ExpireProposalParams{StateAt: formatTime(due), ID: p.ID}); err != nil {
					return fmt.Errorf("expire proposal %d: %w", p.ID, err)
				}
				res.Expired++
			}
		}
		finished, err := q.ListFinishedProposals(ctx)
		if err != nil {
			return readErr("finished proposals", err)
		}
		for _, p := range finished {
			at, err := parseTime(p.StateAt)
			if err != nil {
				return err
			}
			if now.Before(at.Add(FinishedLifetime)) {
				continue
			}
			if r != nil {
				// Read from draft_candidates here rather than through
				// internal/picture, which imports this package.
				ks, err := q.ListProposalCandidateKeys(ctx, storedb.ListProposalCandidateKeysParams{HouseholdID: p.HouseholdID, ProposalID: p.ID})
				if err != nil {
					return readErr(fmt.Sprintf("candidates of proposal %d", p.ID), err)
				}
				keys = append(keys, ks...)
			}
			if err := q.DeleteProposal(ctx, p.ID); err != nil {
				return fmt.Errorf("delete proposal %d: %w", p.ID, err)
			}
			if p.SnapshotID.Valid {
				// A snapshot a recipe holds stays: it goes with the recipe. A
				// finished proposal that still names one cannot normally
				// happen, and deleting the proposal is right either way.
				if err := deleteUnattachedSnapshot(ctx, q, p.HouseholdID, p.SnapshotID.Int64); err != nil && !errors.Is(err, ErrSnapshotAttached) {
					return err
				}
			}
			res.Deleted++
		}
		return nil
	})
	if err != nil {
		return res, err
	}
	if r != nil {
		r.Remove(ctx, keys)
	}
	return res, nil
}
