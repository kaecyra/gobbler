// Package editor is the recipe editor: a structured form for the metadata,
// components, ingredient lines with typed qualifiers, equipment and steps with
// their dependencies (ADR-00004). Quantities are read by package quantity
// (ADR-00005), ingredients, units and equipment are chosen from the catalog
// (ADR-00008), and the step graph is checked by package recipe. Saving goes
// through the recipe service (internal/api/recipes), whose store writes the revision holding the prior
// document; the editor never writes SQL and never touches a source snapshot.
// The author of an edit is the verified identity on the request context
// (ADR-00030). It is mounted from internal/web (ADR-00012). The same form is
// the ingest review screen over an unsaved draft (Unsaved, ADR-00020 decision 3).
package editor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apicatalog "github.com/kaecyra/gobbler/internal/api/catalog"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipepath"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes: the editor of a recipe is the URL
// below the recipe's own page.
const Pattern = "/recipes/{id}/edit"

// scriptPath is the editor's script: drag to reorder, opened rows kept open,
// focus on a new control. The form works without it.
const scriptPath = "/static/editor.js"

// morphPath is idiomorph's htmx extension (ADR-00038).
const morphPath = "/static/vendor/idiomorph-ext.min.js"

// maxFormBytes bounds an editor form body; a long recipe is still prose.
const maxFormBytes = 1 << 20

// Recipes is what the editor needs from the recipe service
// (*apirecipes.Service satisfies it).
type Recipes interface {
	Get(ctx context.Context, c apirecipes.Caller, id int64) (recipe.Recipe, error)
	Info(ctx context.Context, c apirecipes.Caller, id int64) (apirecipes.Info, error)
	UpdateIfVersion(ctx context.Context, c apirecipes.Caller, id, expectedVersion int64, r recipe.Recipe) error
	// GetProposal and AcceptEdit review an agent's proposed edit in this editor
	// (ADR-00035): saving the form with a proposal's token accepts it.
	GetProposal(ctx context.Context, c apirecipes.Caller, token string) (apirecipes.Proposal, error)
	AcceptEdit(ctx context.Context, c apirecipes.Caller, token string, expectedVersion int64, r recipe.Recipe) error
	// KeepThumbnail keeps the preview image of a saved recipe's video
	// (ADR-00021).
	KeepThumbnail(ctx context.Context, c apirecipes.Caller, id int64, videoID string)
}

// Catalog is what the editor needs from the catalog: matching what the user
// typed, listing names to suggest, and proposing an unreviewed entry. The
// router gives it the catalog service bound to the request's Caller
// (apicatalog.Bound), which checks membership on every call (ADR-00043).
type Catalog interface {
	MatchIngredient(ctx context.Context, term string) (apicatalog.Ingredient, bool, error)
	MatchEquipment(ctx context.Context, term string) (apicatalog.Equipment, bool, error)
	MatchUnit(ctx context.Context, term string) (apicatalog.Unit, bool, error)
	ListIngredients(ctx context.Context) ([]apicatalog.Ingredient, error)
	ListEquipment(ctx context.Context) ([]apicatalog.Equipment, error)
	ListUnits(ctx context.Context) ([]apicatalog.Unit, error)
	CreateIngredient(ctx context.Context, in apicatalog.Ingredient) (apicatalog.Ingredient, error)
	CreateEquipment(ctx context.Context, in apicatalog.Equipment) (apicatalog.Equipment, error)
}

// Deps is what the routes need.
type Deps struct {
	Recipes Recipes
	Catalog Catalog
	// Pictures is the edit page's picture picker (ADR-00051). Nil leaves the
	// page without one.
	Pictures Pictures
}

