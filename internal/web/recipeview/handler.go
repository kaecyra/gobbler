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

	"github.com/kaecyra/gobbler/internal/api/caller"
	apicatalog "github.com/kaecyra/gobbler/internal/api/catalog"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipe/display"
	"github.com/kaecyra/gobbler/internal/recipepath"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes. Other recipe features mount under
// the same prefix; this one owns the view URL, whose {id} segment is
// {id}-{slug} (ADR-00047). Its other routes take a plain numeric id.
const Pattern = "/recipes/{id}"

// Recipes is what the view reads from the recipe service
// (*apirecipes.Service satisfies it).
type Recipes interface {
	Get(ctx context.Context, c apirecipes.Caller, id int64) (recipe.Recipe, error)
	Info(ctx context.Context, c apirecipes.Caller, id int64) (apirecipes.Info, error)
	// HasThumbnail and Thumbnail serve the preview image of the recipe's video
	// (ADR-00021). Thumbnail is by recipe, never by video ID.
	HasThumbnail(ctx context.Context, c apirecipes.Caller, videoID string) (bool, error)
	Thumbnail(ctx context.Context, c apirecipes.Caller, id int64) (apirecipes.Thumbnail, error)
	// ListNotes and ListMadeIt feed the tips (pinned notes) and the made
	// count on the page (ADR-00022).
	ListNotes(ctx context.Context, c apirecipes.Caller, id int64) ([]apirecipes.Note, error)
	ListMadeIt(ctx context.Context, c apirecipes.Caller, id int64) ([]apirecipes.MadeIt, error)
	// PendingEdit, GetProposal and RejectProposal show and turn down an agent's
	// proposed edit to the recipe (ADR-00035).
	PendingEdit(ctx context.Context, c apirecipes.Caller, id int64) (apirecipes.Proposal, bool, error)
	GetProposal(ctx context.Context, c apirecipes.Caller, token string) (apirecipes.Proposal, error)
	RejectProposal(ctx context.Context, c apirecipes.Caller, token string) error
	// PendingReimport finds the recipe's pending re-import from its source
	// (ADR-00042), which the page announces in a banner.
	PendingReimport(ctx context.Context, c apirecipes.Caller, id int64) (apirecipes.Proposal, bool, error)
}

// Catalog is what the view reads from the ingredient and equipment catalog.
// The router gives it the catalog service bound to the request's Caller
// (apicatalog.Bound), which checks membership on every call (ADR-00043).
type Catalog interface {
	GetIngredient(ctx context.Context, id int64) (apicatalog.Ingredient, error)
	GetEquipment(ctx context.Context, id int64) (apicatalog.Equipment, error)
}

// Deps is what the routes need.
type Deps struct {
	Recipes Recipes
	Catalog Catalog
	// Pictures gives the address of the recipe's picture. Nil shows the
	// placeholder band for every recipe.
	Pictures Pictures
}

// Pictures finds the picture of a recipe (ADR-00039). The picture package's
// service, through internal/web/picture.Srcs, satisfies it.
type Pictures interface {
	// Src is the address of recipe id's picture, empty when it has none.
	Src(ctx context.Context, id int64) (string, error)
}

// Routes returns the routes of the recipe view, relative to Pattern.
func Routes(d Deps) chi.Router {
	r := chi.NewRouter()
	r.Get("/", handle(d))
	r.Get(thumbnailRoute, thumbnail(d))
	r.Post(rejectRoute, reject(d))
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
		id, err := recipeID(r)
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		t, err := d.Recipes.Thumbnail(r.Context(), caller.FromContext(r.Context()), id)
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

// recipeID is the recipe the request's path names, or an ErrNotFound for an
// id that cannot name one.
func recipeID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("recipe id %q: %w", chi.URLParam(r, "id"), layout.ErrNotFound)
	}
	return id, nil
}

// viewID is the recipe the view page's path segment names. The segment is
// {id}-{slug} (ADR-00047); the slug is ignored here.
func viewID(r *http.Request) (int64, error) {
	seg := chi.URLParam(r, "id")
	id, err := recipepath.ParseSegment(seg)
	if err != nil {
		return 0, fmt.Errorf("recipe path %q: %w", seg, layout.ErrNotFound)
	}
	return id, nil
}

func handle(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := viewID(r)
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		rec, err := d.Recipes.Get(r.Context(), caller.FromContext(r.Context()), id)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
			return
		}
		// Only after Get has checked access: a redirect names the title, which
		// a caller who cannot see the recipe must not learn (ADR-00047). The
		// route answers GET only. An HTMX request is redirected the same way:
		// the browser follows a 301 inside the request and htmx swaps the
		// canonical page.
		if want := recipepath.Segment(id, rec.Name); chi.URLParam(r, "id") != want {
			to := recipepath.Prefix + want
			if r.URL.RawQuery != "" {
				to += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, to, http.StatusMovedPermanently)
			return
		}
		info, err := d.Recipes.Info(r.Context(), caller.FromContext(r.Context()), id)
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
		if d.Pictures != nil {
			// A recipe deleted since it was read has no picture to show: an
			// answer, not a failure. Any other error is one.
			v.Picture, err = d.Pictures.Src(r.Context(), id)
			if err != nil && !errors.Is(err, apirecipes.ErrNotFound) {
				layout.WriteError(w, r, fmt.Errorf("find picture of recipe %d: %w", id, err))
				return
			}
		}
		if rec.VideoID != "" {
			has, err := d.Recipes.HasThumbnail(r.Context(), caller.FromContext(r.Context()), rec.VideoID)
			if err != nil {
				layout.WriteError(w, r, fmt.Errorf("check thumbnail of recipe %d: %w", id, err))
				return
			}
			v.HasThumbnail = has
		}
		notes, err := d.Recipes.ListNotes(r.Context(), caller.FromContext(r.Context()), id)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("load notes of recipe %d: %w", id, err))
			return
		}
		made, err := d.Recipes.ListMadeIt(r.Context(), caller.FromContext(r.Context()), id)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("load made-it log of recipe %d: %w", id, err))
			return
		}
		v.withJournal(notes, made)
		if info.ClonedFrom != nil {
			v.Origin = &originView{RecipeID: info.ClonedFrom.RecipeID, Version: info.ClonedFrom.Version}
		}
		if v.Proposal, err = proposalFor(r.Context(), d, id, rec, info.Version); err != nil {
			layout.WriteError(w, r, err)
			return
		}
		v.CanReimport = rec.SourceURL != ""
		if v.Reimport, err = reimportFor(r.Context(), d, id, rec.SourceURL, info.Version); err != nil {
			layout.WriteError(w, r, err)
			return
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
