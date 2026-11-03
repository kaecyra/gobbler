package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipe/display"
	"github.com/kaecyra/gobbler/internal/shopping"
	"github.com/kaecyra/gobbler/internal/store"
)

// Pattern is where the endpoint is mounted.
const Pattern = "/mcp"

// ReviewPath is where a person reviews a draft: the ingest review screen takes
// the draft's token after it.
const ReviewPath = "/ingest/"

// Recipes is what the recipe tools call on the recipe store; *store.Store
// satisfies it.
type Recipes interface {
	Search(ctx context.Context, q store.SearchQuery) ([]store.Summary, error)
	Get(ctx context.Context, id int64) (recipe.Recipe, error)
}

// Lists is what the shopping tools call on the shopping store;
// *shopping.Store satisfies it.
type Lists interface {
	Lists(ctx context.Context) ([]shopping.ListInfo, error)
	CreateList(ctx context.Context, name string) (shopping.ListInfo, error)
	Load(ctx context.Context, id int64) (shopping.View, error)
	AddEntry(ctx context.Context, listID, recipeID int64, multiplier quantity.Rat) (int64, error)
	AddManualItem(ctx context.Context, listID int64, m shopping.ManualItem) (int64, error)
	SetChecked(ctx context.Context, listID int64, key shopping.ItemKey, checked bool) error
	Pantry(ctx context.Context) ([]recipe.IngredientID, error)
}

// Drafter makes a draft from a URL or pasted text through the rule-first
// pipeline (ADR-00006); *ingest.Pipeline satisfies it.
type Drafter interface {
	FromURL(ctx context.Context, rawURL string) (ingest.Draft, error)
	FromText(ctx context.Context, text string) (ingest.Draft, error)
}

// Drafts holds a draft for the review screen; *ingest.Pending satisfies it.
type Drafts interface {
	Add(hid int64, d ingest.Draft) (string, error)
}

// Deps is everything the endpoint needs.
type Deps struct {
	Recipes Recipes
	Lists   Lists
	Catalog display.Ingredients
	Drafter Drafter
	Drafts  Drafts
	Audit   Audit
	// Limits is the rate and burst each credential is held to.
	Limits config.MCP
	// Version is reported to clients as the server's version.
	Version string
	Logger  *slog.Logger
	// Now is the clock of the rate limit; nil means time.Now.
	Now func() time.Time
}

// NewDeps builds the endpoint's collaborators over the stores the router
// already shares. The pipeline has no LLM provider: an agent's import takes the
// rule-based path and a draft below the confidence threshold is flagged for the
// reviewer, never sent to a model (ADR-00006).
func NewDeps(rec *store.Store, lists *shopping.Store, cat *catalog.Store, pending *ingest.Pending, audit Audit, cfg config.Config, log *slog.Logger) (Deps, error) {
	pipeline, err := ingest.New(cfg.Ingestion, ingest.Deps{Catalog: cat, Logger: log})
	if err != nil {
		return Deps{}, fmt.Errorf("build mcp import pipeline: %w", err)
	}
	return Deps{Recipes: rec, Lists: lists, Catalog: cat, Drafter: pipeline, Drafts: pending, Audit: audit, Limits: cfg.MCP, Logger: log}, nil
}

// Routes returns the endpoint, to be mounted at Pattern behind Bearer and the
// membership check. It holds each credential to its rate limit, then serves
// MCP over Streamable HTTP. It is stateless: every request stands alone, so a
// restart or a second request on another connection loses nothing.
func Routes(d Deps) (http.Handler, error) {
	if d.Recipes == nil || d.Lists == nil || d.Catalog == nil || d.Drafter == nil || d.Drafts == nil || d.Audit == nil {
		return nil, errors.New("mcp: recipes, lists, catalog, drafter, drafts and audit are required")
	}
	if d.Limits.Rate <= 0 || d.Limits.Burst < 1 {
		return nil, fmt.Errorf("mcp: the rate limit needs a positive rate and burst, got %v and %d", d.Limits.Rate, d.Limits.Burst)
	}
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	now := d.Now
	if now == nil {
		now = time.Now
	}
	version := d.Version
	if version == "" {
		version = "dev"
	}
	t := &tools{Deps: d, log: log}
	srv := mcp.NewServer(&mcp.Implementation{Name: "gobbler", Version: version}, &mcp.ServerOptions{
		Instructions: "Gobbler is a household recipe manager. Recipes you create or import become drafts that a person reviews before they are saved. Shopping list changes apply at once.",
	})
	t.register(srv)
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, Logger: log,
	})
	return newLimiter(d.Limits.Rate, d.Limits.Burst, now).limit(h), nil
}

// scoped returns the credential and household of the request a tool runs for,
// both put there by the layers in front of it.
func scoped(ctx context.Context) (Credential, household.Household, error) {
	c, ok := CredentialFrom(ctx)
	if !ok {
		return Credential{}, household.Household{}, errors.New("mcp: no credential on the request")
	}
	h, ok := household.FromContext(ctx)
	if !ok {
		return Credential{}, household.Household{}, errors.New("mcp: no household on the request")
	}
	return c, h, nil
}
