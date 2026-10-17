package shopping

import (
	"fmt"
	"net/http"

	"github.com/kaecyra/gobbler/internal/web/layout"
)

// quickAdd is the one-tap "Add to shopping list" of the recipe page: it puts
// the recipe, at the multiplier the page is showing, on the household's active
// list and shows that list. The active list is the most recently created one;
// the store has no other notion of a current list. A recipe already on it has
// its multiplier set to the one just chosen, because the tap is the latest
// statement of how much to cook. With no list at all the household is sent to
// the lists page to make one.
func (h *handler) quickAdd(w http.ResponseWriter, r *http.Request) {
	if !parseForm(w, r) {
		return
	}
	recipeID, err := idParam("recipe id", r.PostForm.Get("recipe"))
	if err != nil {
		fail(w, r, err)
		return
	}
	mult, ok := parseMultiplier(r.PostForm.Get("multiplier"))
	if !ok {
		fail(w, r, fmt.Errorf("%s: %w", msgMultiplier, layout.ErrInvalid))
		return
	}
	// The recipe is checked first so one of another household is 404 whether or
	// not this household has a list.
	if _, err := h.Recipes.Info(r.Context(), recipeID); err != nil {
		fail(w, r, fmt.Errorf("recipe %d: %w", recipeID, err))
		return
	}
	lists, err := h.Lists.Lists(r.Context())
	if err != nil {
		fail(w, r, fmt.Errorf("list shopping lists: %w", err))
		return
	}
	if len(lists) == 0 {
		http.Redirect(w, r, Pattern, http.StatusSeeOther)
		return
	}
	id := lists[len(lists)-1].ID
	view, err := h.Lists.Load(r.Context(), id)
	if err != nil {
		fail(w, r, fmt.Errorf("load list %d: %w", id, err))
		return
	}
	for _, e := range view.Entries {
		if e.RecipeID != recipeID {
			continue
		}
		if err := h.Lists.SetEntryMultiplier(r.Context(), e.ID, mult); err != nil {
			fail(w, r, fmt.Errorf("set multiplier of entry %d: %w", e.ID, err))
			return
		}
		h.edited(w, r, id)
		return
	}
	if _, err := h.Lists.AddEntry(r.Context(), id, recipeID, mult); err != nil {
		fail(w, r, fmt.Errorf("add recipe %d to list %d: %w", recipeID, id, err))
		return
	}
	h.edited(w, r, id)
}
