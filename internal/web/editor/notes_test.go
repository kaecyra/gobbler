package editor

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// The editor shows a recipe's author notes as editable rows, and what the
// person types, adds, removes and reorders reaches the stored document
// (ADR-00041).
func TestAuthorNotesAreEditedInTheEditor(t *testing.T) {
	e := newEnv(t)
	f := e.form()
	f.Set(nField(0, "title"), "Honey")
	f.Set(nField(0, "text"), "  Never give it to infants.  ")
	f.Set(nField(1, "text"), "Keeps three days.")
	f.Set(nField(2, "title"), "")
	f.Set(nField(2, "text"), " ") // an unused row is dropped
	e.post(f).wantStatus(t, http.StatusSeeOther)

	want := []recipe.AuthorNote{{Title: "Honey", Text: "Never give it to infants."}, {Text: "Keeps three days."}}
	got, _ := e.stored()
	if len(got.AuthorNotes) != 2 || got.AuthorNotes[0] != want[0] || got.AuthorNotes[1] != want[1] {
		t.Fatalf("stored notes = %+v, want %+v", got.AuthorNotes, want)
	}

	// The form shows them back as rows with the section's name.
	page := e.get()
	page.wantStatus(t, http.StatusOK)
	page.wantBody(t, "Author&#39;s Notes", `name="n.0.title"`, `value="Honey"`, `name="n.1.text"`, `data-drag-list="move-note"`, `value="add-note"`, `value="rm-note:0"`)

	// Reorder and remove through the structural actions, then save.
	f = e.form()
	f.Set(fieldAction, "move-note:0.1")
	res := e.post(f)
	res.wantStatus(t, http.StatusOK)
	if i, j := strings.Index(res.body(), "Keeps three days."), strings.Index(res.body(), "Never give it to infants."); i < 0 || j < 0 || i > j {
		t.Error("after move-note the moved note should come first in the form")
	}
	f = e.form()
	f.Set(fieldAction, "rm-note:0")
	res = e.post(f)
	res.wantStatus(t, http.StatusOK)
	if strings.Contains(res.body(), "Never give it to infants.") {
		t.Error("rm-note left the removed note on the form")
	}
	if stored, _ := e.stored(); len(stored.AuthorNotes) != 2 {
		t.Error("an action saved; only Save does")
	}
	f = e.form()
	f.Set(nField(0, "title"), "")
	f.Del(nField(1, "title"))
	f.Del(nField(1, "text"))
	e.post(f).wantStatus(t, http.StatusSeeOther)
	if stored, _ := e.stored(); len(stored.AuthorNotes) != 1 || stored.AuthorNotes[0].Text != "Never give it to infants." {
		t.Errorf("after removing a row, notes = %+v", stored.AuthorNotes)
	}
}

func TestAddNoteAppendsABlankFocusedRow(t *testing.T) {
	e := newEnv(t)
	f := e.form()
	f.Set(fieldAction, "add-note")
	res := e.post(f)
	res.wantStatus(t, http.StatusOK)
	res.wantBody(t, `name="n.0.text"`, `autofocus`)
	if stored, _ := e.stored(); len(stored.AuthorNotes) != 0 {
		t.Error("add-note saved")
	}
	// A blank row left in the form is not saved.
	f = e.form()
	f.Set(nField(0, "title"), "")
	f.Set(nField(0, "text"), "")
	e.post(f).wantStatus(t, http.StatusSeeOther)
	if stored, _ := e.stored(); len(stored.AuthorNotes) != 0 {
		t.Errorf("a blank row was saved: %+v", stored.AuthorNotes)
	}
}

func TestAuthorNoteBadActionIs400(t *testing.T) {
	e := newEnv(t)
	for _, a := range []string{"rm-note:3", "move-note:0.1", "move-note:0"} {
		f := e.form()
		f.Set(fieldAction, a)
		e.post(f).wantStatus(t, http.StatusBadRequest)
	}
}

