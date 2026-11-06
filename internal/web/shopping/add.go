package shopping

import (
	"errors"
	"fmt"
	"net/http"

	apishop "github.com/kaecyra/gobbler/internal/api/shopping"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// quickAdd is the one-tap "Add to shopping list" of the recipe page: it puts
// the recipe, at the multiplier the page is showing, on the household's active
// list and shows that list. The active list is the most recently created one;
// the store has no other notion of a current list. A recipe already on it has
// its multiplier set to the one just chosen, because the tap is the latest
// statement of how much to cook. With no list at all the household is sent to
// the lists page to make one. The service holds the rule (AddToActiveList).
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
	id, err := h.Shop.AddToActiveList(r.Context(), callerOf(r.Context()), recipeID, mult)
	if errors.Is(err, apishop.ErrNoList) {
		http.Redirect(w, r, Pattern, http.StatusSeeOther)
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	h.edited(w, r, id)
}
