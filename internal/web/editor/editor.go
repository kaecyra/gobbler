// Package editor is the recipe editor: a structured form for the metadata,
// components, ingredient lines with typed qualifiers, equipment and steps with
// their dependencies (ADR-00004). Quantities are read by package quantity
// (ADR-00005), ingredients, units and equipment are chosen from the catalog
// (ADR-00008), and the step graph is checked by package recipe. Saving goes
// through the recipe store, which writes the revision holding the prior
// document; the editor never writes SQL and never touches a source snapshot.
// The author of an edit is the verified identity on the request context
// (ADR-00003). It is mounted from internal/web (ADR-00012). The same form is
// the ingest review screen over an unsaved draft (Unsaved, ADR-00020 decision 3).
package editor

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
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/video"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes: the editor of a recipe is the URL
// below the recipe's own page.
const Pattern = "/recipes/{id}/edit"

// maxFormBytes bounds an editor form body; a long recipe is still prose.
const maxFormBytes = 1 << 20

// Recipes is what the editor needs from the recipe store.
type Recipes interface {
	Get(ctx context.Context, id int64) (recipe.Recipe, error)
	Info(ctx context.Context, id int64) (store.Info, error)
	UpdateIfVersion(ctx context.Context, id, expectedVersion int64, r recipe.Recipe) error
	// HasThumbnail and SaveThumbnail keep the preview image of a recipe's video
	// (ADR-00021).
	video.ThumbnailStore
}

// Catalog is what the editor needs from the catalog: matching what the user
// typed, listing names to suggest, and proposing an unreviewed entry.
type Catalog interface {
	MatchIngredient(ctx context.Context, term string) (catalog.Ingredient, bool, error)
	MatchEquipment(ctx context.Context, term string) (catalog.Equipment, bool, error)
	MatchUnit(ctx context.Context, term string) (catalog.Unit, bool, error)
	ListIngredients(ctx context.Context) ([]catalog.Ingredient, error)
	ListEquipment(ctx context.Context) ([]catalog.Equipment, error)
	ListUnits(ctx context.Context) ([]catalog.Unit, error)
	CreateIngredient(ctx context.Context, in catalog.Ingredient) (catalog.Ingredient, error)
	CreateEquipment(ctx context.Context, in catalog.Equipment) (catalog.Equipment, error)
}

// Deps is what the routes need.
type Deps struct {
	Recipes Recipes
	Catalog Catalog
	// Thumbnails downloads the preview image of a recipe's video when the
	// recipe is saved; nil means YouTube's image host.
	Thumbnails video.ThumbnailSource
}

// Routes returns the routes of the editor, relative to Pattern.
func Routes(d Deps) chi.Router {
	h := &handler{Deps: d}
	r := chi.NewRouter()
	r.Get("/", h.show)
	r.Post("/", h.post)
	return r
}

type handler struct{ Deps }

// staleMessage is shown when the recipe changed after the form was loaded.
const staleMessage = "This recipe was changed after you opened the editor, so nothing was saved. " +
	"Copy anything you want to keep, then reload the editor to start from the latest version."

func (h *handler) show(w http.ResponseWriter, r *http.Request) {
	id, ok := recipeID(w, r)
	if !ok {
		return
	}
	// Info first: if the recipe changes between the two reads the form carries
	// the older version and the save is refused as stale, never the reverse.
	info, err := h.Recipes.Info(r.Context(), id)
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
		return
	}
	rec, err := h.Recipes.Get(r.Context(), id)
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
		return
	}
	names, err := h.names(r.Context())
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	d := fromRecipe(rec, names)
	d.Version = info.Version
	h.render(w, r, http.StatusOK, id, d, names)
}

func (h *handler) post(w http.ResponseWriter, r *http.Request) {
	id, ok := recipeID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		layout.WriteError(w, r, fmt.Errorf("%w: read form: %w", layout.ErrInvalid, err))
		return
	}
	form := r.PostForm
	version, err := strconv.ParseInt(form.Get(fieldVersion), 10, 64)
	if err != nil || version <= 0 {
		layout.WriteError(w, r, fmt.Errorf("%w: form version %q", layout.ErrInvalid, form.Get(fieldVersion)))
		return
	}
	base, err := h.Recipes.Get(r.Context(), id)
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
		return
	}
	names, err := h.names(r.Context())
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	d := parseForm(form)
	d.Version = version
	d.arrange()

	action := form.Get(fieldAction)
	if action != "" && action != actionSave {
		if err := d.apply(action); err != nil {
			layout.WriteError(w, r, fmt.Errorf("%w: %w", layout.ErrInvalid, err))
			return
		}
		h.render(w, r, http.StatusOK, id, d, names)
		return
	}
	h.save(w, r, id, base, d, names)
}

