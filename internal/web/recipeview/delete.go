package recipeview

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// DeletePattern is where the router mounts DeleteRoutes, next to Pattern.
const DeletePattern = Pattern + "/delete"

// deleteWord is what a member types to confirm a recipe's deletion.
const deleteWord = "DELETE"

// Deleter is what the delete page needs from the recipe service
// (*apirecipes.Service satisfies it): the service scopes both calls to the
// Caller's household (ADR-00018) and deletes the recipe's picture objects with
// it (ADR-00039).
type Deleter interface {
	Get(ctx context.Context, c apirecipes.Caller, id int64) (recipe.Recipe, error)
	Delete(ctx context.Context, c apirecipes.Caller, id int64) error
}

// DeleteDeps is what the delete routes need.
type DeleteDeps struct {
	Recipes Deleter
}

// DeleteRoutes returns the confirmation page and the delete itself, relative
// to DeletePattern.
func DeleteRoutes(d DeleteDeps) chi.Router {
	r := chi.NewRouter()
	r.Get("/", showDelete(d))
	r.Post("/", doDelete(d))
	return r
}

func showDelete(d DeleteDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := recipeID(r)
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		rec, err := d.Recipes.Get(r.Context(), caller.FromContext(r.Context()), id)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
			return
		}
		layout.Render(w, r, http.StatusOK, deletePage(deleteView{ID: id, Name: rec.Name}))
	}
}

// doDelete deletes the recipe once the typed word matches. The recipe is read
// first so a recipe of another household answers as every other recipe action
// does, before the confirmation is looked at.
func doDelete(d DeleteDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := recipeID(r)
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		if _, err := d.Recipes.Get(r.Context(), caller.FromContext(r.Context()), id); err != nil {
			layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
			return
		}
		if strings.TrimSpace(r.PostFormValue("confirm")) != deleteWord {
			layout.WriteError(w, r, fmt.Errorf("delete recipe %d: type %s to confirm: %w", id, deleteWord, layout.ErrInvalid))
			return
		}
		// The store's error already says which recipe it was deleting.
		if err := d.Recipes.Delete(r.Context(), caller.FromContext(r.Context()), id); err != nil {
			layout.WriteError(w, r, err)
			return
		}
		http.Redirect(w, r, libraryPath, http.StatusSeeOther)
	}
}
