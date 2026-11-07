package recipeview

import (
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// Step text and author-note text render through layout.Markdown (ADR-00046):
// the subset becomes markup, and anything else stays the text it was written
// as.
func TestStepAndNoteTextRenderAsMarkdown(t *testing.T) {
	r := withNotes(t, recipe.AuthorNote{Title: "Swaps", Text: "Use **either**:\n- butter\n- ghee <script>alert(1)</script>"})
	r.Components[0].Steps[0].Text = "Mix **well**.\n# Not a heading\n- flour\n- sugar"
	body := page(t, fakeRecipes{7: r}, "/recipes/7")
	mustContain(t, body,
		"<p>Mix <strong>well</strong>.<br>\n# Not a heading</p>\n<ul>\n<li>flour</li>\n<li>sugar</li>\n</ul>",
		`<b data-testid="author-note-title">Swaps:</b>`,
		"<p>Use <strong>either</strong>:</p>\n<ul>\n<li>butter</li>\n<li>ghee &lt;script&gt;alert(1)&lt;/script&gt;</li>\n</ul>",
		`<small>From the author</small>`,
	)
	mustNotContain(t, body, "<h1>Not a heading", "<script>alert(1)")
}

// A step's text offered while another step waits renders the same way.
func TestMeanwhileItemRendersAsMarkdown(t *testing.T) {
	r := doughAndSauce()
	for ci := range r.Components {
		for si := range r.Components[ci].Steps {
			if r.Components[ci].Steps[si].Text == "Chop <tomatoes>." {
				r.Components[ci].Steps[si].Text = "Chop **ripe** <tomatoes>."
			}
		}
	}
	body := page(t, fakeRecipes{3: r}, "/recipes/3")
	// Once as the step itself, once in the callout of the step it waits on.
	if n := strings.Count(body, `<div class="md"><p>Chop <strong>ripe</strong> &lt;tomatoes&gt;.</p>`); n != 2 {
		t.Errorf("rendered step text %d times, want 2", n)
	}
}
