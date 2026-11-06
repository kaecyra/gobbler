package ingest

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/recipe"
)

func TestAuthorNotesFlagWhatIsNotAShortTip(t *testing.T) {
	nutrition := "serving: 209g calories: 566kcal fat: 40g (62%) sodium: 516mg (22%) protein: 30g"
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"a tip", "Use panko, not standard breadcrumbs, for a crunchier crust.", nil},
		{"a tip that mentions calories once", "Skipping the oil saves about 120 calories: 1 tbsp is plenty.", nil},
		{"exactly 400 characters", strings.Repeat("a", 400), nil},
		{"401 characters", strings.Repeat("a", 401), []string{IssueNoteTooLong}},
		{"nutrition table", nutrition, []string{IssueNoteNutrition}},
		{"nutrition heading", "Nutrition per serving. It assumes the oil is absorbed.", []string{IssueNoteNutrition}},
		{"publication history", "Originally published in March 2017 as part of the Katsu Curry post.", []string{IssueNotePublication}},
		{"updated on", "Updated in June 2022 with a new video.", []string{IssueNotePublication}},
		{"meal ideas", "Meal Ideas: a typical Japanese meal has rice and soup.", []string{IssueNoteMealPlan}},
		{"over the limit and nutrition", strings.Repeat("calories: 5g ", 40), []string{IssueNoteTooLong, IssueNoteNutrition}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := authorNotes([]extract.Note{{Text: c.text}})
			if len(got) != 1 {
				t.Fatalf("got %d notes, want 1 (ingest drops nothing)", len(got))
			}
			if !reflect.DeepEqual(got[0].Issues, c.want) {
				t.Errorf("issues = %v, want %v", got[0].Issues, c.want)
			}
			if flagged := got[0].Confidence < 0.7; flagged != (c.want != nil) {
				t.Errorf("confidence = %v, flagged = %v, want flagged = %v", got[0].Confidence, flagged, c.want != nil)
			}
		})
	}
}

func TestAuthorNotesKeepOrderAndTitlesAndSkipBlanks(t *testing.T) {
	got := authorNotes([]extract.Note{{Title: " Honey ", Text: "Not for infants."}, {Text: "  "}, {Text: "Keeps three days."}})
	if len(got) != 2 || got[0].Title != "Honey" || got[0].Text != "Not for infants." || got[1].Text != "Keeps three days." {
		t.Errorf("notes = %+v", got)
	}
}

// The saved RecipeTin Eats page: thirteen paragraphs in its notes block, all
// kept, and only the nutrition paragraphs arrive at low confidence.
func TestIngestChickenCutletNotesFlagNutrition(t *testing.T) {
	page, err := os.ReadFile("extract/testdata/real_recipetineats_graph.html")
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	srv, _ := serve(t, page)
	d, err := e.pipeline(t, srv.Client()).FromURL(e.ctx, srv.URL+"/chicken-cutlet")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.AuthorNotes) != 13 {
		t.Fatalf("got %d author notes, want 13: %+v", len(d.AuthorNotes), d.AuthorNotes)
	}
	var flagged []string
	for _, n := range d.AuthorNotes {
		if n.Confidence < d.Threshold {
			flagged = append(flagged, strings.Fields(n.Text)[0])
		}
	}
	want := []string{"7.", "serving:"}
	if !reflect.DeepEqual(flagged, want) {
		t.Errorf("low-confidence notes = %q, want %q", flagged, want)
	}
	if d.AuthorNotes[0].Confidence != 1 || !strings.HasPrefix(d.AuthorNotes[0].Text, "1. I used chicken thigh") {
		t.Errorf("first note = %+v, want a plain tip kept at full confidence", d.AuthorNotes[0])
	}
}

func TestAuthorNotesSurviveTheDraftDocumentAndTheSave(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipeline(t, nil).FromText(e.ctx, string(readFixture(t, "garlic_spaghetti.txt")))
	if err != nil {
		t.Fatal(err)
	}
	d.AuthorNotes = []AuthorNote{
		{Title: "Honey", Text: "Not for infants.", Confidence: 1},
		{Text: "Nutrition per serving.", Confidence: 0.4, Issues: []string{IssueNoteNutrition}},
	}
	doc, err := EncodeDraft(d)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeDraft(doc, snapshotOf(d))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.AuthorNotes, d.AuthorNotes) {
		t.Errorf("decoded notes = %+v, want %+v", back.AuthorNotes, d.AuthorNotes)
	}
	saved, err := e.saver().Save(e.ctx, confirm(t, back))
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.store.Get(e.ctx, saved.RecipeID)
	if err != nil {
		t.Fatal(err)
	}
	want := []recipe.AuthorNote{{Title: "Honey", Text: "Not for infants."}, {Text: "Nutrition per serving."}}
	if !reflect.DeepEqual(got.AuthorNotes, want) {
		t.Errorf("saved notes = %+v, want %+v", got.AuthorNotes, want)
	}
}

func TestConfirmRefusesAnAuthorNoteTheRecipeWouldRefuse(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipeline(t, nil).FromText(e.ctx, string(readFixture(t, "garlic_spaghetti.txt")))
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"two paragraph": "One.\n\nTwo.",
		"empty":         " ",
	} {
		t.Run(name, func(t *testing.T) {
			bad := d
			bad.AuthorNotes = []AuthorNote{{Title: "Tip", Text: text}}
			_, err := Confirm(bad)
			if !errors.Is(err, ErrIncompleteDraft) || !errors.Is(err, recipe.ErrInvalidAuthorNote) || !strings.Contains(err.Error(), "author note 1 (Tip)") {
				t.Errorf("Confirm = %v, want ErrIncompleteDraft naming author note 1", err)
			}
		})
	}
}
