package catalog

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	domain "github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// EquipmentPattern is where the router mounts EquipmentRoutes. It sits under
// /operator, which layout.OperatorPaths lists, so only an operator reaches it.
const EquipmentPattern = "/operator/equipment"

// Equipment is what the equipment review uses from the catalog store.
// *catalog.Store satisfies it.
type Equipment interface {
	ListEquipment(ctx context.Context) ([]domain.Equipment, error)
	GetEquipment(ctx context.Context, id int64) (domain.Equipment, error)
	UpdateEquipment(ctx context.Context, in domain.Equipment) error
}

// EquipmentDeps is what the equipment routes need.
type EquipmentDeps struct {
	Catalog Equipment
}

type equipmentHandler struct{ EquipmentDeps }

// EquipmentRoutes returns the operator's review of catalog equipment, relative
// to EquipmentPattern: the list, and the control that sets or clears the sized
// flag behind the scaled view's larger-pan hint (ADR-00019). Equipment is
// central, so the operator is its only curator (ADR-00018, Catalog).
func EquipmentRoutes(d EquipmentDeps) chi.Router {
	h := &equipmentHandler{d}
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Post("/{id}/sized", h.setSized)
	return r
}

// operatorOnly answers a request that does not come from an operator. Package
// web has already refused everyone else under layout.OperatorPaths; this is the
// second lock, so a route mounted outside them by mistake still refuses.
func operatorOnly(w http.ResponseWriter, r *http.Request) bool {
	if layout.IsOperator(r.Context()) {
		return true
	}
	layout.Render(w, r, http.StatusForbidden, layout.ErrorPage(http.StatusForbidden, "This page is for the operator."))
	return false
}

func (h *equipmentHandler) list(w http.ResponseWriter, r *http.Request) {
	if !operatorOnly(w, r) {
		return
	}
	all, err := h.Catalog.ListEquipment(r.Context())
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("list equipment: %w", err))
		return
	}
	layout.Render(w, r, http.StatusOK, equipmentPage(newEquipmentView(all)))
}

// setSized sets or clears the flag, to the value the form names, and changes
// nothing else about the row. Naming the target state rather than toggling
// keeps a repeated or stale submit harmless.
func (h *equipmentHandler) setSized(w http.ResponseWriter, r *http.Request) {
	if !operatorOnly(w, r) {
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(chi.URLParam(r, "id")), 10, 64)
	if err != nil || id <= 0 {
		layout.WriteError(w, r, fmt.Errorf("equipment id %q: %w", chi.URLParam(r, "id"), layout.ErrInvalid))
		return
	}
	if !parseForm(w, r) {
		return
	}
	var sized bool
	switch v := r.PostForm.Get("sized"); v {
	case "1":
		sized = true
	case "0":
	default:
		layout.WriteError(w, r, fmt.Errorf("sized %q must be 1 or 0: %w", v, layout.ErrInvalid))
		return
	}
	eq, err := h.Catalog.GetEquipment(r.Context(), id)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	eq.Sized = sized
	if err := h.Catalog.UpdateEquipment(r.Context(), eq); err != nil {
		layout.WriteError(w, r, fmt.Errorf("set equipment %d sized: %w", id, err))
		return
	}
	http.Redirect(w, r, EquipmentPattern, http.StatusSeeOther)
}

type equipmentRow struct {
	ID      int64
	Name    string
	Aliases string
	Sized   bool
}

type equipmentView struct {
	Rows []equipmentRow
}

func newEquipmentView(all []domain.Equipment) equipmentView {
	var v equipmentView
	for _, e := range all {
		v.Rows = append(v.Rows, equipmentRow{ID: e.ID, Name: e.Name, Aliases: strings.Join(e.Aliases, ", "), Sized: e.Sized})
	}
	return v
}
