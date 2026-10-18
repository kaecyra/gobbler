package editor

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/recipe"
)

func (e *env) postCuisine(cuisine string) result {
	e.t.Helper()
	f := e.form()
	f.Set("cuisine", cuisine)
	return e.post(f)
}

// ADR-00025: the editor sets and clears the cuisine, and offers the closed list.
func TestTheEditorSetsAndClearsTheCuisine(t *testing.T) {
	e := newEnv(t)
	if res := e.postCuisine("middle_eastern"); res.Code != http.StatusSeeOther {
		t.Fatalf("status = %d: %s", res.Code, res.body())
	}
	if got, _ := e.stored(); got.Cuisine != recipe.CuisineMiddleEastern {
		t.Errorf("stored cuisine = %q, want middle_eastern", got.Cuisine)
	}
	body := e.get().body()
	for _, want := range []string{`name="cuisine"`, `<option value="middle_eastern" selected`, `>Middle Eastern</option>`, `<option value="other"`} {
		if !strings.Contains(body, want) {
			t.Errorf("editor page lacks %s", want)
		}
	}
	if res := e.postCuisine(""); res.Code != http.StatusSeeOther {
		t.Fatalf("clearing status = %d: %s", res.Code, res.body())
	}
	if got, _ := e.stored(); got.Cuisine != "" {
		t.Errorf("after clearing: cuisine = %q, want unset", got.Cuisine)
	}
}

func TestACuisineOutsideTheListIsRefusedAndSavesNothing(t *testing.T) {
	e := newEnv(t)
	_, before := e.stored()
	res := e.postCuisine("asian")
	if res.Code != http.StatusBadRequest || !strings.Contains(res.body(), `is not a cuisine`) {
		t.Fatalf("status = %d, want 400 with a message:\n%s", res.Code, res.body())
	}
	if _, v := e.stored(); v != before {
		t.Errorf("version %d -> %d; a refused save must change nothing", before, v)
	}
}

// On the review screen the cuisine ingest proposed is shown, and what the user
// picks goes back into the draft (ADR-00025, ADR-00006).
func TestReviewShowsAndRoundTripsTheCuisine(t *testing.T) {
	e := newEnv(t)
	u := NewUnsaved(e.cat)
	base := fieldsBase()
	base.Cuisine = recipe.CuisineJapanese
	form, err := u.Open(e.ctx, base, ReviewOptions{Action: "/x"})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := form.Component().Render(e.ctx, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `<option value="japanese" selected`) {
		t.Error("review form does not show the proposed cuisine")
	}

	v := postedGraphForm(t, e, u, base)
	v.Set("cuisine", "thai")
	out, err := u.Submit(e.ctx, v, base, ReviewOptions{Action: "/x"})
	if err != nil || !out.Ready || out.Draft.Cuisine != recipe.CuisineThai {
		t.Fatalf("after choosing thai: ready=%v err=%v cuisine=%q", out.Ready, err, out.Draft.Cuisine)
	}
	v.Set("cuisine", "")
	out, err = u.Submit(e.ctx, v, base, ReviewOptions{Action: "/x"})
	if err != nil || !out.Ready || out.Draft.Cuisine != "" {
		t.Errorf("after clearing: ready=%v err=%v cuisine=%q", out.Ready, err, out.Draft.Cuisine)
	}
}