// save validates the draft, proposes any new catalog entries it asked for and
// writes the recipe. A draft that is rejected, for any reason, is shown again
// with what the user typed and the stored recipe untouched.
func (h *handler) save(w http.ResponseWriter, r *http.Request, id int64, base recipe.Recipe, d draft, names lookups) {
	ctx := r.Context()
	rec, proposals, _, ok, err := h.build(ctx, &d, base)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	if !ok {
		h.render(w, r, rejectedStatus(r), id, d, names)
		return
	}
	if len(proposals) > 0 {
		// Catch the common stale save before it adds anything to the catalog.
		// A change landing after this check is still refused by
		// UpdateIfVersion below, with the proposed entries left in the catalog.
		info, err := h.Recipes.Info(ctx, id)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
			return
		}
		if info.Version != d.Version {
			h.stale(w, r, id, d, names, fmt.Errorf("%w: recipe %d is at version %d, not %d", store.ErrStaleVersion, id, info.Version, d.Version))
			return
		}
	}
	if err := h.propose(ctx, &rec, proposals); err != nil {
		layout.WriteError(w, r, err)
		return
	}
	err = h.Recipes.UpdateIfVersion(ctx, id, d.Version, rec)
	switch {
	case errors.Is(err, store.ErrStaleVersion):
		h.stale(w, r, id, d, names, err)
		return
	case err != nil:
		layout.WriteError(w, r, fmt.Errorf("save recipe %d: %w", id, err))
		return
	}
	h.keepThumbnail(ctx, id, rec.VideoID)
	to := recipePath(id)
	if FromHTMX(r) {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// keepThumbnail downloads the preview image of the saved recipe's video when
// none is stored for it yet (ADR-00021 decision 3). The recipe is already
// saved, so a failure is logged and the page shows the placeholder; the next
// save of a recipe with this video tries again.
func (h *handler) keepThumbnail(ctx context.Context, id int64, videoID string) {
	if videoID == "" {
		return
	}
	var src video.ThumbnailSource = video.NewFetcher()
	if h.Thumbnails != nil {
		src = h.Thumbnails
	}
	if err := video.EnsureThumbnail(ctx, h.Recipes, src, videoID); err != nil {
		slog.WarnContext(ctx, "recipe saved without a video thumbnail", "recipe_id", id, "video_id", videoID, "error", err.Error())
	}
}

// stale shows the form again with the stale-save message.
func (h *handler) stale(w http.ResponseWriter, r *http.Request, id int64, d draft, names lookups, cause error) {
	slog.WarnContext(r.Context(), "stale editor save", "recipe_id", id, "form_version", d.Version, "error", cause.Error())
	d.Problems = append(d.Problems, staleMessage)
	h.render(w, r, staleStatus(r), id, d, names)
}

// rejectedStatus is the status of a form shown again with messages: htmx 2
// does not swap a 4xx, so a fragment is a 200 and a plain form gets the
// status.
func rejectedStatus(r *http.Request) int {
	if FromHTMX(r) {
		return http.StatusOK
	}
	return http.StatusBadRequest
}

func staleStatus(r *http.Request) int {
	if FromHTMX(r) {
		return http.StatusOK
	}
	return layout.StatusFor(store.ErrStaleVersion)
}

func (h *handler) render(w http.ResponseWriter, r *http.Request, status int, id int64, d draft, names lookups) {
	v := view{ID: id, D: d, Names: names}
	if FromHTMX(r) {
		layout.Render(w, r, status, editorForm(v))
		return
	}
	layout.Render(w, r, status, editorPage(v))
}

// names loads the catalog names the form suggests and uses to label ids.
func (h *handler) names(ctx context.Context) (lookups, error) {
	ings, err := h.Catalog.ListIngredients(ctx)
	if err != nil {
		return lookups{}, fmt.Errorf("list ingredients: %w", err)
	}
	eq, err := h.Catalog.ListEquipment(ctx)
	if err != nil {
		return lookups{}, fmt.Errorf("list equipment: %w", err)
	}
	units, err := h.Catalog.ListUnits(ctx)
	if err != nil {
		return lookups{}, fmt.Errorf("list units: %w", err)
	}
	l := lookups{ingredient: map[int64]string{}, equipment: map[int64]string{}}
	for _, i := range ings {
		l.Ingredients = append(l.Ingredients, i.Name)
		l.ingredient[i.ID] = i.Name
	}
	for _, e := range eq {
		l.Equipment = append(l.Equipment, e.Name)
		l.equipment[e.ID] = e.Name
	}
	for _, u := range units {
		l.Units = append(l.Units, u.Unit.Name)
	}
	return l, nil
}

func recipeID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		layout.WriteError(w, r, fmt.Errorf("recipe id %q: %w", raw, layout.ErrNotFound))
		return 0, false
	}
	return id, true
}

// FromHTMX reports whether r was sent by htmx, which swaps the answer into
// the page instead of loading it.
func FromHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

func recipePath(id int64) string { return "/recipes/" + strconv.FormatInt(id, 10) }

func editPath(id int64) string { return recipePath(id) + "/edit" }
