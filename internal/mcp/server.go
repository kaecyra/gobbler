package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apicatalog "github.com/kaecyra/gobbler/internal/api/catalog"
	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
	apiingest "github.com/kaecyra/gobbler/internal/api/ingest"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	apishop "github.com/kaecyra/gobbler/internal/api/shopping"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/recipe/display"
	"github.com/kaecyra/gobbler/internal/recipepath"
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
	return recipepath.Base(recipeID) + "/edit?proposal=" + token
}

// Catalog is what the recipe tools read from the ingredient catalog: ingredient
// names for display, and the matching an edited line needs to be read like any
// other ingredient line. NewDeps gives it the catalog service bound to the
// credential of the request (apicatalog.Bound), which checks membership on every
// call, whichever kind of credential it is (ADR-00031, ADR-00043).
type Catalog interface {
	display.Ingredients
	// display.Equipment names a recipe's equipment for get_recipe.
	display.Equipment
	MatchIngredient(ctx context.Context, term string) (apicatalog.Ingredient, bool, error)
	MatchUnit(ctx context.Context, term string) (apicatalog.Unit, bool, error)
	// ListIngredients and ListUnits are what the shopping service reads
	// (apishop.Catalog).
	ListIngredients(ctx context.Context) ([]apicatalog.Ingredient, error)
	ListUnits(ctx context.Context) ([]apicatalog.Unit, error)
}

// Drafter makes a draft from a URL or pasted text through the rule-first
// pipeline (ADR-00006). NewDeps gives it the ingestion service bound to the
// credential of the request (apiingest.Bound), which checks membership on every
// call, whichever kind of credential it is (ADR-00031, ADR-00043).
type Drafter interface {
	FromURL(ctx context.Context, rawURL string) (apiingest.Draft, error)
	FromText(ctx context.Context, text string) (apiingest.Draft, error)
}

// Previewer drafts what an import would hold without holding it
// (ADR-00048). NewDeps gives it the ingestion service bound to the credential
// of the request (apiingest.Bound), as it does Drafter.
type Previewer interface {
	Preview(ctx context.Context, src apiingest.PreviewSource) (apiingest.Preview, error)
}

// Deps is everything the endpoint needs.
type Deps struct {
	// Recipes is the recipe service (ADR-00043): the recipe, edit, import and
	// re-import tools reach the recipe domain only through it.
	Recipes *apirecipes.Service
	// RecipeReads is what the shopping service reads of the recipes it lists
	// (apishop.Recipes); *apirecipes.Store satisfies it. It carries no Caller:
	// the shopping service has already admitted its own.
	RecipeReads apishop.Recipes
	// Lists is the shopping store the shopping service is built over.
	Lists *apishop.Store
	// Households is the membership lookup the shopping service enforces.
	Households apishop.Members
	// HouseStyle reads the household's house style for get_house_style
	// (ADR-00048).
	HouseStyle HouseStyles
	Catalog    Catalog
	Drafter    Drafter
	Previewer  Previewer
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
// reviewer, never sent to a model (ADR-00006). reads is also where a preview
// reads a saved recipe's original page (apiingest.Originals).
func NewDeps(rec *apirecipes.Service, reads *apirecipes.Store, lists *apishop.Store, households *apihouseholds.Store, cat *apicatalog.Store, audit Audit, cfg config.Config, log *slog.Logger) (Deps, error) {
	// The household service reads the house style; get_house_style is the only
	// tool that calls it, and it reads.
	hs, err := apihouseholds.New(apihouseholds.Deps{Households: households})
	if err != nil {
		return Deps{}, fmt.Errorf("build mcp household service: %w", err)
	}
	ing, err := apiingest.New(cfg.Ingestion, apiingest.Deps{Catalog: cat, Members: households, Logger: log, Originals: reads})
	if err != nil {
		return Deps{}, fmt.Errorf("build mcp import service: %w", err)
	}
	svc, err := apicatalog.New(apicatalog.Deps{Catalog: cat, Members: households})
	if err != nil {
		return Deps{}, fmt.Errorf("build mcp catalog service: %w", err)
	}
	return Deps{Recipes: rec, RecipeReads: reads, Lists: lists, Households: households, HouseStyle: hs, Catalog: svc.Bind(credentialCaller(log)), Drafter: ing.Bind(credentialCaller(log)), Previewer: ing.Bind(credentialCaller(log)), Audit: audit, Limits: cfg.MCP, Logger: log}, nil
}

// credentialCaller names the Caller of a catalog lookup or a draft from the
// credential and household on the request's context (callerOf), for
// apicatalog.Bound and apiingest.Bound. A context with neither names nobody: the
// zero Caller, which those services refuse with ErrNotMember, so a call that
// lost its credential fails closed.
// The cause is logged at debug, so the refusal can be traced to the scoping
// failure.
func credentialCaller(log *slog.Logger) func(ctx context.Context) caller.Caller {
	if log == nil {
		log = slog.Default()
	}
	return func(ctx context.Context) caller.Caller {
		c, err := recipeCaller(ctx)
		if err != nil {
			log.DebugContext(ctx, "mcp catalog lookup has no caller", "error", err.Error())
			return caller.Caller{}
		}
		return c
	}
}

// Routes returns the endpoint, to be mounted at Pattern behind Bearer and the
// membership check. It holds each credential to its rate limit, then serves
// MCP over Streamable HTTP. It is stateless: every request stands alone, so a
// restart or a second request on another connection loses nothing.
func Routes(d Deps) (http.Handler, error) {
	if d.Recipes == nil || d.RecipeReads == nil || d.Lists == nil || d.Households == nil || d.HouseStyle == nil || d.Catalog == nil || d.Drafter == nil || d.Previewer == nil || d.Audit == nil {
		return nil, errors.New("mcp: recipes, recipe reads, lists, households, house style, catalog, drafter, previewer and audit are required")
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
	shop, err := apishop.New(apishop.Deps{Lists: d.Lists, Recipes: d.RecipeReads, Catalog: d.Catalog, Members: d.Households})
	if err != nil {
		return nil, fmt.Errorf("mcp: %w", err)
	}
	t := &tools{Deps: d, log: log, shop: shop}
	srv := mcp.NewServer(&mcp.Implementation{Name: "gobbler", Version: version}, &mcp.ServerOptions{
		Instructions: "Gobbler is a household recipe manager. Recipes you create or import become drafts, and a change to an existing recipe becomes a proposal, that a person reviews before anything is saved. Shopping list changes apply at once. Before you revise a draft or propose a change to a recipe, read the household's house style with get_house_style and follow it.",
	})
	t.register(srv)
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, Logger: log,
	})
	return newLimiter(d.Limits.Rate, d.Limits.Burst, now).limit(h), nil
}

// scoped returns the credential and household of the request a tool runs for,
// both put there by the layers in front of it.
func scoped(ctx context.Context) (Credential, apihouseholds.Household, error) {
	c, ok := CredentialFrom(ctx)
	if !ok {
		return Credential{}, apihouseholds.Household{}, errors.New("mcp: no credential on the request")
	}
	h, ok := apihouseholds.FromContext(ctx)
	if !ok {
		return Credential{}, apihouseholds.Household{}, errors.New("mcp: no household on the request")
	}
	return c, h, nil
}
