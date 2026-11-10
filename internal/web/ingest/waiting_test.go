package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/household"
	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/store"
)

// ctxOf is the context of a request by h's member.
func ctxOf(h household.Household) context.Context {
	var got context.Context
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	asMember(h)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Context() })).ServeHTTP(httptest.NewRecorder(), req)
	return got
}

// hold holds d for household h and returns its token.
func (f *fixture) hold(t *testing.T, h household.Household, d ingestcore.Draft) string {
	t.Helper()
	token, err := f.rs.drafts.Add(ctxOf(h), d, store.Provenance{Person: memberEmails[h.ID]})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// ADR-00049: the import page lists the household's drafts waiting for review,
// oldest first, each with its name, source, age and marks, and links each to
// its review page. Another household's drafts are not listed.
func TestImportPageListsTheDraftsWaiting(t *testing.T) {
	f := newFixture(t)
	if body := f.do(http.MethodGet, "/", nil).Body.String(); strings.Contains(body, "Waiting for review") {
		t.Error("the list is drawn with no draft waiting")
	}

	page := f.hold(t, householdA, sampleDraft())
	pasted := sampleDraft()
	pasted.Source.Kind, pasted.Source.URL = store.SnapshotText, ""
	pasted.Name.Value = "Pasted pie"
	pastedToken := f.hold(t, householdA, pasted)
	photo := sampleDraft()
	photo.Source.Kind, photo.Source.URL = store.SnapshotOCR, ""
	photo.Name.Value = "Photo pie"
	f.hold(t, householdA, photo)
	reimport := f.holdReimport(t, f.savedRecipe(t))
	theirs := sampleDraft()
	theirs.Name.Value = "Their secret pie"
	f.hold(t, householdB, theirs)

	// An agent revises the pasted draft (ADR-00048); the list does not mark it.
	p, err := f.rs.proposals.GetProposal(f.ctx(t), pastedToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.rs.proposals.ReviseDraft(f.ctx(t), pastedToken, p.Document, store.DraftRevision(p.Document), "tidied"); err != nil {
		t.Fatal(err)
	}

	rec := f.do(http.MethodGet, "/", nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	list := between(t, body, `data-testid="waiting-drafts-list"`, "</section>")
	if strings.Contains(body, "Their secret pie") {
		t.Error("another household's draft is listed")
	}
	if got := strings.Count(list, `data-testid="waiting-draft"`); got != 4 {
		t.Errorf("listed %d drafts, want 4", got)
	}
	for _, want := range []string{
		`href="/ingest/` + page + `"`, "example.test", "today",
		"Pasted text", "Photo", "Photo pie",
		`href="/ingest/` + reimport + `"`, "Re-import of Old pie",
	} {
		if !strings.Contains(list, want) {
			t.Errorf("list has no %q", want)
		}
	}
	if strings.Contains(list, "evised") || strings.Contains(list, "tidied") {
		t.Error("the list marks a draft an agent revised, or prints its reason")
	}
	if strings.Count(list, "Re-import of") != 1 {
		t.Error("the re-import mark is on a draft that is not a re-import")
	}
	// Oldest first: the order they were held in.
	if i, j := strings.Index(list, "Apple pie"), strings.Index(list, "Pasted pie"); i < 0 || j < i {
		t.Error("the drafts are not oldest first")
	}
	// The list sits above the import form.
	if strings.Index(body, "Waiting for review") > strings.Index(body, `action="/ingest"`) {
		t.Error("the list is not above the forms")
	}
}

func TestSourceLabel(t *testing.T) {
	for _, tc := range []struct {
		d    apirecipes.DraftSummary
		want string
	}{
		{apirecipes.DraftSummary{SourceURL: "https://www.example.test/a/b", SourceKind: store.SnapshotHTML}, "example.test"},
		{apirecipes.DraftSummary{SourceKind: store.SnapshotText}, "Pasted text"},
		{apirecipes.DraftSummary{SourceKind: store.SnapshotOCR}, "Photo"},
		{apirecipes.DraftSummary{}, "Pasted text"},
	} {
		if got := sourceLabel(tc.d); got != tc.want {
			t.Errorf("sourceLabel(%+v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestRowsOfNamesAnUntitledDraftAndCountsAge(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	rows := rowsOf([]apirecipes.DraftSummary{
		{Token: "a", CreatedAt: now.Add(-72 * time.Hour), Reimport: true},
		{Token: "b", Name: " Pie ", CreatedAt: now},
	}, now)
	if rows[0].Name != "Untitled draft" || rows[0].Age != "3 days ago" || rows[0].ReimportOf != "a saved recipe" || rows[0].Path != "/ingest/a" {
		t.Errorf("row 0 = %+v", rows[0])
	}
	if rows[1].Name != "Pie" || rows[1].Age != "today" || rows[1].ReimportOf != "" {
		t.Errorf("row 1 = %+v", rows[1])
	}
}

// ADR-00049: confirming a draft lands on the oldest draft still waiting, with
// a message saying what was saved; discarding the last lands on the import
// page, with a message saying what was discarded. Neither counts the drafts
// left (#588).
func TestConfirmAndCancelMoveOnToTheNextDraft(t *testing.T) {
	f := newFixture(t)
	first, second, third := f.ingestText(t), f.ingestText(t), f.ingestText(t)

	rec := f.do(http.MethodPost, "/"+first+"/confirm", reviewForm())
	want := "/ingest/" + second + "?done=" + first
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
		t.Fatalf("confirm = %d to %q, want 303 to %s", rec.Code, rec.Header().Get("Location"), want)
	}
	body := f.do(http.MethodGet, local(want), nil).Body.String()
	note := between(t, body, `data-testid="draft-done"`, "</div>")
	if !strings.Contains(note, `Saved <a href="/recipes/1-`) || strings.Contains(note, "waiting") {
		t.Errorf("message = %s", note)
	}

	// From htmx, the same destination as an HX-Redirect.
	req := newFormRequest("/"+second+"/confirm", reviewForm())
	req.Header.Set("HX-Request", "true")
	rec = f.serve(req)
	if got := rec.Header().Get("HX-Redirect"); got != "/ingest/"+third+"?done="+second {
		t.Errorf("htmx confirm redirects to %q, want the next draft", got)
	}

	rec = f.do(http.MethodPost, "/"+third+"/cancel", url.Values{})
	if want := "/ingest?done=" + third; rec.Header().Get("Location") != want {
		t.Fatalf("cancelling the last draft went to %q, want %s", rec.Header().Get("Location"), want)
	}
	body = f.do(http.MethodGet, local(rec.Header().Get("Location")), nil).Body.String()
	note = between(t, body, `data-testid="draft-done"`, "</div>")
	if !strings.Contains(note, "Discarded Apple pie.") || strings.Contains(note, "waiting") {
		t.Errorf("message = %s", note)
	}
}

// With none waiting, a confirm goes to the saved recipe as before; a discard
// with others waiting moves on to the oldest.
func TestConfirmTheLastDraftGoesToTheRecipeAndCancelMovesOn(t *testing.T) {
	f := newFixture(t)
	first, second := f.ingestText(t), f.ingestText(t)
	rec := f.do(http.MethodPost, "/"+first+"/cancel", url.Values{})
	if want := "/ingest/" + second + "?done=" + first; rec.Header().Get("Location") != want {
		t.Fatalf("cancel went to %q, want %s", rec.Header().Get("Location"), want)
	}
	body := f.do(http.MethodGet, local(rec.Header().Get("Location")), nil).Body.String()
	if !strings.Contains(body, "Discarded Apple pie.") {
		t.Error("the next draft's page does not say what was discarded")
	}
	rec = f.do(http.MethodPost, "/"+second+"/confirm", reviewForm())
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/recipes/1") {
		t.Errorf("confirming the last draft went to %q, want the recipe", loc)
	}
}

// The message names only an ended draft of the household's own: a pending
// draft, another household's ended draft or nonsense shows nothing, and the
// page still renders.
func TestDoneShowsOnlyTheHouseholdsEndedDraft(t *testing.T) {
	f := newFixture(t)
	pending := f.ingestText(t)
	theirs := f.hold(t, householdB, sampleDraft())
	if err := f.rs.drafts.Discard(ctxOf(householdB), theirs); err != nil {
		t.Fatal(err)
	}
	for _, done := range []string{pending, theirs, "nonsense", strings.Repeat("a", 200)} {
		for _, path := range []string{"/", "/" + pending} {
			rec := f.do(http.MethodGet, path+"?done="+url.QueryEscape(done), nil)
			if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `data-testid="draft-done"`) {
				t.Errorf("%s?done=%.12s = %d, with a message: %v", path, done, rec.Code, strings.Contains(rec.Body.String(), "draft-done"))
			}
		}
	}
}

// Another household's request to cancel a draft is answered as if the draft
// were unknown, and moves on to nothing: it names no draft of either household.
func TestCancelOfAnotherHouseholdsDraftNamesNothing(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	asB := asMember(householdB)(f.h.routes())
	req := newFormRequest("/"+token+"/cancel", url.Values{})
	rec := httptest.NewRecorder()
	asB.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ingest" {
		t.Errorf("cancel as household B = %d to %q, want 303 to /ingest", rec.Code, rec.Header().Get("Location"))
	}
	unknown := newFormRequest("/nope/cancel", url.Values{})
	rec2 := httptest.NewRecorder()
	asB.ServeHTTP(rec2, unknown)
	if rec2.Header().Get("Location") != rec.Header().Get("Location") {
		t.Error("an unknown token and another household's are answered differently")
	}
}

// A saved draft whose recipe is gone is named without a link to nothing.
func TestDoneNoteWithoutARecipe(t *testing.T) {
	var b strings.Builder
	if err := doneNote(&doneView{Saved: true, Name: "Pie"}).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Saved Pie.") || strings.Contains(b.String(), "<a") {
		t.Errorf("note = %s", b.String())
	}
}

// local is a redirect's location as the handler's routes, mounted at /ingest,
// see it.
func local(loc string) string {
	p := strings.TrimPrefix(loc, "/ingest")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}
