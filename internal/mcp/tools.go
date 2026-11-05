package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/parse"
	"github.com/kaecyra/gobbler/internal/shopping"
	"github.com/kaecyra/gobbler/internal/store"
)

// tools holds the collaborators every tool handler shares.
type tools struct {
	Deps
	log *slog.Logger
}

func (t *tools) register(s *mcp.Server) {
	t.registerRecipes(s)
	t.registerImport(s)
	t.registerEdit(s)
	t.registerShopping(s)
}

var (
	readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true}
	// applies marks a tool whose change is made at once, and never destroys
	// anything.
	applies = &mcp.ToolAnnotations{DestructiveHint: boolPtr(false)}
	// drafts marks a tool whose result is only a draft for review.
	drafts = &mcp.ToolAnnotations{DestructiveHint: boolPtr(false)}
)

func boolPtr(b bool) *bool { return &b }

// userError is a refusal whose message the client may see and act on.
type userError string

func (e userError) Error() string { return string(e) }

func refuse(format string, args ...any) error { return userError(fmt.Sprintf(format, args...)) }

// toolError turns an error into what the client is shown. Domain errors that
// the caller can fix carry their own message; anything else is logged in full
// and shown as a generic failure, so a query or a path never reaches an agent.
func (t *tools) toolError(ctx context.Context, tool string, err error) error {
	var ue userError
	switch {
	case errors.As(err, &ue):
		return ue
	case errors.Is(err, store.ErrNotFound), errors.Is(err, shopping.ErrNotFound):
		return refuse("not found")
	case errors.Is(err, shopping.ErrInvalid), errors.Is(err, store.ErrInvalid):
		return refuse("invalid input: %s", err)
	case errors.Is(err, shopping.ErrUnknownReference), errors.Is(err, store.ErrUnknownReference):
		return refuse("that refers to a recipe, ingredient or unit gobbler does not have")
	case errors.Is(err, parse.ErrEmptyText):
		return refuse("there is no text to read")
	case errors.Is(err, ingest.ErrFetch):
		return refuse("the page could not be fetched: %s", err)
	}
	t.log.ErrorContext(ctx, "mcp tool failed", "tool", tool, "error", err.Error())
	return errors.New("the tool failed; the failure was logged")
}

// recordWrite logs that the credential made a write. It runs after the write
// succeeded, so its failure is reported to the client, never swallowed: a
// write that cannot be attributed is a bug to see (ADR-00031).
func (t *tools) recordWrite(ctx context.Context, c Credential, hid int64, tool, target string) error {
	if err := t.Audit.RecordWrite(ctx, c, hid, tool, target); err != nil {
		return fmt.Errorf("record %s write of %s: %w", tool, c.Subject(), err)
	}
	return nil
}
