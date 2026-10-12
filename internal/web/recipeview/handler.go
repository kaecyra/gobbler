// Package recipeview is the read-only recipe page: components, ingredient
// lines, equipment, steps and times, with scaling and unit modes (ADR-00005).
// It only displays: scaling and conversion come from package quantity, and the
// stored recipe is never changed by what the page shows. It is mounted from
// internal/web (ADR-00012).
package recipeview

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes. Other recipe features mount under
// the same prefix; this one owns the bare recipe URL.
const Pattern = "/recipes/{id}"

// Recipes is what the view reads from the recipe store.
type Recipes interface {
	Get(ctx context.Context, id int64) (recipe.Recipe, error)
}

// Catalog is what the view reads from the ingredient and equipment catalog.
type Catalog interface {
	GetIngredient(ctx context.Context, id int64) (catalog.Ingredient, error)
	GetEquipment(ctx context.Context, id int64) (catalog.Equipment, error)
}

// Deps is what the routes need.
type Deps struct {
	Recipes Recipes
	Catalog Catalog
}

// Routes returns the routes of the recipe view, relative to Pattern.
func Routes(d Deps) chi.Router {
	r := chi.NewRouter()
	r.Get("/", handle(d))
	return r
}

func handle(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || id <= 0 {
			layout.WriteError(w, r, fmt.Errorf("recipe id %q: %w", chi.URLParam(r, "id"), layout.ErrNotFound))
			return
		}
		rec, err := d.Recipes.Get(r.Context(), id)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
			return
		}
		opts, err := parseOptions(r.URL.Query(), rec)
		notice := ""
		if err != nil {
			if !fromHTMX(r) || !errors.Is(err, layout.ErrInvalid) {
				layout.WriteError(w, r, err)
				return
			}
			// htmx 2 does not swap a 4xx response, so a 400 would leave the
			// user with no feedback. Answer 200 with the recipe as stored and
			// a message, and keep the bad URL out of the address bar.
			slog.WarnContext(r.Context(), "request rejected",
				"error", err.Error(), "status", http.StatusOK, "method", r.Method, "path", r.URL.Path)
			opts, notice = options{mult: quantity.Int(1)}, invalidNotice
			if errors.Is(err, errNoServingsBase) {
				notice = noServingsBaseNotice
			}
			w.Header().Set("HX-Push-Url", "false")
		}
		v, err := buildView(r.Context(), d.Catalog, id, rec, opts)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("build view of recipe %d: %w", id, err))
			return
		}
		v.Notice = notice
		layout.Render(w, r, http.StatusOK, recipePage(v))
	}
}

// invalidNotice is the message for a scale, servings or units value the page
// cannot use. It is fixed text: the cause is in the log.
const invalidNotice = "That value cannot be used for this recipe. Enter a positive number or fraction, such as 6 or 1/2."

// noServingsBaseNotice answers a servings target on a recipe with a range of
// servings (or none): the number typed is fine, so say what to do instead.
const noServingsBaseNotice = "This recipe has no single serving count (a range, or none), so it cannot be scaled to one. Scale by a multiplier instead, such as 2× or 1/2×."

func fromHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// opaque flattens a catalog "not found" met while resolving a reference of an
// existing recipe: it is a broken reference, not a missing page, so it must
// not map to 404 (the page would claim the recipe does not exist).
func opaque(err error, what string) error {
	if errors.Is(err, catalog.ErrNotFound) {
		return fmt.Errorf("%s: dangling reference: %s", what, err.Error())
	}
	return fmt.Errorf("%s: %w", what, err)
}
