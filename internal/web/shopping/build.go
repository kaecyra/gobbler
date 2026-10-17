package shopping

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	domain "github.com/kaecyra/gobbler/internal/shopping"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Messages the user can act on, shown inline next to the form that failed.
const (
	msgMultiplier = "The multiplier must be a number above zero, such as 2 or 1/2."
	msgDuplicate  = "That recipe is already on the list. Change its multiplier instead."
	msgReference  = "That recipe or ingredient no longer exists. Reload the page and choose again."
	msgManual     = "A manual item needs a name, or an ingredient chosen from the catalog."
	msgQuantity   = "The quantity must be a number such as 2, 1 1/2 or 2-3."
	msgUnit       = "That unit is not in the catalog."
	msgName       = "A list needs a name."
)

// indexView is the page of all lists.
type indexView struct {
	Lists   []domain.ListInfo
	Problem string
}

func (h *handler) index(w http.ResponseWriter, r *http.Request) {
	h.renderIndex(w, r, http.StatusOK, "")
}

func (h *handler) renderIndex(w http.ResponseWriter, r *http.Request, status int, problem string) {
	lists, err := h.Lists.Lists(r.Context())
	if err != nil {
		fail(w, r, fmt.Errorf("list shopping lists: %w", err))
		return
	}
	layout.Render(w, r, status, indexPage(indexView{Lists: lists, Problem: problem}))
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	if !parseForm(w, r) {
		return
	}
	info, err := h.Lists.CreateList(r.Context(), r.PostForm.Get("name"))
	if errors.Is(err, domain.ErrInvalid) {
		h.renderIndex(w, r, http.StatusBadRequest, msgName)
		return
	}
	if err != nil {
		fail(w, r, fmt.Errorf("create shopping list: %w", err))
		return
	}
	http.Redirect(w, r, listURL(info.ID), http.StatusSeeOther)
}

func (h *handler) build(w http.ResponseWriter, r *http.Request) {
	id, err := listID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	h.renderBuild(w, r, id, http.StatusOK, "")
}

// renderBuild loads list id and renders the build page with problem, if any,
// next to the forms.
func (h *handler) renderBuild(w http.ResponseWriter, r *http.Request, id int64, status int, problem string) {
	ctx := r.Context()
	view, err := h.Lists.Load(ctx, id)
	if err != nil {
		fail(w, r, fmt.Errorf("load list %d: %w", id, err))
		return
	}
	ingredients, err := h.Catalog.ListIngredients(ctx)
	if err != nil {
		fail(w, r, fmt.Errorf("list ingredients: %w", err))
		return
	}
	units, err := h.Catalog.ListUnits(ctx)
	if err != nil {
		fail(w, r, fmt.Errorf("list units: %w", err))
		return
	}
	recipes, err := h.Recipes.Search(ctx, store.SearchQuery{})
	if err != nil {
		fail(w, r, fmt.Errorf("list recipes: %w", err))
		return
	}
	group, err := groupOf(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	list, err := newListing(view, group)
	if err != nil {
		fail(w, r, err)
		return
	}
	dropped, err := newDropped(view.Pantry)
	if err != nil {
		fail(w, r, err)
		return
	}
	manual, err := newManual(view.Manual, ingredientNames(ingredients))
	if err != nil {
		fail(w, r, err)
		return
	}
	text, err := view.Generated().Text()
	if err != nil {
		fail(w, r, fmt.Errorf("export list %d: %w", id, err))
		return
	}
	v := buildView{
		ID: id, Name: view.List.Name, IncludeOptional: view.IncludeOptional, Entries: newEntries(view.Entries), Manual: manual,
		List: list, Dropped: dropped, Export: text,
		Ingredients: ingredientOptions(ingredients), Problem: problem,
	}
	for _, rs := range recipes {
		v.Recipes = append(v.Recipes, option{Value: strconv.FormatInt(rs.ID, 10), Label: rs.Name})
	}
	for _, u := range units {
		v.Units = append(v.Units, u.Unit.Name)
	}
	layout.Render(w, r, status, buildPage(v))
}

// parseMultiplier reads a multiplier field; empty means 1.
func parseMultiplier(s string) (quantity.Rat, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return quantity.Int(1), true
	}
	m, err := quantity.ParseRat(s)
	if err != nil || m.Sign() <= 0 {
		return quantity.Rat{}, false
	}
	return m, true
}

