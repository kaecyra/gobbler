package catalog

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	apicatalog "github.com/kaecyra/gobbler/internal/api/catalog"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// CentralPattern is where the router mounts CentralRoutes. It sits under
// /operator, which layout.OperatorPaths lists, so only an operator reaches it.
const CentralPattern = "/operator/ingredients"

// Central is what the operator's central ingredient page uses from the
// catalog: the operator operations of the catalog service, bound to the
// request's Caller (apicatalog.Bound). The service checks the person is an
// operator and runs every call in the central scope, whether or not the
// operator is also a member of a household (ADR-00043).
type Central interface {
	OperatorListIngredients(ctx context.Context) ([]apicatalog.Ingredient, error)
	OperatorGetIngredient(ctx context.Context, id int64) (apicatalog.Ingredient, error)
	OperatorUpdateIngredient(ctx context.Context, in apicatalog.Ingredient) error
}

// CentralDeps is what the central ingredient routes need.
type CentralDeps struct {
	Catalog Central
}

type centralHandler struct{ CentralDeps }

// CentralRoutes returns the operator's editor of the central ingredient layer,
// relative to CentralPattern: the list, with the same filter as the catalog
// page, and an edit form for the name, aliases, aisle and density of one entry.
// The operator is the only curator of the central layer (ADR-00018, Catalog).
// There is no merge and no delete here, and the USDA id is kept as the import
// set it.
func CentralRoutes(d CentralDeps) chi.Router {
	h := &centralHandler{d}
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Get("/{id}", h.editShow)
	r.Post("/{id}", h.editSave)
	return r
}

func (h *centralHandler) list(w http.ResponseWriter, r *http.Request) {
	if !operatorOnly(w, r) {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	all, err := h.Catalog.OperatorListIngredients(r.Context())
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("list central ingredients: %w", err))
		return
	}
	layout.Render(w, r, http.StatusOK, centralListPage(centralListView{Query: q, Rows: rowsOf(filter(all, q), nil), Total: len(all)}))
}

func (h *centralHandler) editShow(w http.ResponseWriter, r *http.Request) {
	if !operatorOnly(w, r) {
		return
	}
	id, err := idParam(chi.URLParam(r, "id"))
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	in, err := h.Catalog.OperatorGetIngredient(r.Context(), id)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	f := formFrom(in)
	f.Curator = true
	h.renderEdit(w, r, http.StatusOK, f)
}

// editSave saves the form over the central entry. The USDA id and the reviewed
// flag are not on the form, so they are taken from the stored entry rather
// than from whatever the request carries.
func (h *centralHandler) editSave(w http.ResponseWriter, r *http.Request) {
	if !operatorOnly(w, r) {
		return
	}
	id, err := idParam(chi.URLParam(r, "id"))
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	if !parseForm(w, r) {
		return
	}
	cur, err := h.Catalog.OperatorGetIngredient(r.Context(), id)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	f := formFromRequest(id, r)
	f.USDAID = formFrom(cur).USDAID
	f.Reviewed = cur.Reviewed
	f.Central, f.Curator = true, true
	in, problems := f.ingredient()
	if len(problems) > 0 {
		f.Problems = problems
		h.renderEdit(w, r, http.StatusBadRequest, f)
		return
	}
	if err := h.Catalog.OperatorUpdateIngredient(r.Context(), in); err != nil {
		if field, msg, ok := inlineMessage(err); ok {
			f.Problems = map[string]string{field: msg}
			h.renderEdit(w, r, layout.StatusFor(err), f)
			return
		}
		layout.WriteError(w, r, fmt.Errorf("save central ingredient %d: %w", id, err))
		return
	}
	http.Redirect(w, r, CentralPattern, http.StatusSeeOther)
}

// renderEdit shows the form with the central layer's aisles as suggestions.
func (h *centralHandler) renderEdit(w http.ResponseWriter, r *http.Request, status int, f formView) {
	all, err := h.Catalog.OperatorListIngredients(r.Context())
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("list central ingredients: %w", err))
		return
	}
	f.Aisles = aislesOf(all)
	layout.Render(w, r, status, centralEditPage(f))
}

// centralListView is the operator's list of central ingredients, narrowed by
// the filter. Total is the number before the filter, so an empty result can
// say whether the layer or the filter is what is empty.
type centralListView struct {
	Query string
	Rows  []row
	Total int
}
