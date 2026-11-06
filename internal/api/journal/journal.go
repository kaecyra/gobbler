// Package journal is the journal service: every operation on a recipe's notes,
// its rating and its made-it log, written once, for the web handlers to call
// (ADR-00043). It is in-process. It is not a network API, it speaks neither
// HTTP nor JSON and it reads no cookie or header. It follows the pattern
// internal/api/shopping set: a method takes the context, then the
// caller.Caller, then its own arguments; it refuses a Caller that is not a
// member of the household with ErrNotMember before touching a row
// (ADR-00018); and it returns the domain's errors, which the web turns into
// statuses in internal/web/layout (statusTable).
//
// The journal has no domain package of its own: its rows live in the recipe
// store (ADR-00004), which this service is built over. The package re-exports
// the store values and errors its callers need as aliases, so internal/web
// imports this package and not internal/store; the recipes-through-api rule in
// .golangci.yml fails make lint when it does.
package journal

import (
	"context"
	"errors"
	"fmt"

	"github.com/kaecyra/gobbler/internal/api/caller"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// Caller is who an operation is for (caller.Caller).
type Caller = caller.Caller

// Members is the membership lookup the service enforces (caller.Members).
type Members = caller.Members

// ErrNotMember is returned for a Caller that is not a verified member of a
// household (caller.ErrNotMember).
var ErrNotMember = caller.ErrNotMember

// The store values the callers of the service work with.
type (
	// Note is a note on a recipe (store.Note).
	Note = store.Note
	// MadeIt is a made-it entry of a recipe (store.MadeIt).
	MadeIt = store.MadeIt
)

// The errors a service method returns, for callers to branch on with
// errors.Is.
var (
	ErrNotFound = store.ErrNotFound
	ErrInvalid  = store.ErrInvalid
)

// Recipes is what the service uses from the recipe store. *store.Store
// satisfies it. The rating lives on the recipe, so setting it is a Get then an
// Update, which writes a revision.
type Recipes interface {
	Get(ctx context.Context, id int64) (recipe.Recipe, error)
	Update(ctx context.Context, id int64, r recipe.Recipe) error
	AddNote(ctx context.Context, id int64, body string, pinned bool) (store.Note, error)
	ListNotes(ctx context.Context, id int64) ([]store.Note, error)
	SetNotePinned(ctx context.Context, id, noteID int64, pinned bool) error
	UpdateNoteBody(ctx context.Context, id, noteID int64, body string) error
	DeleteNote(ctx context.Context, id, noteID int64) error
	AddMadeIt(ctx context.Context, id int64, e store.MadeIt) (store.MadeIt, error)
	ListMadeIt(ctx context.Context, id int64) ([]store.MadeIt, error)
}

// Deps is what the service is built from.
type Deps struct {
	// Recipes is the recipe store. Required.
	Recipes Recipes
	// Members is the membership lookup. Required.
	Members Members
}

// Service is the journal operations. It is safe for concurrent use.
type Service struct {
	recipes Recipes
	members Members
}

// New returns a Service. Recipes and Members are required; a missing one is an
// error here, not a nil dereference on the first request.
func New(d Deps) (*Service, error) {
	if d.Recipes == nil || d.Members == nil {
		return nil, errors.New("journal service: recipes and members are required")
	}
	return &Service{recipes: d.Recipes, members: d.Members}, nil
}

// enter checks the Caller and returns a context that runs in its household
// (caller.Enter). It is the first line of every method.
func (s *Service) enter(ctx context.Context, c Caller) (context.Context, error) {
	return caller.Enter(ctx, s.members, c)
}

// Recipe returns recipe id, which the journal shows the name and rating of, or
// ErrNotFound.
func (s *Service) Recipe(ctx context.Context, c Caller, id int64) (recipe.Recipe, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return recipe.Recipe{}, err
	}
	return s.recipes.Get(ctx, id)
}

// ListNotes returns recipe id's notes.
func (s *Service) ListNotes(ctx context.Context, c Caller, id int64) ([]Note, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.recipes.ListNotes(ctx, id)
}

// ListMadeIt returns recipe id's made-it entries.
func (s *Service) ListMadeIt(ctx context.Context, c Caller, id int64) ([]MadeIt, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.recipes.ListMadeIt(ctx, id)
}

// AddNote adds a note to recipe id, pinned as a tip when pinned is set. An
// empty body is ErrInvalid.
func (s *Service) AddNote(ctx context.Context, c Caller, id int64, body string, pinned bool) (Note, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return Note{}, err
	}
	return s.recipes.AddNote(ctx, id, body, pinned)
}

// SetNotePinned pins note noteID of recipe id as a tip, or unpins it. A note
// that is not one of the recipe's is ErrNotFound.
func (s *Service) SetNotePinned(ctx context.Context, c Caller, id, noteID int64, pinned bool) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	return s.recipes.SetNotePinned(ctx, id, noteID, pinned)
}

// UpdateNoteBody replaces the body of note noteID of recipe id. An empty body
// is ErrInvalid, and a note that is not one of the recipe's is ErrNotFound.
func (s *Service) UpdateNoteBody(ctx context.Context, c Caller, id, noteID int64, body string) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	return s.recipes.UpdateNoteBody(ctx, id, noteID, body)
}

// DeleteNote removes note noteID of recipe id. A note that is not one of the
// recipe's is ErrNotFound.
func (s *Service) DeleteNote(ctx context.Context, c Caller, id, noteID int64) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	return s.recipes.DeleteNote(ctx, id, noteID)
}

// AddMadeIt logs that recipe id was made. A zero On means today.
func (s *Service) AddMadeIt(ctx context.Context, c Caller, id int64, e MadeIt) (MadeIt, error) {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return MadeIt{}, err
	}
	return s.recipes.AddMadeIt(ctx, id, e)
}

// SetRating sets recipe id's rating, or clears it when rating is nil. Update
// is the store's one way to change the recipe, so this writes a revision.
func (s *Service) SetRating(ctx context.Context, c Caller, id int64, rating *int) error {
	ctx, err := s.enter(ctx, c)
	if err != nil {
		return err
	}
	rec, err := s.recipes.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("load recipe %d: %w", id, err)
	}
	rec.Rating = rating
	if err := s.recipes.Update(ctx, id, rec); err != nil {
		return fmt.Errorf("rate recipe %d: %w", id, err)
	}
	return nil
}