// The recipe's rule holds in the form: a note of two paragraphs, or a title
// with no text, is refused with a message on its row and nothing saved. Length
// is never refused (ADR-00044).
func TestAuthorNoteLimitsAreEnforcedOnTheForm(t *testing.T) {
	cases := []struct {
		name, title, text, msg string
		ok                     bool
	}{
		{"400", "", strings.Repeat("a", 400), "", true},
		{"1000", "", strings.Repeat("a", 1000), "", true},
		{"two paragraphs", "", "One.\n\nTwo.", "A note is one paragraph.", false},
		{"title only", "Honey", "", "A note needs text.", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			f := e.form()
			f.Set(nField(0, "title"), c.title)
			f.Set(nField(0, "text"), c.text)
			res := e.post(f)
			if c.ok {
				res.wantStatus(t, http.StatusSeeOther)
				return
			}
			res.wantStatus(t, http.StatusBadRequest)
			res.wantBody(t, c.msg)
			if stored, _ := e.stored(); len(stored.AuthorNotes) != 0 || len(e.revisions()) != 0 {
				t.Errorf("a refused note was saved: %+v", stored.AuthorNotes)
			}
		})
	}
}

func notesBase() ingestcore.Draft {
	d := graphBase()
	d.AuthorNotes = []ingestcore.AuthorNote{
		{Title: "Honey", Text: "Not for infants.", Confidence: 1},
		{Text: "Nutrition per serving: 566 kcal.", Confidence: 0.4, Issues: []string{ingestcore.IssueNoteNutrition}},
	}
	return d
}

// The review screen shows the candidates as rows; the one the rules flagged
// reads as a low-confidence line does, with words and not colour alone.
func TestReviewShowsAuthorNotesWithTheFlaggedOneMarked(t *testing.T) {
	e := newEnv(t)
	form, err := NewUnsaved(e.cat).Open(e.ctx, notesBase(), ReviewOptions{Action: "/x"})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := form.Component().Render(e.ctx, &buf); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{"Author&#39;s Notes", `value="Honey"`, "Not for infants.", `name="n.1.mark" value="low"`, `name="n.1.issue" value="note_nutrition"`, "it reads as nutrition facts"} {
		if !strings.Contains(body, want) {
			t.Errorf("review form lacks %q", want)
		}
	}
	if strings.Contains(body, `name="n.0.mark"`) {
		t.Error("the plain tip was marked")
	}
	if n := form.NeedsLook(); n != 1 {
		t.Errorf("NeedsLook = %d, want 1 for the flagged note", n)
	}
}

// What the person does to the notes on the review screen goes back into the
// draft, with the flag following its note.
func TestReviewAuthorNotesRoundTripThroughSubmit(t *testing.T) {
	e := newEnv(t)
	u := NewUnsaved(e.cat)
	v := postedGraphForm(t, e, u, notesBase())
	v.Set("n.0.title", "Honey")
	v.Set("n.0.text", "Not for infants. Trimmed.")
	v.Set("n.1.text", "Nutrition per serving: 566 kcal.")
	v.Set("n.1.mark", "low")
	v.Add("n.1.issue", ingestcore.IssueNoteNutrition)
	out, err := u.Submit(e.ctx, v, notesBase(), ReviewOptions{Action: "/x"})
	if err != nil || !out.Ready {
		t.Fatalf("Submit: ready=%v err=%v", out.Ready, err)
	}
	n := out.Draft.AuthorNotes
	if len(n) != 2 || n[0].Text != "Not for infants. Trimmed." || n[0].Confidence != 1 {
		t.Fatalf("notes = %+v", n)
	}
	if n[1].Confidence >= out.Draft.Threshold || len(n[1].Issues) != 1 {
		t.Errorf("the flagged note lost its flag: %+v", n[1])
	}

	// Removing the flagged note leaves the other; reordering swaps them.
	v.Set(fieldAction, "move-note:0.1")
	out, err = u.Submit(e.ctx, v, notesBase(), ReviewOptions{Action: "/x"})
	if err != nil || out.Ready {
		t.Fatalf("a structural action: ready=%v err=%v", out.Ready, err)
	}
	var buf bytes.Buffer
	if err := out.Form.Component().Render(e.ctx, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `name="n.0.mark" value="low"`) {
		t.Error("after the move the flag should sit on the first row, with its note")
	}
	v.Del(fieldAction)
	v.Set("n.0.text", strings.Repeat("a", 1000))
	out, err = u.Submit(e.ctx, v, notesBase(), ReviewOptions{Action: "/x"})
	if err != nil || !out.Ready || out.Rejected {
		t.Fatalf("a 1000-character note: ready=%v rejected=%v err=%v, want it accepted", out.Ready, out.Rejected, err)
	}
}
