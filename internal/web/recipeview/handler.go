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
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipe/display"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes. Other recipe features mount under
// the same prefix; this one owns the bare recipe URL.
const Pattern = "/recipes/{id}"

// Recipes is what the view reads from the recipe store.
type Recipes interface {
	Get(ctx context.Context, id int64) (recipe.Recipe, error)
	Info(ctx context.Context, id int64) (store.Info, error)
	// HasThumbnail and Thumbnail serve the preview image of the recipe's video
	// (ADR-00021). Thumbnail is by recipe, never by video ID.
	HasThumbnail(ctx context.Context, videoID string) (bool, error)
	Thumbnail(ctx context.Context, id int64) (store.Thumbnail, error)
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
	r.Get(thumbnailRoute, thumbnail(d))
	return r
}

// thumbnailRoute is the thumbnail's path below Pattern.
const thumbnailRoute = "/video/thumbnail"

// thumbnailMaxAge lets the browser keep a thumbnail for a day; it is private
// because the response is behind the household check.
const thumbnailMaxAge = "private, max-age=86400"

// thumbnail serves the stored preview image of the recipe's video. The image
// is reached through the recipe, which the store scopes to the household, so a
// recipe of another household, a recipe with no video and a video with no
// stored image all answer 404 (ADR-00021 decision 4).
func thumbnail(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || id <= 0 {
			layout.WriteError(w, r, fmt.Errorf("recipe id %q: %w", chi.URLParam(r, "id"), layout.ErrNotFound))
			return
		}
		t, err := d.Recipes.Thumbnail(r.Context(), id)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("load thumbnail of recipe %d: %w", id, err))
			return
		}
		h := w.Header()
		h.Set("Content-Type", t.ContentType)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", thumbnailMaxAge)
		if _, err := w.Write(t.Content); err != nil {
			slog.WarnContext(r.Context(), "write thumbnail", "recipe_id", id, "error", err.Error())
		}
	}
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
		info, err := d.Recipes.Info(r.Context(), id)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("load info of recipe %d: %w", id, err))
			return
		}
		q := r.URL.Query()
		opts, err := display.ParseOptions(q.Get(display.ParamScale), q.Get(display.ParamServings), q.Get(display.ParamUnits), rec)
		notice := ""
		if err != nil {
			if !fromHTMX(r) || !errors.Is(err, display.ErrInvalid) {
				layout.WriteError(w, r, err)
				return
			}
			// htmx 2 does not swap a 4xx response, so a 400 would leave the
			// user with no feedback. Answer 200 with the recipe as stored and
			// a message, and keep the bad URL out of the address bar.
			slog.WarnContext(r.Context(), "request rejected",
				"error", err.Error(), "status", http.StatusOK, "method", r.Method, "path", r.URL.Path)
			opts, notice = display.Default(), invalidNotice
			if errors.Is(err, display.ErrNoServingsBase) {
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
		if rec.VideoID != "" {
			has, err := d.Recipes.HasThumbnail(r.Context(), rec.VideoID)
			if err != nil {
				layout.WriteError(w, r, fmt.Errorf("check thumbnail of recipe %d: %w", id, err))
				return
			}
			v.HasThumbnail = has
		}
		if info.ClonedFrom != nil {
			v.Origin = &originView{RecipeID: info.ClonedFrom.RecipeID, Version: info.ClonedFrom.Version}
		}
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
