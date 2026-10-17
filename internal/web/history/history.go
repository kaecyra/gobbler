// Package history is the revision history of a recipe: the list of saved
// earlier versions, a read-only view of any one of them, restoring one, and
// cloning a recipe (ADR-00004). The store owns what a revision, a restore and
// a clone mean; this package only exposes them. No route here touches a source
// snapshot. It is mounted from internal/web (ADR-00012).
package history

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/video"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Mount points. recipeview owns the bare /recipes/{id}; the history and the
// clone action mount beneath it.
const (
	Pattern      = "/recipes/{id}/history"
	ClonePattern = "/recipes/{id}/clone"
)

// confirmField and confirmValue are the form field that proves the user saw the
// restore confirmation step; a restore without it is refused.
const (
	confirmField = "confirm"
	confirmValue = "yes"
)

// Recipes is what the history needs from the recipe store.
type Recipes interface {
	Get(ctx context.Context, id int64) (recipe.Recipe, error)
	Info(ctx context.Context, id int64) (store.Info, error)
	ListRevisions(ctx context.Context, id int64) ([]store.Revision, error)
	GetVersion(ctx context.Context, id, version int64) (recipe.Recipe, error)
	Restore(ctx context.Context, id, version int64) error
	Clone(ctx context.Context, id int64) (int64, error)
	// HasThumbnail and SaveThumbnail keep the preview image of a recipe's video
	// (ADR-00021).
	video.ThumbnailStore
}

// Catalog is what a version's page reads from the ingredient and equipment
// catalog.
type Catalog interface {
	GetIngredient(ctx context.Context, id int64) (catalog.Ingredient, error)
	GetEquipment(ctx context.Context, id int64) (catalog.Equipment, error)
}

// Deps is what the routes need.
type Deps struct {
	Recipes Recipes
	Catalog Catalog
	// Thumbnails downloads the preview image of a restored recipe's video;
	// nil means YouTube's image host.
	Thumbnails video.ThumbnailSource
}

// Routes returns the history routes, relative to Pattern.
func Routes(d Deps) chi.Router {
	r := chi.NewRouter()
	r.Get("/", list(d))
	r.Get("/{version}", show(d))
	r.Post("/{version}/restore", restore(d))
	return r
}

// CloneRoutes returns the clone route, relative to ClonePattern.
func CloneRoutes(d Deps) chi.Router {
	r := chi.NewRouter()
	r.Post("/", clone(d))
	return r
}

func recipePath(id int64) string { return "/recipes/" + strconv.FormatInt(id, 10) }

func historyPath(id int64) string { return recipePath(id) + "/history" }

func versionPath(id, version int64) string {
	return historyPath(id) + "/" + strconv.FormatInt(version, 10)
}

// idParam reads a positive integer URL parameter; anything else is a missing
// page, not a bad request, since no such record can exist.
func idParam(r *http.Request, name string) (int64, error) {
	raw := chi.URLParam(r, name)
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s %q: %w", name, raw, layout.ErrNotFound)
	}
	return n, nil
}

// refuse answers 400 with a message that says what to do instead. The generic
// error page shows only the sentinel's text, which would not.
func refuse(w http.ResponseWriter, r *http.Request, message, cause string) {
	slog.WarnContext(r.Context(), "request rejected",
		"error", cause, "status", http.StatusBadRequest, "method", r.Method, "path", r.URL.Path)
	layout.Render(w, r, http.StatusBadRequest, layout.ErrorPage(http.StatusBadRequest, message))
}

func list(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := idParam(r, "id")
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		info, err := d.Recipes.Info(r.Context(), id)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
			return
		}
		revs, err := d.Recipes.ListRevisions(r.Context(), id)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("list revisions of recipe %d: %w", id, err))
			return
		}
		doc, err := d.Recipes.GetVersion(r.Context(), id, info.Version)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
			return
		}
		// The store lists oldest first; the page shows newest first.
		slices.Reverse(revs)
		layout.Render(w, r, http.StatusOK, listPage(buildList(id, doc.Name, info, revs)))
	}
}

func show(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := idParam(r, "id")
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		version, err := idParam(r, "version")
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		info, err := d.Recipes.Info(r.Context(), id)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
			return
		}
		doc, err := d.Recipes.GetVersion(r.Context(), id, version)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("load version %d of recipe %d: %w", version, id, err))
			return
		}
		v, err := buildVersion(r.Context(), d.Catalog, id, version, info.Version, doc)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("build version %d of recipe %d: %w", version, id, err))
			return
		}
		layout.Render(w, r, http.StatusOK, versionPage(v))
	}
}

func restore(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := idParam(r, "id")
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		version, err := idParam(r, "version")
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		info, err := d.Recipes.Info(r.Context(), id)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
			return
		}
		if err := r.ParseForm(); err != nil {
			layout.WriteError(w, r, fmt.Errorf("read restore form: %w", layout.ErrInvalid))
			return
		}
		if r.PostForm.Get(confirmField) != confirmValue {
			refuse(w, r, "Restoring needs confirmation. Open the version and use the confirm button.",
				fmt.Sprintf("restore of version %d of recipe %d was not confirmed", version, id))
			return
		}
		if version == info.Version {
			refuse(w, r, "That is already the current version, so there is nothing to restore.",
				fmt.Sprintf("version %d of recipe %d is the current version", version, id))
			return
		}
		if err := d.Recipes.Restore(r.Context(), id, version); err != nil {
			layout.WriteError(w, r, fmt.Errorf("restore version %d of recipe %d: %w", version, id, err))
			return
		}
		// The restored revision can carry a video with no stored thumbnail
		// (ADR-00021 decision 3); the restore is already done.
		restored, err := d.Recipes.GetVersion(r.Context(), id, version)
		keepThumbnail(r.Context(), d, id, restored, err)
		http.Redirect(w, r, historyPath(id), http.StatusSeeOther)
	}
}

func clone(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := idParam(r, "id")
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		newID, err := d.Recipes.Clone(r.Context(), id)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("clone recipe %d: %w", id, err))
			return
		}
		// The clone is a saved revision carrying the source's video (ADR-00021
		// decision 3); the clone is already made.
		cloned, err := d.Recipes.Get(r.Context(), newID)
		keepThumbnail(r.Context(), d, newID, cloned, err)
		http.Redirect(w, r, recipePath(newID), http.StatusSeeOther)
	}
}

// keepThumbnail stores the thumbnail of the video of the recipe a handler has
// just saved. readErr is the failure of reading that recipe back: the save is
// already done, so it is logged and the response is unchanged.
func keepThumbnail(ctx context.Context, d Deps, id int64, saved recipe.Recipe, readErr error) {
	if readErr != nil {
		slog.WarnContext(ctx, "saved recipe left without a video thumbnail check", "recipe_id", id, "error", readErr.Error())
		return
	}
	video.KeepThumbnail(ctx, d.Recipes, d.Thumbnails, id, saved.VideoID)
}
