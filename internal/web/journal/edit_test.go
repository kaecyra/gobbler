package journal

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/kaecyra/gobbler/internal/recipe"
)

func (e *env) notePath(noteID int64, suffix string) string {
	return e.path("/notes/" + strconv.FormatInt(noteID, 10) + suffix)
}

// addNote adds a note through the store and returns its id.
func (e *env) addNote(body string, pinned bool) int64 {
	e.t.Helper()
	n, err := e.store.AddNote(e.ctx, e.id, body, pinned)
	if err != nil {
		e.t.Fatal(err)
	}
	return n.ID
}

// foreignNote is a note on another recipe of the same household: its id must
// not reach it through this recipe's routes.
func (e *env) foreignNote() (recipeID, noteID int64) {
	e.t.Helper()
	other, err := e.store.Create(e.ctx, recipe.Recipe{
		Name:       "Other",
		Components: []recipe.Component{{Steps: []recipe.Step{{ID: "mix", Text: "Mix."}}}},
	})
	if err != nil {
		e.t.Fatal(err)
	}
	n, err := e.store.AddNote(e.ctx, other, "the other recipe's note", false)
	if err != nil {
		e.t.Fatal(err)
	}
	return other, n.ID
}

func TestEachNoteOffersEditAndDelete(t *testing.T) {
	e := newEnv(t)
	id := e.addNote("salt early", false)
	body := e.do(http.MethodGet, e.path(""), nil, htmx...).Body.String()
	mustContain(t, body, `<details class="note-menu">`, `aria-label="More for this note"`,
		`href="`+e.notePath(id, "/edit")+`"`, `href="`+e.notePath(id, "/delete")+`"`)
}

func TestANoteIsEditedInItsPlace(t *testing.T) {
	e := newEnv(t)
	id := e.addNote("salt early", true)

	rec := e.do(http.MethodGet, e.notePath(id, "/edit"), nil, htmx...)
	if rec.Code != http.StatusOK {
		t.Fatalf("edit form = %d\n%s", rec.Code, rec.Body)
	}
	mustContain(t, rec.Body.String(), `action="`+e.notePath(id, "")+`"`, ">salt early</textarea>", "Save note", "Cancel")
	mustNotContain(t, rec.Body.String(), `<p class="note-body">salt early</p>`)

	rec = e.do(http.MethodPost, e.notePath(id, ""), url.Values{"body": {"salt the night before"}}, htmx...)
	if rec.Code != http.StatusOK {
		t.Fatalf("save = %d\n%s", rec.Code, rec.Body)
	}
	n := e.notes()
	if len(n) != 1 || n[0].Body != "salt the night before" || n[0].ID != id || !n[0].Pinned {
		t.Errorf("notes = %+v, want the one note, edited and still pinned", n)
	}
	mustContain(t, rec.Body.String(), `<p class="note-body">salt the night before</p>`)
	mustNotContain(t, rec.Body.String(), "Save note")
}

func TestAnEmptyEditIsRejectedInPlace(t *testing.T) {
	e := newEnv(t)
	id := e.addNote("salt early", false)
	rec := e.do(http.MethodPost, e.notePath(id, ""), url.Values{"body": {"  "}}, htmx...)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 so htmx swaps the message", rec.Code)
	}
	mustContain(t, rec.Body.String(), `role="alert"`, "Write something", "Save note")
	if n := e.notes(); n[0].Body != "salt early" {
		t.Errorf("body = %q, want it unchanged", n[0].Body)
	}
	// Without scripts the page comes back with 400.
	if rec := e.do(http.MethodPost, e.notePath(id, ""), url.Values{"body": {""}}); rec.Code != http.StatusBadRequest {
		t.Errorf("plain empty edit = %d, want 400", rec.Code)
	}
}

func TestANoteIsDeletedOnlyAfterTheConfirmation(t *testing.T) {
	e := newEnv(t)
	id := e.addNote("salt early", false)

	rec := e.do(http.MethodGet, e.notePath(id, "/delete"), nil, htmx...)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirmation = %d\n%s", rec.Code, rec.Body)
	}
	mustContain(t, rec.Body.String(), "Delete this note?", `action="`+e.notePath(id, "/delete")+`"`, ">Keep</a>")
	if len(e.notes()) != 1 {
		t.Fatal("asking to confirm deleted the note")
	}

	rec = e.do(http.MethodPost, e.notePath(id, "/delete"), url.Values{}, htmx...)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d\n%s", rec.Code, rec.Body)
	}
	if n := e.notes(); len(n) != 0 {
		t.Errorf("notes = %+v, want none", n)
	}
	mustContain(t, rec.Body.String(), "No notes yet.")
}

func TestPlainEditAndDeleteWorkWithoutScripts(t *testing.T) {
	e := newEnv(t)
	id := e.addNote("salt early", false)
	for _, suffix := range []string{"/edit", "/delete"} {
		rec := e.do(http.MethodGet, e.notePath(id, suffix), nil)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d", suffix, rec.Code)
		}
		mustContain(t, rec.Body.String(), "<html")
	}
	rec := e.do(http.MethodPost, e.notePath(id, ""), url.Values{"body": {"salt late"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != e.path("") {
		t.Errorf("plain save = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = e.do(http.MethodPost, e.notePath(id, "/delete"), url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != e.path("") {
		t.Errorf("plain delete = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if n := e.notes(); len(n) != 0 {
		t.Errorf("notes = %+v, want none", n)
	}
}

func TestEditAndDeleteRefuseANoteThatIsNotTheRecipes(t *testing.T) {
	e := newEnv(t)
	mine := e.addNote("salt early", false)
	otherRecipe, foreign := e.foreignNote()
	for name, tc := range map[string]struct {
		method, path string
		form         url.Values
	}{
		"edit form of another recipe's note": {http.MethodGet, e.notePath(foreign, "/edit"), nil},
		"save another recipe's note":         {http.MethodPost, e.notePath(foreign, ""), url.Values{"body": {"hijacked"}}},
		"confirm another recipe's note":      {http.MethodGet, e.notePath(foreign, "/delete"), nil},
		"delete another recipe's note":       {http.MethodPost, e.notePath(foreign, "/delete"), url.Values{}},
		"edit form of a missing note":        {http.MethodGet, e.notePath(999999, "/edit"), nil},
		"save a missing note":                {http.MethodPost, e.notePath(999999, ""), url.Values{"body": {"x"}}},
		// An empty body does not turn a 404 into the empty-note message.
		"empty save of another recipe's note": {http.MethodPost, e.notePath(foreign, ""), url.Values{"body": {"  "}}},
		"empty save of a missing note":        {http.MethodPost, e.notePath(999999, ""), url.Values{"body": {""}}},
		"delete a missing note":               {http.MethodPost, e.notePath(999999, "/delete"), url.Values{}},
		"note id text":                        {http.MethodGet, e.path("/notes/abc/edit"), nil},
	} {
		if rec := e.do(tc.method, tc.path, tc.form, htmx...); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, rec.Code)
		}
	}
	if n := e.notes(); len(n) != 1 || n[0].ID != mine || n[0].Body != "salt early" {
		t.Errorf("this recipe's notes = %+v, want them untouched", n)
	}
	theirs, err := e.store.ListNotes(e.ctx, otherRecipe)
	if err != nil {
		t.Fatal(err)
	}
	if len(theirs) != 1 || theirs[0].Body != "the other recipe's note" {
		t.Errorf("the other recipe's notes = %+v, want them untouched", theirs)
	}
}
