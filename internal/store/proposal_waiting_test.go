package store

import (
	"errors"
	"testing"
)

// The import page's list (ADR-00049) reads each draft's source kind and, for a
// re-import, the recipe it replaces; another household's drafts are neither
// listed nor counted.
func TestPendingDraftListForTheImportPage(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	pasted, err := s.ProposeDraft(ctx, draftInput(0))
	if err != nil {
		t.Fatal(err)
	}
	photo := draftInput(0)
	photo.Source.Kind = SnapshotOCR
	photoToken, err := s.ProposeDraft(ctx, photo)
	if err != nil {
		t.Fatal(err)
	}
	id := recipeWithSource(ctx, t, s, ids.salt)
	reimport, err := s.ProposeDraft(ctx, reimportInput(ids.salt, id, 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProposeDraft(otherCtx(t), draftInput(0)); err != nil {
		t.Fatal(err)
	}

	got, err := s.PendingDraftList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("pending drafts = %+v, want the household's three", got)
	}
	want := []struct {
		token string
		kind  SnapshotKind
		of    int64
		name  string
	}{{pasted, SnapshotText, 0, ""}, {photoToken, SnapshotOCR, 0, ""}, {reimport, SnapshotHTML, id, "Base"}}
	for i, w := range want {
		g := got[i]
		if g.Token != w.token || g.SourceKind != w.kind || g.ReimportOf != w.of || g.ReimportName != w.name || g.Reimport != (w.of != 0) {
			t.Errorf("draft %d = %+v, want token %s, kind %s, re-import of %d %q", i, g, w.token, w.kind, w.of, w.name)
		}
	}

	n, err := s.WaitingDrafts(ctx)
	if err != nil || n != 3 {
		t.Errorf("waiting drafts = %d, %v; want 3 with the re-import counted", n, err)
	}
	// The bound's count leaves the re-import out.
	if n, err := s.PendingDrafts(ctx); err != nil || n != 2 {
		t.Errorf("pending drafts of new recipes = %d, %v; want 2", n, err)
	}
	if n, err := s.WaitingDrafts(otherCtx(t)); err != nil || n != 1 {
		t.Errorf("the other household's waiting drafts = %d, %v; want 1", n, err)
	}
	if _, err := s.WaitingDrafts(noHouseholdCtx(t)); err == nil {
		t.Error("waiting drafts with no household: want an error")
	}
}

// An ended draft is named for the page a person lands on (ADR-00049): a saved
// one by the recipe it became, a discarded one by its own name. A pending one,
// or another household's, is not found.
func TestEndedDraftByToken(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	in := draftInput(0)
	in.Document = []byte(`{"name":{"v":"Pie"},"components":[]}`)
	discarded, err := s.ProposeDraft(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RejectProposal(ctx, discarded); err != nil {
		t.Fatal(err)
	}
	saved, err := s.ProposeDraft(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	rid, err := s.CreateFromDraft(ctx, saved, simpleRecipe("Renamed pie"))
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.ProposeDraft(ctx, in)
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.EndedDraftByToken(ctx, discarded)
	if err != nil || got != (EndedDraft{Name: "Pie"}) {
		t.Errorf("discarded = %+v, %v; want Pie, not saved", got, err)
	}
	got, err = s.EndedDraftByToken(ctx, saved)
	if err != nil || got != (EndedDraft{Saved: true, RecipeID: rid, Name: "Renamed pie"}) {
		t.Errorf("saved = %+v, %v; want recipe %d named as saved", got, err, rid)
	}
	for name, tc := range map[string]struct {
		token string
		other bool
	}{"pending": {pending, false}, "unknown": {"nope", false}, "another household's": {saved, true}} {
		c := ctx
		if tc.other {
			c = otherCtx(t)
		}
		if _, err := s.EndedDraftByToken(c, tc.token); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s draft: err = %v, want ErrNotFound", name, err)
		}
	}
}
