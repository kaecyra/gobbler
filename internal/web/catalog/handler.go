// Package catalog is the catalog cleanup page: the ingredients ingestion
// proposed, listed for review, with an edit form and a merge flow (ADR-00008).
// Persistence and the merge itself belong to internal/catalog; this package is
// the page over them. It never creates an ingredient: creation stays on the
// ingest review screen. It is mounted from internal/web (ADR-00012).
package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	domain "github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes.
const Pattern = "/catalog"

// maxFormBytes bounds a form body; an ingredient is a few short fields.
const maxFormBytes = 64 << 10

// Catalog is what the routes use from the catalog store. *catalog.Store
// satisfies it.
type Catalog interface {
	ListIngredients(ctx context.Context) ([]domain.Ingredient, error)
	ListUnreviewedIngredients(ctx context.Context) ([]domain.Ingredient, error)
	GetIngredient(ctx context.Context, id int64) (domain.Ingredient, error)
	UpdateIngredient(ctx context.Context, in domain.Ingredient) error
	MergeIngredients(ctx context.Context, winnerID, loserID int64) (domain.Ingredient, error)
}

// Usage counts what a merge would re-point. *catalog.Usage satisfies it. The
// count comes from the tables' owners through the catalog, never from here.
type Usage interface {
	IngredientUsage(ctx context.Context, id int64) (int64, error)
}

// Deps is what the routes need.
type Deps struct {
	Catalog Catalog
	Usage   Usage
}

// Routes returns the routes of the page, relative to Pattern.
func Routes(d Deps) chi.Router {
	h := &handler{Deps: d}
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Get("/merge", h.mergeShow)
	r.Post("/merge", h.mergeDo)
	r.Get("/{id}", h.editShow)
	r.Post("/{id}", h.editSave)
	r.Post("/{id}/reviewed", h.markReviewed)
	return r
}

type handler struct{ Deps }

// idParam parses a positive ingredient id; anything else is a bad request.
func idParam(s string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("ingredient id %q: %w", s, layout.ErrInvalid)
	}
	return id, nil
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	unreviewed, err := h.Catalog.ListUnreviewedIngredients(ctx)
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("list unreviewed ingredients: %w", err))
		return
	}
	all, err := h.Catalog.ListIngredients(ctx)
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("list ingredients: %w", err))
		return
	}
	layout.Render(w, r, http.StatusOK, listPage(newListView(q, unreviewed, all)))
}

func (h *handler) editShow(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(chi.URLParam(r, "id"))
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	in, err := h.Catalog.GetIngredient(r.Context(), id)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	h.renderEdit(w, r, http.StatusOK, formFrom(in))
}

func (h *handler) editSave(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(chi.URLParam(r, "id"))
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	if !parseForm(w, r) {
		return
	}
	f := formFromRequest(id, r)
	in, problems := f.ingredient()
	if len(problems) > 0 {
		f.Problems = problems
		h.renderEdit(w, r, http.StatusBadRequest, f)
		return
	}
	if err := h.Catalog.UpdateIngredient(r.Context(), in); err != nil {
		if field, msg, ok := inlineMessage(err); ok {
			f.Problems = map[string]string{field: msg}
			h.renderEdit(w, r, layout.StatusFor(err), f)
			return
		}
		layout.WriteError(w, r, fmt.Errorf("save ingredient %d: %w", id, err))
		return
	}
	http.Redirect(w, r, Pattern, http.StatusSeeOther)
}

// markReviewed sets the reviewed flag and nothing else, from the list.
func (h *handler) markReviewed(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(chi.URLParam(r, "id"))
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	in, err := h.Catalog.GetIngredient(r.Context(), id)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	in.Reviewed = true
	if err := h.Catalog.UpdateIngredient(r.Context(), in); err != nil {
		layout.WriteError(w, r, fmt.Errorf("mark ingredient %d reviewed: %w", id, err))
		return
	}
	http.Redirect(w, r, Pattern, http.StatusSeeOther)
}

func (h *handler) renderEdit(w http.ResponseWriter, r *http.Request, status int, f formView) {
	all, err := h.Catalog.ListIngredients(r.Context())
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("list ingredients: %w", err))
		return
	}
	f.Aisles = aislesOf(all)
	layout.Render(w, r, status, editPage(f))
}

// mergeShow shows the target chooser, or, once source and target are both
// given, the confirmation with the number of uses (recipe lines, shopping rows)
// the merge re-points.
func (h *handler) mergeShow(w http.ResponseWriter, r *http.Request) {
	h.mergeStep(w, r, r.URL.Query().Get("source"), r.URL.Query().Get("target"))
}

// mergeDo performs the merge the confirmation described.
func (h *handler) mergeDo(w http.ResponseWriter, r *http.Request) {
	if !parseForm(w, r) {
		return
	}
	src, dst, ok := h.mergePair(w, r, r.PostForm.Get("source"), r.PostForm.Get("target"))
	if !ok {
		return
	}
	if _, err := h.Catalog.MergeIngredients(r.Context(), dst.ID, src.ID); err != nil {
		layout.WriteError(w, r, fmt.Errorf("merge ingredient %d into %d: %w", src.ID, dst.ID, err))
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s/%d", Pattern, dst.ID), http.StatusSeeOther)
}

// mergePair parses and loads source and target. A bad pair has already been
// answered when ok is false: a self-merge with the chooser and its message, a
// missing ingredient or an unreadable id with the error page.
func (h *handler) mergePair(w http.ResponseWriter, r *http.Request, source, target string) (src, dst domain.Ingredient, ok bool) {
	sourceID, err := idParam(source)
	if err != nil {
		layout.WriteError(w, r, err)
		return src, dst, false
	}
	targetID, err := idParam(target)
	if err != nil {
		layout.WriteError(w, r, err)
		return src, dst, false
	}
	if sourceID == targetID {
		h.renderChooser(w, r, http.StatusBadRequest, sourceID, selfMergeMessage)
		return src, dst, false
	}
	if src, err = h.Catalog.GetIngredient(r.Context(), sourceID); err != nil {
		layout.WriteError(w, r, err)
		return src, dst, false
	}
	if dst, err = h.Catalog.GetIngredient(r.Context(), targetID); err != nil {
		layout.WriteError(w, r, err)
		return src, dst, false
	}
	return src, dst, true
}

func (h *handler) mergeStep(w http.ResponseWriter, r *http.Request, source, target string) {
	if strings.TrimSpace(target) == "" {
		sourceID, err := idParam(source)
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		h.renderChooser(w, r, http.StatusOK, sourceID, "")
		return
	}
	src, dst, ok := h.mergePair(w, r, source, target)
	if !ok {
		return
	}
	ctx := r.Context()
	uses, err := h.Usage.IngredientUsage(ctx, src.ID)
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("count references of ingredient %d: %w", src.ID, err))
		return
	}
	layout.Render(w, r, http.StatusOK, confirmPage(confirmView{Source: src, Target: dst, Uses: uses}))
}

func (h *handler) renderChooser(w http.ResponseWriter, r *http.Request, status int, sourceID int64, problem string) {
	src, err := h.Catalog.GetIngredient(r.Context(), sourceID)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	all, err := h.Catalog.ListIngredients(r.Context())
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("list ingredients: %w", err))
		return
	}
	layout.Render(w, r, status, chooserPage(newChooserView(src, all, problem)))
}

// parseForm bounds and parses the body, answering a malformed one itself.
func parseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		layout.WriteError(w, r, fmt.Errorf("read form: %w", errors.Join(layout.ErrInvalid, err)))
		return false
	}
	return true
}
