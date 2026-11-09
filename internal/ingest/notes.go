package ingest

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// Issues a draft's author note can carry (ADR-00041). Like the line issues
// they are data about a less certain candidate, shown on the review screen,
// where the person trims or deletes it. Ingest drops nothing on its own.
const (
	// IssueNoteTooLong: the note is over recipe.AuthorNoteGuidelineChars characters
	// or holds more than one paragraph. Only the paragraph rule makes the recipe
	// refuse it; a long note is flagged and saves (ADR-00044).
	IssueNoteTooLong = "note_too_long"
	// IssueNoteNutrition: the note reads as nutrition facts.
	IssueNoteNutrition = "note_nutrition"
	// IssueNotePublication: the note reads as the page's publication history.
	IssueNotePublication = "note_publication_history"
	// IssueNoteMealPlan: the note reads as a meal plan or serving suggestions.
	IssueNoteMealPlan = "note_meal_plan"
)

// flaggedNoteConfidence is the confidence in a candidate note that is over the
// limit or reads as junk. It is below the default threshold, so the review
// screen flags the note as it does a low-confidence ingredient line.
const flaggedNoteConfidence = 0.4

// AuthorNote is one candidate author note of a draft: a title that may be
// empty and a text, as the recipe holds them, with the confidence the rules
// have that it is a short cooking tip.
type AuthorNote struct {
	Title string
	Text  string
	// Confidence is 0 to 1; Issues says why it is below 1.
	Confidence float64
	Issues     []string
	// FromLLM marks a note an agent's revision supplied (ADR-00048).
	FromLLM bool
}

var (
	// Nutrition: a stated heading, or a run of nutrient names with amounts.
	nutritionHeadingRE = regexp.MustCompile(`(?i)\bnutrition(al)?\s+(facts|information|info|values?|data|estimates?|per\s+serving)\b`)
	nutrientRE         = regexp.MustCompile(`(?i)\b(calories|fat|protein|carbohydrates?|carbs|sodium|cholesterol|sugars?|fib(?:er|re)|potassium|calcium|iron)\b\s*:?\s*\d[\d.,]*\s*(kcal|g|mg|mcg|%)`)
	// Publication history: when the post first went up or was revised.
	publicationRE = regexp.MustCompile(`(?i)\b(originally|first)\s+(published|posted)\b|\b(this|the)\s+(post|article|recipe)\s+(was\s+)?(originally|first|updated|republished|re-?posted)\b|\b(updated|republished|re-?posted)\s+(in|on)\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec|\d{4})`)
	// Meal plan: ideas for what to serve alongside.
	mealPlanRE = regexp.MustCompile(`(?i)\b(meal\s+ideas|meal\s+plan|menu\s+ideas|what\s+to\s+serve|side\s+dish\s+\d)\b`)
)

// minNutrientMatches is how many nutrient amounts make a note a nutrition
// table, so a tip that mentions the calories of one ingredient is left alone.
const minNutrientMatches = 3

// authorNotes reads the notes block a recipe plugin printed as candidate
// notes, in order, flagging each that does not look like a short tip. A note
// whose text is empty is skipped; no other candidate is dropped.
func authorNotes(raw []extract.Note) []AuthorNote {
	var out []AuthorNote
	for _, n := range raw {
		text := strings.TrimSpace(n.Text)
		if text == "" {
			continue
		}
		note := AuthorNote{Title: strings.TrimSpace(n.Title), Text: text, Confidence: 1}
		if utf8.RuneCountInString(text) > recipe.AuthorNoteGuidelineChars || recipe.MoreThanOneParagraph(text) {
			note.Issues = append(note.Issues, IssueNoteTooLong)
		}
		if nutritionHeadingRE.MatchString(text) || len(nutrientRE.FindAllString(text, -1)) >= minNutrientMatches {
			note.Issues = append(note.Issues, IssueNoteNutrition)
		}
		if publicationRE.MatchString(text) {
			note.Issues = append(note.Issues, IssueNotePublication)
		}
		if mealPlanRE.MatchString(text) {
			note.Issues = append(note.Issues, IssueNoteMealPlan)
		}
		if len(note.Issues) > 0 {
			note.Confidence = flaggedNoteConfidence
		}
		out = append(out, note)
	}
	return out
}
