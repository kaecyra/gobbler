package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	apishop "github.com/kaecyra/gobbler/internal/api/shopping"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipe/display"
	"github.com/kaecyra/gobbler/internal/store"
)

// Pattern is where the endpoint is mounted.
const Pattern = "/mcp"

// ReviewPath is where a person reviews a draft: the ingest review screen takes
// the draft's token after it.
const ReviewPath = "/ingest/"

// EditPath is where a person reviews a proposed edit: the recipe's editor, with
// the proposal's token in the query. ReviewPath's ingest screen is for new
// recipes; an edit is reviewed in the editor (ADR-00035).
func EditPath(recipeID int64, token string) string {
	return fmt.Sprintf("/recipes/%d/edit?proposal=%s", recipeID, token)
}

// Recipes is what the recipe tools call on the recipe store; *store.Store
// satisfies it.
type Recipes interface {
	Search(ctx context.Context, q store.SearchQuery) ([]store.Summary, error)
	Get(ctx context.Context, id int64) (recipe.Recipe, error)
	Info(ctx context.Context, id int64) (store.Info, error)
	// ProposeEdit holds a proposed new version of a recipe for review
	// (ADR-00035); GetProposal reads one back in whatever state it is in.
	ProposeEdit(ctx context.Context, id, baseVersion int64, r recipe.Recipe, by store.Provenance) (string, error)
	GetProposal(ctx context.Context, token string) (store.Proposal, error)
}

// Catalog is what the recipe tools read from the ingredient catalog: ingredient
// names for display, and the matching an edited line needs to be read like any
// other ingredient line. *catalog.Store satisfies it.
type Catalog interface {
	display.Ingredients
	MatchIngredient(ctx context.Context, term string) (catalog.Ingredient, bool, error)
	MatchUnit(ctx context.Context, term string) (catalog.Unit, bool, error)
	// ListIngredients and ListUnits are what the shopping service reads
	// (apishop.Catalog).
	ListIngredients(ctx context.Context) ([]catalog.Ingredient, error)
	ListUnits(ctx context.Context) ([]catalog.Unit, error)
}

// Drafter makes a draft from a URL or pasted text through the rule-first
// pipeline (ADR-00006); *ingest.Pipeline satisfies it.
type Drafter interface {
	FromURL(ctx context.Context, rawURL string) (ingest.Draft, error)
	FromText(ctx context.Context, text string) (ingest.Draft, error)
}

// Drafts holds a draft for the review screen, durably (ADR-00035);
// *ingest.Drafts satisfies it. The household is the one on ctx.
type Drafts interface {
	Add(ctx context.Context, d ingest.Draft, by store.Provenance) (string, error)
	// Full says the household already holds the most drafts it may, so a tool
	// can refuse before it fetches or reads anything. Add stays the authority.
	Full(ctx context.Context) (bool, error)
}

// Deps is everything the endpoint needs.
type Deps struct {
	Recipes Recipes
	// Lists is the shopping store the shopping service is built over.
	Lists *apishop.Store
	// Households is the membership lookup the shopping service enforces.
	Households apishop.Members
	Catalog    Catalog
	Drafter    Drafter
	Drafts     Drafts
	Audit      Audit
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
func NewDeps(rec *store.Store, lists *apishop.Store, households apishop.Members, cat *catalog.Store, drafts Drafts, audit Audit, cfg config.Config, log *slog.Logger) (Deps, error) {
	pipeline, err := ingest.New(cfg.Ingestion, ingest.Deps{Catalog: cat, Logger: log})
	if err != nil {
		return Deps{}, fmt.Errorf("build mcp import pipeline: %w", err)
	}
	return Deps{Recipes: rec, Lists: lists, Households: households, Catalog: cat, Drafter: pipeline, Drafts: drafts, Audit: audit, Limits: cfg.MCP, Logger: log}, nil
}

// Routes returns the endpoint, to be mounted at Pattern behind Bearer and the
// membership check. It holds each credential to its rate limit, then serves
// MCP over Streamable HTTP. It is stateless: every request stands alone, so a
// restart or a second request on another connection loses nothing.
func Routes(d Deps) (http.Handler, error) {
	if d.Recipes == nil || d.Lists == nil || d.Households == nil || d.Catalog == nil || d.Drafter == nil || d.Drafts == nil || d.Audit == nil {
		return nil, errors.New("mcp: recipes, lists, households, catalog, drafter, drafts and audit are required")
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
	shop, err := apishop.New(apishop.Deps{Lists: d.Lists, Recipes: d.Recipes, Catalog: d.Catalog, Members: d.Households})
	if err != nil {
		return nil, fmt.Errorf("mcp: %w", err)
	}
	t := &tools{Deps: d, log: log, shop: shop, reimporter: ingest.NewReimporter(d.Recipes, d.Drafter, d.Drafts)}
	srv := mcp.NewServer(&mcp.Implementation{Name: "gobbler", Version: version}, &mcp.ServerOptions{
		Instructions: "Gobbler is a household recipe manager. Recipes you create or import become drafts, and a change to an existing recipe becomes a proposal, that a person reviews before anything is saved. Shopping list changes apply at once.",
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
