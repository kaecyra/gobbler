package journal

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

func (e *env) pinPath(noteID int64) string {
	return e.path("/notes/" + strconv.FormatInt(noteID, 10) + "/pin")
}

func TestANoteIsPinnedAsATipAndUnpinned(t *testing.T) {
	e := newEnv(t)
	if _, err := e.store.AddNote(e.ctx, e.id, "salt early", false); err != nil {
		t.Fatal(err)
	}
	id := e.notes()[0].ID

	rec := e.do(http.MethodPost, e.pinPath(id), url.Values{"pinned": {"1"}}, htmx...)
	if rec.Code != http.StatusOK {
		t.Fatalf("pin status = %d: %s", rec.Code, rec.Body)
	}
	if !e.notes()[0].Pinned {
		t.Error("the note is not pinned after the post")
	}
	// The fragment offers the opposite action.
	mustContain(t, rec.Body.String(), `name="pinned" value="0"`, "Unpin")

	rec = e.do(http.MethodPost, e.pinPath(id), url.Values{"pinned": {"0"}}, htmx...)
	if rec.Code != http.StatusOK {
		t.Fatalf("unpin status = %d: %s", rec.Code, rec.Body)
	}
	if e.notes()[0].Pinned {
		t.Error("the note is still pinned after unpinning")
	}
	mustContain(t, rec.Body.String(), `name="pinned" value="1"`, "Pin as tip")
}

func TestPinningAPlainPostRedirects(t *testing.T) {
	e := newEnv(t)
	if _, err := e.store.AddNote(e.ctx, e.id, "salt early", false); err != nil {
		t.Fatal(err)
	}
	rec := e.do(http.MethodPost, e.pinPath(e.notes()[0].ID), url.Values{"pinned": {"1"}})
	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", rec.Code)
	}
}

func TestPinRefusesWhatIsNotANoteOfTheRecipeOrAChoice(t *testing.T) {
	e := newEnv(t)
	if _, err := e.store.AddNote(e.ctx, e.id, "salt early", false); err != nil {
		t.Fatal(err)
	}
	id := e.notes()[0].ID
	for name, tc := range map[string]struct {
		path string
		form url.Values
		want int
	}{
		"missing note":   {e.pinPath(999999), url.Values{"pinned": {"1"}}, http.StatusNotFound},
		"note id text":   {e.path("/notes/abc/pin"), url.Values{"pinned": {"1"}}, http.StatusNotFound},
		"no choice":      {e.pinPath(id), url.Values{}, http.StatusBadRequest},
		"unknown choice": {e.pinPath(id), url.Values{"pinned": {"maybe"}}, http.StatusBadRequest},
	} {
		if rec := e.do(http.MethodPost, tc.path, tc.form, htmx...); rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", name, rec.Code, tc.want)
		}
	}
	if e.notes()[0].Pinned {
		t.Error("a refused post pinned the note")
	}
}

func TestANoteIsPinnedAsItIsAdded(t *testing.T) {
	e := newEnv(t)
	rec := e.do(http.MethodPost, e.path("/notes"), url.Values{"body": {"salt early"}, "pinned": {"1"}}, htmx...)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	notes := e.notes()
	if len(notes) != 1 || !notes[0].Pinned {
		t.Fatalf("notes = %+v, want one pinned", notes)
	}
	// It shows as a tip, offering to unpin.
	mustContain(t, rec.Body.String(), `class="note-tip"`, `name="pinned" value="0"`, "Unpin")

	// Without the box ticked the note is not a tip.
	e.do(http.MethodPost, e.path("/notes"), url.Values{"body": {"use stock"}}, htmx...)
	for _, n := range e.notes() {
		if n.Body == "use stock" && n.Pinned {
			t.Error("a note added without the box is pinned")
		}
	}
}

func TestARejectedNoteKeepsItsPinBox(t *testing.T) {
	e := newEnv(t)
	rec := e.do(http.MethodPost, e.path("/notes"), url.Values{"body": {" "}, "pinned": {"1"}}, htmx...)
	mustContain(t, rec.Body.String(), `role="alert"`, `name="pinned" value="1" checked`)
	if notes := e.notes(); len(notes) != 0 {
		t.Errorf("saved %+v", notes)
	}
}

func TestAddingANoteRefusesAnUnknownPinChoice(t *testing.T) {
	e := newEnv(t)
	rec := e.do(http.MethodPost, e.path("/notes"), url.Values{"body": {"salt early"}, "pinned": {"yes"}}, htmx...)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if notes := e.notes(); len(notes) != 0 {
		t.Errorf("saved %+v", notes)
	}
}
