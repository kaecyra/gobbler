package shopping

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// maxQueryRunes bounds the ingredient search text.
const maxQueryRunes = 100

const pantryPath = Pattern + "/pantry"

// pantryView is the pantry page: what is in the pantry, and the catalog
// ingredients matching the search that could be added.
type pantryView struct {
	Query   string
	Pantry  []option
	Matches []option
}

// matches reports whether the ingredient's name or an alias contains q,
// ignoring case. An empty q matches everything.
func matches(in catalog.Ingredient, q string) bool {
	if q == "" || strings.Contains(strings.ToLower(in.Name), q) {
		return true
	}
	for _, a := range in.Aliases {
		if strings.Contains(strings.ToLower(a), q) {
			return true
		}
	}
	return false
}

func (h *handler) pantryShow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(q) > maxQueryRunes {
		fail(w, r, fmt.Errorf("search text of %d characters: %w", utf8.RuneCountInString(q), layout.ErrInvalid))
		return
	}
	ids, err := h.Lists.Pantry(ctx)
	if err != nil {
		fail(w, r, fmt.Errorf("read pantry: %w", err))
		return
	}
	all, err := h.Catalog.ListIngredients(ctx)
	if err != nil {
		fail(w, r, fmt.Errorf("list ingredients: %w", err))
		return
	}
	in := make(map[recipe.IngredientID]bool, len(ids))
	for _, id := range ids {
		in[id] = true
	}
	v := pantryView{Query: q}
	needle := strings.ToLower(q)
	for _, ing := range all {
		o := option{Value: strconv.FormatInt(ing.ID, 10), Label: ing.Name}
		switch {
		case in[recipe.IngredientID(ing.ID)]:
			v.Pantry = append(v.Pantry, o)
		case q != "" && matches(ing, needle):
			v.Matches = append(v.Matches, o)
		}
	}
	layout.Render(w, r, http.StatusOK, pantryPage(v))
}

// backToPantry redirects to the pantry page with the search the user had.
func backToPantry(w http.ResponseWriter, r *http.Request) {
	target := pantryPath
	if q := strings.TrimSpace(r.PostForm.Get("q")); q != "" {
		target += "?" + url.Values{"q": {q}}.Encode()
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *handler) pantryAdd(w http.ResponseWriter, r *http.Request) {
	if !parseForm(w, r) {
		return
	}
	id, err := idParam("ingredient id", r.PostForm.Get("ingredient"))
	if err != nil {
		fail(w, r, err)
		return
	}
	if err := h.Lists.AddToPantry(r.Context(), recipe.IngredientID(id)); err != nil {
		fail(w, r, fmt.Errorf("add ingredient %d to the pantry: %w", id, err))
		return
	}
	backToPantry(w, r)
}

func (h *handler) pantryRemove(w http.ResponseWriter, r *http.Request) {
	if !parseForm(w, r) {
		return
	}
	id, err := idParam("ingredient id", r.PostForm.Get("ingredient"))
	if err != nil {
		fail(w, r, err)
		return
	}
	if err := h.Lists.RemoveFromPantry(r.Context(), recipe.IngredientID(id)); err != nil {
		fail(w, r, fmt.Errorf("remove ingredient %d from the pantry: %w", id, err))
		return
	}
	backToPantry(w, r)
}
