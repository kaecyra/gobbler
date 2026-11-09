package editor

import (
	"bytes"
	"strings"
	"testing"

	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// revisedBase is a draft an agent revised (ADR-00048): it changed servings,
// calories, meal type, cuisine and an author note, and cleared the author.
func revisedBase() ingestcore.Draft {
	d := fieldsBase()
	d.Servings = ingestcore.Field[quantity.Quantity]{Value: quantity.Exact(quantity.Int(6)), Confidence: 1, FromLLM: true}
	d.Calories = ingestcore.Field[int]{Value: 280, Confidence: 1, FromLLM: true}
	d.Author = ingestcore.Field[string]{Confidence: 1, FromLLM: true}
	d.Cuisine = recipe.Cuisine("french")
	d.MealTypeFromLLM, d.CuisineFromLLM = true, true
	d.AuthorNotes = []ingestcore.AuthorNote{{Text: "Serve warm.", Confidence: 1, FromLLM: true}}
	return d
}

// Every value an agent's revision changed is marked LLM on the review screen,
// and a field it cleared is marked as cleared.
func TestReviewMarksEveryRevisedField(t *testing.T) {
	e := newEnv(t)
	form, err := NewUnsaved(e.cat).Open(e.ctx, revisedBase(), ReviewOptions{Action: "/x"})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := form.Component().Render(e.ctx, &buf); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, field := range []string{"servings", "calories", "meal_type", "cuisine", "author"} {
		if !strings.Contains(body, `name="mark.`+field+`" value="llm"`) {
			t.Errorf("the review form does not mark %s", field)
		}
	}
	if !strings.Contains(body, `aria-label="Cleared by the LLM"`) {
		t.Error("the cleared author is not marked as cleared")
	}
	if !strings.Contains(body, `name="n.0.mark" value="llm"`) {
		t.Error("the revised author note is not marked")
	}
}

// The marks ride the form back into the draft, so a draft kept after a failed
// save reopens with them.
func TestRevisionMarksRoundTripThroughTheReviewForm(t *testing.T) {
	e := newEnv(t)
	u := NewUnsaved(e.cat)
	v := postedGraphForm(t, e, u, revisedBase())
	v.Set("servings", "6")
	v.Set("calories", "280")
	v.Set("meal_type", "dessert")
	v.Set("cuisine", "french")
	for _, field := range []string{"servings", "calories", "meal_type", "cuisine", "author"} {
		v.Set(markName(field), "llm")
	}
	v.Set("n.0.text", "Serve warm.")
	v.Set("n.0.mark", "llm")
	out, err := u.Submit(e.ctx, v, revisedBase(), ReviewOptions{Action: "/x"})
	if err != nil || !out.Ready {
		t.Fatalf("Submit: ready=%v err=%v", out.Ready, err)
	}
	d := out.Draft
	if !d.Servings.FromLLM || !d.Calories.FromLLM || !d.MealTypeFromLLM || !d.CuisineFromLLM || !d.Author.FromLLM {
		t.Errorf("marks after the form: servings %v calories %v meal type %v cuisine %v author %v",
			d.Servings.FromLLM, d.Calories.FromLLM, d.MealTypeFromLLM, d.CuisineFromLLM, d.Author.FromLLM)
	}
	if len(d.AuthorNotes) != 1 || !d.AuthorNotes[0].FromLLM {
		t.Errorf("notes after the form = %+v, want the note marked", d.AuthorNotes)
	}
}
