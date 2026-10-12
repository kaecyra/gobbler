// Package cook is cook mode: one step of a recipe at a time, with step timers
// and a screen wake lock (ADR-00011). The page is server-rendered; only the
// timers and the Screen Wake Lock API need JavaScript (ADR-00002), served from
// the embedded static directory. Scaling, unit modes and step numbering come
// from internal/recipe/display, shared with the recipe view, because a feature
// package may not import another (ADR-00012).
package cook

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipe/display"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes.
const Pattern = "/recipes/{id}/cook"

// paramStep is the query parameter holding the 1-based number of the step
// shown. It counts steps in the reading order of the recipe view.
const paramStep = "step"

// Recipes is what cook mode reads from the recipe store.
type Recipes interface {
	Get(ctx context.Context, id int64) (recipe.Recipe, error)
}

// Catalog is what cook mode reads from the ingredient and equipment catalog.
type Catalog interface {
	GetIngredient(ctx context.Context, id int64) (catalog.Ingredient, error)
	GetEquipment(ctx context.Context, id int64) (catalog.Equipment, error)
}

// Deps is what the routes need.
type Deps struct {
	Recipes Recipes
	Catalog Catalog
}

// Routes returns the routes of cook mode, relative to Pattern.
func Routes(d Deps) chi.Router {
	r := chi.NewRouter()
	r.Get("/", handle(d))
	return r
}

func handle(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rawID := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || id <= 0 {
			layout.WriteError(w, r, fmt.Errorf("recipe id %q: %w", rawID, layout.ErrNotFound))
			return
		}
		rec, err := d.Recipes.Get(r.Context(), id)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
			return
		}
		q := r.URL.Query()
		opts, err := display.ParseOptions(q.Get(display.ParamScale), q.Get(display.ParamServings), q.Get(display.ParamUnits), rec)
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		step, err := parseStep(q.Get(paramStep))
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		p, err := buildPage(r.Context(), d.Catalog, id, rec, opts, step)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("build cook page of recipe %d: %w", id, err))
			return
		}
		layout.Render(w, r, http.StatusOK, cookPage(p))
	}
}

// parseStep reads the step parameter; absent means the first step.
func parseStep(raw string) (int, error) {
	if raw == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%w: step %q", layout.ErrInvalid, raw)
	}
	return n, nil
}

// errStepOutOfRange marks a step number past the end of the recipe.
var errStepOutOfRange = errors.New("step is past the last step")