// Routes returns the routes of the editor, relative to Pattern.
func Routes(d Deps) chi.Router {
	h := &handler{Deps: d}
	r := chi.NewRouter()
	r.Get("/", h.show)
	r.Post("/", h.post)
	if d.Pictures != nil {
		r.Post("/picture/search", h.searchPictures)
		r.Post("/picture/upload", h.uploadPicture)
	}
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
	info, err := h.Recipes.Info(r.Context(), caller.FromContext(r.Context()), id)
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
		return
	}
	rec, err := h.Recipes.Get(r.Context(), caller.FromContext(r.Context()), id)
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
		return
	}
	names, err := h.names(r.Context())
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	token := strings.TrimSpace(r.URL.Query().Get(fieldProposal))
	var review *proposalView
	live := rec
	if token != "" {
		// The proposal is read after the version, as the recipe is: a save in
		// between leaves a form at the older version, refused on save.
		p, ok := h.openForReview(w, r, id, info.Version, token)
		if !ok {
			return
		}
		rec = *p.Edit
		review = summaryOf(live, p, names)
	}
	d := fromRecipe(rec, names)
	d.Version = info.Version
	d.Proposal, d.review = token, review
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
	base, err := h.Recipes.Get(r.Context(), caller.FromContext(r.Context()), id)
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
	if d.Proposal != "" {
		// What the form does not edit comes from the proposal, as it comes from
		// the stored recipe otherwise.
		p, ok := h.openForReview(w, r, id, version, d.Proposal)
		if !ok {
			return
		}
		d.review = summaryOf(base, p, names)
		base = *p.Edit
	}

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
	choice, err := pictureChoice(r.PostForm)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
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
		info, err := h.Recipes.Info(ctx, caller.FromContext(ctx), id)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
			return
		}
		if info.Version != d.Version {
			h.stale(w, r, id, d, names, fmt.Errorf("%w: recipe %d is at version %d, not %d", apirecipes.ErrStaleVersion, id, info.Version, d.Version), staleMessage)
			return
		}
	}
	if err := h.propose(ctx, &rec, proposals); err != nil {
		layout.WriteError(w, r, err)
		return
	}
	err = h.saveWithPicture(ctx, id, choice, func(ctx context.Context) error {
		if d.Proposal != "" {
			return h.Recipes.AcceptEdit(ctx, caller.FromContext(ctx), d.Proposal, d.Version, rec)
		}
		return h.Recipes.UpdateIfVersion(ctx, caller.FromContext(ctx), id, d.Version, rec)
	})
	if errors.Is(err, apirecipes.ErrOfferGone) {
		d.Problems = append(d.Problems, offerGone)
		h.render(w, r, rejectedStatus(r), id, d, names)
		return
	}
	if errors.Is(err, apirecipes.ErrPictureNotApplied) {
		// The recipe is saved; say so, after keeping its video's preview.
		h.Recipes.KeepThumbnail(ctx, caller.FromContext(ctx), id, rec.VideoID)
		layout.WriteError(w, r, fmt.Errorf("save recipe %d: %w", id, err))
		return
	}
	if msg, ok := acceptFailure(err); ok {
		h.stale(w, r, id, d, names, err, msg)
		return
	}
	switch {
	case errors.Is(err, apirecipes.ErrStaleVersion):
		h.stale(w, r, id, d, names, err, staleMessage)
		return
	case err != nil:
		layout.WriteError(w, r, fmt.Errorf("save recipe %d: %w", id, err))
		return
	}
	h.Recipes.KeepThumbnail(ctx, caller.FromContext(ctx), id, rec.VideoID)
	to := recipepath.View(id, rec.Name)
	if FromHTMX(r) {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// stale shows the form again with the message for a save that was refused
// because the recipe, or the proposal being reviewed, is out of date.
func (h *handler) stale(w http.ResponseWriter, r *http.Request, id int64, d draft, names lookups, cause error, message string) {
	slog.WarnContext(r.Context(), "stale editor save", "recipe_id", id, "form_version", d.Version, "error", cause.Error())
	d.Problems = append(d.Problems, message)
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
	return layout.StatusFor(apirecipes.ErrStaleVersion)
}

func (h *handler) render(w http.ResponseWriter, r *http.Request, status int, id int64, d draft, names lookups) {
	v := view{ID: id, D: d, Names: names}
	v.Proposal = d.review
	if FromHTMX(r) {
		layout.Render(w, r, status, editorForm(v))
		return
	}
	// The picker sits outside the form, so only a whole page draws it. A form
	// shown again keeps the picture it chose. A page a search without scripts
	// sent the browser back to shows that search's outcome and starts none.
	outcome := r.URL.Query().Get(searchParam)
	pic, err := h.picture(r.Context(), id, d.SourceURL, r.PostForm.Get(fieldPicture), outcome == "")
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	if pic != nil {
		pic.NothingFound = outcome == searchEmpty && len(pic.Offers) == 0
		pic.SearchFailed = outcome == searchFailed
	}
	v.Picture = pic
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

func editPath(id int64) string { return recipepath.Base(id) + "/edit" }
