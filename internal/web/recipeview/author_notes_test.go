package recipeview

import (
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/recipe"
)

func withNotes(t *testing.T, notes ...recipe.AuthorNote) recipe.Recipe {
	t.Helper()
	r := testRecipe(t)
	r.AuthorNotes = notes
	return r
}

// The notes continue the step chain: they sit in the last list of steps,
// after the last step, each credited to the author, with the title in bold.
func TestAuthorNotesEndTheStepChain(t *testing.T) {
	r := withNotes(t,
		recipe.AuthorNote{Title: "Wine", Text: "Any dry red works."},
		recipe.AuthorNote{Text: "Keeps for <3 days> & freezes."},
	)
	body := page(t, fakeRecipes{7: r}, "/recipes/7")
	mustContain(t, body,
		`class="step anote"`, `data-testid="author-note"`,
		`<b data-testid="author-note-title">Wine:</b>`, "Any dry red works.",
		`<small>From the author</small>`,
		"Keeps for &lt;3 days&gt; &amp; freezes.")
	if n := strings.Count(body, `data-testid="author-note"`); n != 2 {
		t.Errorf("%d notes on the page, want 2", n)
	}
	if n := strings.Count(body, `<small>From the author</small>`); n != 2 {
		t.Errorf("%d credits, want one on each note", n)
	}
	// After the last step, inside the same list, so the rail runs on.
	lastStep := strings.LastIndex(body, `class="step"`)
	firstNote := strings.Index(body, `class="step anote"`)
	closing := strings.Index(body[firstNote:], "</ol>")
	if lastStep < 0 || firstNote < lastStep || closing < 0 || strings.Contains(body[lastStep:firstNote], "</ol>") {
		t.Errorf("notes are not in the last step list after the last step (last step at %d, first note at %d)", lastStep, firstNote)
	}
	mustNotContain(t, body, `>Author's Notes<`, `>Author&#39;s Notes<`)
}

func TestAuthorNoteTitleKeepsItsOwnPunctuation(t *testing.T) {
	for title, want := range map[string]string{"Wine": "Wine:", " Wine ": "Wine:", "Why?": "Why?", "Note.": "Note.", "Tip:": "Tip:", "": ""} {
		if got := leadOf(title); got != want {
			t.Errorf("leadOf(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestRecipeWithoutAuthorNotesShowsNone(t *testing.T) {
	body := page(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	mustNotContain(t, body, `data-testid="author-note"`, "From the author")
}

// A scaled view is a swapped fragment of the same page; the notes are in it.
func TestAuthorNotesSurviveScaling(t *testing.T) {
	r := withNotes(t, recipe.AuthorNote{Text: "Stir often."})
	body := page(t, fakeRecipes{7: r}, "/recipes/7?scale=2")
	mustContain(t, body, `data-testid="author-note"`, "Stir often.")
}

// A recipe of ingredients alone still shows its notes, as a list of their own.
func TestAuthorNotesShowOnARecipeWithNoSteps(t *testing.T) {
	r := withNotes(t, recipe.AuthorNote{Title: "Tip", Text: "Use ripe fruit."})
	for ci := range r.Components {
		r.Components[ci].Steps = nil
	}
	body := page(t, fakeRecipes{7: r}, "/recipes/7")
	mustContain(t, body, `data-testid="author-note"`, "Use ripe fruit.")
}
