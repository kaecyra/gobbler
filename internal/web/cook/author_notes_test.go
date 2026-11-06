package cook

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// Cook mode shows one step at a time, so the author's notes are behind an
// "Author's Notes" button that is on every step (ADR-00041).
func TestAuthorNotesButtonOpensTheNotesOnEveryStep(t *testing.T) {
	r := pizza(t)
	r.AuthorNotes = []recipe.AuthorNote{
		{Title: "Flour", Text: "Bread flour gives a chewier crust."},
		{Text: "Dough keeps <2 days> & freezes."},
	}
	for _, path := range []string{"/recipes/3/cook", "/recipes/3/cook?step=4", "/recipes/3/cook?step=6"} {
		code, body := serve(t, fakeRecipes{3: r}, path)
		if code != http.StatusOK {
			t.Fatalf("%s: status = %d", path, code)
		}
		mustContain(t, body,
			`data-testid="notes-open"`, `popovertarget="cook-notes"`, "Author's Notes",
			`id="cook-notes"`, `popover`, `<b>Flour:</b>`, "Bread flour gives a chewier crust.",
			"Dough keeps &lt;2 days&gt; &amp; freezes.")
		if n := strings.Count(body, `<small>From the author</small>`); n != 2 {
			t.Errorf("%s: %d credits, want one on each note", path, n)
		}
	}
}

func TestNoAuthorNotesButtonWithoutNotes(t *testing.T) {
	_, body := serve(t, fakeRecipes{3: pizza(t)}, "/recipes/3/cook")
	mustNotContain(t, body, `data-testid="notes-open"`, "Author's Notes", `id="cook-notes"`)
}