// inline reports whether err is one the user fixes in the form that caused it,
// and the status and message to show. Anything else is an error page.
func inline(err error) (status int, msg string, ok bool) {
	switch {
	case errors.Is(err, domain.ErrInvalidMultiplier):
		return http.StatusBadRequest, msgMultiplier, true
	case errors.Is(err, domain.ErrInvalidManualItem):
		return http.StatusBadRequest, msgManual, true
	case errors.Is(err, domain.ErrDuplicate):
		return http.StatusConflict, msgDuplicate, true
	case errors.Is(err, domain.ErrUnknownReference):
		return http.StatusBadRequest, msgReference, true
	}
	return 0, "", false
}

// edited answers a successful edit of list id: it regenerates the list, which
// drops the check state of items the edit removed and keeps the rest, then goes
// back to the build page.
func (h *handler) edited(w http.ResponseWriter, r *http.Request, id int64) {
	if _, err := h.Lists.Regenerate(r.Context(), id); err != nil {
		fail(w, r, fmt.Errorf("regenerate list %d: %w", id, err))
		return
	}
	back(w, r, id, "")
}

// failedEdit answers a failed edit: inline when the user can fix it there, an
// error page otherwise.
func (h *handler) failedEdit(w http.ResponseWriter, r *http.Request, id int64, err error) {
	if status, msg, ok := inline(err); ok {
		h.renderBuild(w, r, id, status, msg)
		return
	}
	fail(w, r, err)
}

func (h *handler) addRecipe(w http.ResponseWriter, r *http.Request) {
	id, err := listID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	if !parseForm(w, r) {
		return
	}
	recipeID, err := idParam("recipe id", r.PostForm.Get("recipe"))
	if err != nil {
		h.renderBuild(w, r, id, http.StatusBadRequest, msgReference)
		return
	}
	mult, ok := parseMultiplier(r.PostForm.Get("multiplier"))
	if !ok {
		h.renderBuild(w, r, id, http.StatusBadRequest, msgMultiplier)
		return
	}
	if _, err := h.Lists.AddEntry(r.Context(), id, recipeID, mult); err != nil {
		h.failedEdit(w, r, id, fmt.Errorf("add recipe %d to list %d: %w", recipeID, id, err))
		return
	}
	h.edited(w, r, id)
}

// entryOf returns the entry id of the URL after checking it belongs to list id,
// so a URL naming one list cannot change another.
func (h *handler) entryOf(w http.ResponseWriter, r *http.Request, id int64) (int64, bool) {
	entry, err := idParam("entry id", chi.URLParam(r, "entry"))
	if err != nil {
		fail(w, r, err)
		return 0, false
	}
	view, err := h.Lists.Load(r.Context(), id)
	if err != nil {
		fail(w, r, fmt.Errorf("load list %d: %w", id, err))
		return 0, false
	}
	for _, e := range view.Entries {
		if e.ID == entry {
			return entry, true
		}
	}
	fail(w, r, fmt.Errorf("entry %d is not on list %d: %w", entry, id, layout.ErrNotFound))
	return 0, false
}

func (h *handler) setMultiplier(w http.ResponseWriter, r *http.Request) {
	id, err := listID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	if !parseForm(w, r) {
		return
	}
	entry, ok := h.entryOf(w, r, id)
	if !ok {
		return
	}
	mult, ok := parseMultiplier(r.PostForm.Get("multiplier"))
	if !ok {
		h.renderBuild(w, r, id, http.StatusBadRequest, msgMultiplier)
		return
	}
	if err := h.Lists.SetEntryMultiplier(r.Context(), entry, mult); err != nil {
		h.failedEdit(w, r, id, fmt.Errorf("set multiplier of entry %d: %w", entry, err))
		return
	}
	h.edited(w, r, id)
}

func (h *handler) removeRecipe(w http.ResponseWriter, r *http.Request) {
	id, err := listID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	entry, ok := h.entryOf(w, r, id)
	if !ok {
		return
	}
	if err := h.Lists.RemoveEntry(r.Context(), entry); err != nil {
		fail(w, r, fmt.Errorf("remove entry %d: %w", entry, err))
		return
	}
	h.edited(w, r, id)
}

