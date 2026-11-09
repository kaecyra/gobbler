package ingest

import (
	"errors"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// revisionOf is held's recipe as a revision that changes nothing.
func revisionOf(d Draft) Revision {
	r := Revision{
		Name: d.Name.Value, Author: d.Author.Value, Description: d.Description.Value,
		Yield: d.Yield.Value, Servings: d.Servings.Value, Calories: d.Calories.Value,
		Prep: d.Prep.Value, Cook: d.Cook.Value, Total: d.Total.Value, MealType: d.MealType, Cuisine: d.Cuisine,
	}
	for _, c := range d.Components {
		rc := RevisedComponent{Name: c.Name}
		for _, l := range c.Lines {
			rc.Lines = append(rc.Lines, RevisedLine{Text: l.Raw, Ingredient: l.Ingredient, Optional: l.Optional})
		}
		for _, s := range c.Steps {
			rc.Steps = append(rc.Steps, RevisedStep{Text: s.Text, Title: s.Title, Kind: s.Kind, Duration: s.Duration, Passive: s.Passive, DependsOn: s.DependsOn})
		}
		r.Components = append(r.Components, rc)
	}
	for _, n := range d.AuthorNotes {
		r.AuthorNotes = append(r.AuthorNotes, RevisedNote{Title: n.Title, Text: n.Text})
	}
	for _, e := range d.Equipment {
		r.Equipment = append(r.Equipment, RevisedEquipment{Name: e.Name, Equipment: e.Equipment})
	}
	return r
}

// A revision that changes nothing is the held draft; what it changes is
// marked as the LLM fallback's values are, and what it leaves is held's own,
// marks and all, even when it moved (ADR-00048).
func TestReviseMarksOnlyWhatChanged(t *testing.T) {
	e := newEnv(t)
	held := richDraft(0)
	held.Components[0].Lines[0].Ingredient = 0
	held.Equipment = held.Equipment[1:] // the catalog in this test has no dutch oven
	held.Proposed = ProposedFor(held.Components, nil)
	held.AuthorNotes = []AuthorNote{{Title: "Storage", Text: "Keeps two days.", Confidence: 0.6, Issues: []string{"long"}}}

	same, err := Revise(e.ctx, e.catalog, held, revisionOf(held))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := EncodeDraft(held)
	b, _ := EncodeDraft(same)
	if string(a) != string(b) {
		t.Fatalf("an unchanged revision changed the draft:\n%s\n%s", a, b)
	}

	r := revisionOf(held)
	r.Description = "Sharp."
	r.Cook = 0
	r.Author = ""
	r.Servings = quantity.Exact(quantity.Int(6))
	r.Calories = 280
	r.MealType = recipe.MealType("breakfast")
	r.Cuisine = recipe.Cuisine("french")
	r.AuthorNotes = append(r.AuthorNotes, RevisedNote{Text: "Serve warm."})
	c := &r.Components[0]
	c.Lines[0], c.Lines[1] = c.Lines[1], c.Lines[0] // moved, not changed
	c.Steps[0].Duration = 7 * time.Minute
	c.Steps[1].DependsOn = nil
	r.Equipment = append(r.Equipment, RevisedEquipment{Name: "Rolling pin"})
	got, err := Revise(e.ctx, e.catalog, held, r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description.Value != "Sharp." || !got.Description.FromLLM {
		t.Errorf("description = %+v, want the agent's, marked", got.Description)
	}
	if got.Cook.Value != 0 || !got.Cook.FromLLM {
		t.Errorf("cleared cook time = %+v, want no value, marked", got.Cook)
	}
	if got.Author.Value != "" || !got.Author.FromLLM {
		t.Errorf("cleared author = %+v, want no value, marked", got.Author)
	}
	if got.Servings.Value.String() != "6" || !got.Servings.FromLLM || got.Calories.Value != 280 || !got.Calories.FromLLM {
		t.Errorf("servings %+v and calories %+v, want the agent's, marked", got.Servings, got.Calories)
	}
	if got.MealType != "breakfast" || !got.MealTypeFromLLM || got.Cuisine != "french" || !got.CuisineFromLLM {
		t.Errorf("meal type %q (%v), cuisine %q (%v); want the agent's, marked", got.MealType, got.MealTypeFromLLM, got.Cuisine, got.CuisineFromLLM)
	}
	if ns := got.AuthorNotes; len(ns) != 2 || ns[0].FromLLM || ns[0].Confidence != 0.6 || !ns[1].FromLLM {
		t.Errorf("author notes = %+v, want the held note as it was and the new one marked", ns)
	}
	if got.Name != held.Name || got.Yield.FromLLM != held.Yield.FromLLM || got.Prep != held.Prep {
		t.Error("an unchanged field lost its marks")
	}
	ls := got.Components[0].Lines
	if ls[0].Raw != "salt to taste" || ls[0].FromLLM || ls[0].Confidence != 0.4 || !ls[1].FromLLM {
		t.Errorf("moved lines = %+v, want each its held self", ls)
	}
	ss := got.Components[0].Steps
	if !ss[0].FromLLM || ss[0].Duration != 7*time.Minute {
		t.Errorf("step with a new time = %+v, want it marked", ss[0])
	}
	if !ss[1].GraphFromLLM || ss[1].GraphCue || ss[1].GraphByUser || len(ss[1].DependsOn) != 0 {
		t.Errorf("step with a new graph = %+v, want only GraphFromLLM", ss[1])
	}
	if eq := got.Equipment; len(eq) != 2 || eq[1].Name != "rolling pin" || !eq[1].FromLLM {
		t.Errorf("equipment = %+v, want the new rolling pin marked", eq)
	}
	if got.Source.SHA256 != held.Source.SHA256 || got.ParserVersion != held.ParserVersion || got.Overall != held.Overall {
		t.Error("a revision changed the source, parser version or score")
	}
}

func TestReviseRefusesWhatCannotBeADraft(t *testing.T) {
	e := newEnv(t)
	held := richDraft(0)
	held.Equipment = nil
	for name, mutate := range map[string]func(*Revision){
		"no name":         func(r *Revision) { r.Name = " " },
		"no components":   func(r *Revision) { r.Components = nil },
		"bad meal type":   func(r *Revision) { r.MealType = recipe.MealType("brunchish") },
		"self dependency": func(r *Revision) { r.Components[0].Steps[1].DependsOn = []int{2} },
		"blank line":      func(r *Revision) { r.Components[0].Lines[0].Text = " " },
		"unknown kit":     func(r *Revision) { r.Equipment = []RevisedEquipment{{Name: "x", Equipment: 99}} },
	} {
		r := revisionOf(held)
		mutate(&r)
		if _, err := Revise(e.ctx, e.catalog, held, r); !errors.Is(err, ErrInvalidRevision) {
			t.Errorf("%s: %v, want ErrInvalidRevision", name, err)
		}
	}
}

// The revision marks on meal type, cuisine and author notes survive the draft
// document, and a document stored before them reads as unmarked.
func TestRevisionMarksSurviveTheDraftDocument(t *testing.T) {
	d := richDraft(0)
	d.Cuisine = recipe.Cuisine("french")
	d.MealTypeFromLLM, d.CuisineFromLLM = true, true
	d.AuthorNotes = []AuthorNote{{Text: "Serve warm.", Confidence: 1, FromLLM: true}}
	doc, err := EncodeDraft(d)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeDraft(doc, snapshotOf(d))
	if err != nil {
		t.Fatal(err)
	}
	if !back.MealTypeFromLLM || !back.CuisineFromLLM || len(back.AuthorNotes) != 1 || !back.AuthorNotes[0].FromLLM {
		t.Errorf("marks after a round trip: meal type %v, cuisine %v, notes %+v", back.MealTypeFromLLM, back.CuisineFromLLM, back.AuthorNotes)
	}

	old := []byte(`{"parser_version":"20","name":{"v":"Pie","c":1},"meal_type":"dessert","cuisine":"french","components":[],"author_notes":[{"text":"Serve warm.","confidence":1}],"overall":1,"threshold":0.7}`)
	was, err := DecodeDraft(old, snapshotOf(d))
	if err != nil {
		t.Fatal(err)
	}
	if was.MealType != "dessert" || was.MealTypeFromLLM || was.CuisineFromLLM || len(was.AuthorNotes) != 1 || was.AuthorNotes[0].FromLLM {
		t.Errorf("a document stored before the marks reads as %+v", was)
	}
}
