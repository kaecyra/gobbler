// Package shopping is the shopping pages: build a list from recipes and manual
// items, the pantry, the live phone view that updates over server-sent events,
// and the plain-text export (ADR-00009). Generation, persistence and export
// belong to internal/shopping; this package is the page over them and adds no
// merge or storage logic. It is mounted from internal/web (ADR-00012).
package shopping

import (
	"context"
	_ "embed" // the page script and stylesheet are embedded (ADR-00002)
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	domain "github.com/kaecyra/gobbler/internal/shopping"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes.
const Pattern = "/shopping"

// maxFormBytes bounds a form body; every form here is a few short fields.
const maxFormBytes = 64 << 10

// heartbeatEvery keeps an idle event stream alive through proxies, which drop a
// connection that stays silent (the Cloudflare tunnel does so after about a
// hundred seconds).
const heartbeatEvery = 25 * time.Second

//go:embed assets/shopping.js
var scriptJS []byte

//go:embed assets/shopping.css
var styleCSS []byte

// Lists is what the routes use from the shopping store. *shopping.Store
// satisfies it.
type Lists interface {
	CreateList(ctx context.Context, name string) (domain.ListInfo, error)
	Lists(ctx context.Context) ([]domain.ListInfo, error)
	GetList(ctx context.Context, id int64) (domain.ListInfo, error)
	Load(ctx context.Context, id int64) (domain.View, error)
	Regenerate(ctx context.Context, id int64) (domain.View, error)
	AddEntry(ctx context.Context, listID, recipeID int64, multiplier quantity.Rat) (int64, error)
	SetEntryMultiplier(ctx context.Context, id int64, multiplier quantity.Rat) error
	RemoveEntry(ctx context.Context, id int64) error
	SetIncludeOptional(ctx context.Context, id int64, include bool) error
	AddManualItem(ctx context.Context, listID int64, m domain.ManualItem) (int64, error)
	RemoveManualItem(ctx context.Context, id int64) error
	SetChecked(ctx context.Context, listID int64, key domain.ItemKey, checked bool) error
	AddToPantry(ctx context.Context, ingredient recipe.IngredientID) error
	RemoveFromPantry(ctx context.Context, ingredient recipe.IngredientID) error
	Pantry(ctx context.Context) ([]recipe.IngredientID, error)
	OnChange(h func(domain.Change))
}

// Recipes is what the routes use from the recipe store. *store.Store satisfies
// it.
type Recipes interface {
	Search(ctx context.Context, q store.SearchQuery) ([]store.Summary, error)
}

// Catalog is what the routes use from the catalog store. *catalog.Store
// satisfies it.
type Catalog interface {
	ListIngredients(ctx context.Context) ([]catalog.Ingredient, error)
	ListUnits(ctx context.Context) ([]catalog.Unit, error)
}

// Deps is what the routes need.
type Deps struct {
	Lists   Lists
	Recipes Recipes
	Catalog Catalog
	Logger  *slog.Logger
}

// Routes returns the routes of the pages, relative to Pattern. It registers the
// live view's change handler on d.Lists, so call it once, at start-up, before
// the store is shared between goroutines (shopping.Store.OnChange).
func Routes(d Deps) chi.Router {
	h := newHandler(d)
	d.Lists.OnChange(h.hub.notify)
	return h.routes()
}

func (h *handler) routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.index)
	r.Post("/", h.create)
	r.Get("/pantry", h.pantryShow)
	r.Post("/pantry/add", h.pantryAdd)
	r.Post("/pantry/remove", h.pantryRemove)
	r.Get("/shopping.js", asset("text/javascript; charset=utf-8", scriptJS))
	r.Get("/shopping.css", asset("text/css; charset=utf-8", styleCSS))
	r.Get("/{id}", h.build)
	r.Post("/{id}/recipes", h.addRecipe)
	r.Post("/{id}/recipes/{entry}", h.setMultiplier)
	r.Post("/{id}/recipes/{entry}/remove", h.removeRecipe)
	r.Post("/{id}/manual", h.addManual)
	r.Post("/{id}/manual/{item}/remove", h.removeManual)
	r.Post("/{id}/addback", h.addBack)
	r.Post("/{id}/regenerate", h.regenerate)
	r.Post("/{id}/optional", h.setOptional)
	r.Get("/{id}/live", h.live)
	r.Post("/{id}/check", h.check)
	r.Get("/{id}/events", h.events)
	r.Get("/{id}/export.txt", h.export)
	return r
}

type handler struct {
	Deps
	hub       *hub
	log       *slog.Logger
	heartbeat time.Duration
}

func newHandler(d Deps) *handler {
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	return &handler{Deps: d, hub: newHub(), log: log, heartbeat: heartbeatEvery}
}

// asset serves an embedded file. The bytes never change while the binary runs.
func asset(contentType string, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		if _, err := w.Write(body); err != nil {
			// The client went away after the headers; nothing else to send.
			slog.WarnContext(r.Context(), "write asset", "error", err.Error())
		}
	}
}

// fail answers err through the layout's error page. It is the one place a
// shopping error becomes a web error kind, so the status table in layout stays
// the one place a kind becomes a status.
func fail(w http.ResponseWriter, r *http.Request, err error) {
	layout.WriteError(w, r, webError(err))
}

// webError tags a shopping store error with the layout kind that carries its
// status. Errors it does not know stay as they are, an internal failure.
func webError(err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return errors.Join(layout.ErrNotFound, err)
	case errors.Is(err, domain.ErrDuplicate):
		return errors.Join(layout.ErrConflict, err)
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, domain.ErrUnknownReference):
		return errors.Join(layout.ErrInvalid, err)
	}
	return err
}

// idParam parses a positive id from a URL segment or form field; anything else
// is a bad request.
func idParam(what, s string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%s %q: %w", what, s, layout.ErrInvalid)
	}
	return id, nil
}

func listID(r *http.Request) (int64, error) { return idParam("list id", chi.URLParam(r, "id")) }

// parseForm bounds and parses the body, answering a malformed one itself.
func parseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		fail(w, r, fmt.Errorf("read form: %w", errors.Join(layout.ErrInvalid, err)))
		return false
	}
	return true
}

func listURL(id int64) string { return fmt.Sprintf("%s/%d", Pattern, id) }

// The two ways a list is grouped, chosen by the group query parameter. By aisle
// is the default.
const (
	groupAisle  = "aisle"
	groupRecipe = "recipe"

	// manualGroupName heads the by-recipe group of hand-added items.
	manualGroupName = "Added by hand"
)

// groupOf reads the grouping a request asks for. A value other than the two
// known ones is a bad request rather than a quiet fall back to the default.
func groupOf(r *http.Request) (string, error) {
	switch g := r.URL.Query().Get("group"); g {
	case "", groupAisle:
		return groupAisle, nil
	case groupRecipe:
		return groupRecipe, nil
	default:
		return "", fmt.Errorf("group %q is neither %q nor %q: %w", g, groupAisle, groupRecipe, layout.ErrInvalid)
	}
}

// groupQuery is the query string that keeps a page in grouping g.
func groupQuery(g string) string {
	if g == groupRecipe {
		return "?group=" + groupRecipe
	}
	return ""
}

// back redirects to path of list id, in the grouping the request came from, so
// an edit does not throw the reader back to the default view.
func back(w http.ResponseWriter, r *http.Request, id int64, suffix string) {
	g, err := groupOf(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	http.Redirect(w, r, listURL(id)+suffix+groupQuery(g), http.StatusSeeOther)
}