// manualItem reads the manual item form. A problem is a message for the user.
func (h *handler) manualItem(r *http.Request) (domain.ManualItem, string, error) {
	var m domain.ManualItem
	m.Text = strings.TrimSpace(r.PostForm.Get("text"))
	if raw := strings.TrimSpace(r.PostForm.Get("ingredient")); raw != "" {
		ing, err := idParam("ingredient id", raw)
		if err != nil {
			return m, msgReference, nil
		}
		m.Ingredient = recipe.IngredientID(ing)
	}
	if m.Text == "" && m.Ingredient == 0 {
		return m, msgManual, nil
	}
	rawQty := strings.TrimSpace(r.PostForm.Get("qty"))
	if rawQty == "" {
		return m, "", nil
	}
	qty, err := quantity.ParseQuantity(rawQty)
	if err != nil {
		return m, msgQuantity, nil
	}
	m.Amount.Qty = qty
	if name := strings.TrimSpace(r.PostForm.Get("unit")); name != "" {
		units, err := h.Catalog.ListUnits(r.Context())
		if err != nil {
			return m, "", fmt.Errorf("list units: %w", err)
		}
		found := false
		for _, u := range units {
			if u.Unit.Name == name {
				m.Amount.Unit, found = u.Unit, true
				break
			}
		}
		if !found {
			return m, msgUnit, nil
		}
	}
	return m, "", nil
}

func (h *handler) addManual(w http.ResponseWriter, r *http.Request) {
	id, err := listID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	if !parseForm(w, r) {
		return
	}
	m, problem, err := h.manualItem(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	if problem != "" {
		h.renderBuild(w, r, id, http.StatusBadRequest, problem)
		return
	}
	if _, err := h.Lists.AddManualItem(r.Context(), id, m); err != nil {
		h.failedEdit(w, r, id, fmt.Errorf("add manual item to list %d: %w", id, err))
		return
	}
	h.edited(w, r, id)
}

func (h *handler) removeManual(w http.ResponseWriter, r *http.Request) {
	id, err := listID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	item, err := idParam("manual item id", chi.URLParam(r, "item"))
	if err != nil {
		fail(w, r, err)
		return
	}
	view, err := h.Lists.Load(r.Context(), id)
	if err != nil {
		fail(w, r, fmt.Errorf("load list %d: %w", id, err))
		return
	}
	owned := false
	for _, m := range view.Manual {
		owned = owned || m.ID == item
	}
	if !owned {
		fail(w, r, fmt.Errorf("manual item %d is not on list %d: %w", item, id, layout.ErrNotFound))
		return
	}
	if err := h.Lists.RemoveManualItem(r.Context(), item); err != nil {
		fail(w, r, fmt.Errorf("remove manual item %d: %w", item, err))
		return
	}
	h.edited(w, r, id)
}

// addBack puts a dropped pantry item back on the list. The store keeps no
// add-back set, but a manual item that names a pantry ingredient keeps it on
// the list (ADR-00009), with the recipes' amounts, so adding one is the add-back.
// Removing that manual item sets the ingredient aside again.
func (h *handler) addBack(w http.ResponseWriter, r *http.Request) {
	id, err := listID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	if !parseForm(w, r) {
		return
	}
	ing, err := idParam("ingredient id", r.PostForm.Get("ingredient"))
	if err != nil {
		fail(w, r, err)
		return
	}
	if _, err := h.Lists.AddManualItem(r.Context(), id, domain.ManualItem{Ingredient: recipe.IngredientID(ing)}); err != nil {
		h.failedEdit(w, r, id, fmt.Errorf("add back ingredient %d to list %d: %w", ing, id, err))
		return
	}
	h.edited(w, r, id)
}

// setOptional saves whether the list keeps optional recipe lines. It does not
// regenerate: the checks of items the change hides stay stored, so switching the
// lines back on brings them back checked.
func (h *handler) setOptional(w http.ResponseWriter, r *http.Request) {
	id, err := listID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	if !parseForm(w, r) {
		return
	}
	include := r.PostForm.Get("optional") == "1"
	if err := h.Lists.SetIncludeOptional(r.Context(), id, include); err != nil {
		fail(w, r, fmt.Errorf("set include optional on list %d: %w", id, err))
		return
	}
	back(w, r, id, "")
}

func (h *handler) regenerate(w http.ResponseWriter, r *http.Request) {
	id, err := listID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	h.edited(w, r, id)
}

func (h *handler) export(w http.ResponseWriter, r *http.Request) {
	id, err := listID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	view, err := h.Lists.Load(r.Context(), id)
	if err != nil {
		fail(w, r, fmt.Errorf("load list %d: %w", id, err))
		return
	}
	text, err := view.Generated().Text()
	if err != nil {
		fail(w, r, fmt.Errorf("export list %d: %w", id, err))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := w.Write([]byte(text)); err != nil {
		h.log.WarnContext(r.Context(), "write export", "error", err.Error())
	}
}
