package store

import (
	"bytes"
	"errors"
	"testing"
)

// An agent's revision replaces a draft only while the draft is at the revision
// it was made from, checked where the draft is written (ADR-00048): a person's
// edit in between wins, and the agent's document is not stored. The reason is
// stored with a revision, and a person's later replace keeps it.
func TestReviseDraftChecksTheRevisionAndStoresTheReason(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	token, err := s.ProposeDraft(ctx, draftInput(0))
	if err != nil {
		t.Fatal(err)
	}
	read := DraftRevision(draftDoc(0))

	person := []byte(`{"name":"Person","components":[]}`)
	if err := s.ReplaceDraft(ctx, token, person); err != nil {
		t.Fatal(err)
	}
	agent := []byte(`{"name":"Agent","components":[]}`)
	if err := s.ReviseDraft(ctx, token, agent, read, "rename"); !errors.Is(err, ErrStaleVersion) {
		t.Fatalf("revising from an old revision = %v, want ErrStaleVersion", err)
	}
	p, err := s.GetProposal(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.Document, person) || p.Reason != "" {
		t.Fatalf("after a refused revision the document is %s and the reason %q; want the person's edit", p.Document, p.Reason)
	}

	if err := s.ReviseDraft(ctx, token, agent, DraftRevision(person), "rename"); err != nil {
		t.Fatal(err)
	}
	if p, _ = s.GetProposal(ctx, token); !bytes.Equal(p.Document, agent) || p.Reason != "rename" {
		t.Fatalf("after revising the document is %s and the reason %q", p.Document, p.Reason)
	}
	if p.Snapshot == nil || string(p.Snapshot.Content) != "2 eggs" || p.Snapshot.ParserVersion != "7" {
		t.Errorf("the snapshot changed: %+v", p.Snapshot)
	}
	if err := s.ReplaceDraft(ctx, token, person); err != nil {
		t.Fatal(err)
	}
	if p, _ = s.GetProposal(ctx, token); p.Reason != "rename" {
		t.Errorf("a person's replace dropped the reason: %q", p.Reason)
	}
	if err := s.ReviseDraft(ctx, token, agent, DraftRevision(person), " "); !errors.Is(err, ErrInvalid) {
		t.Errorf("a revision with no reason = %v, want ErrInvalid", err)
	}
}

// The pending draft list holds the household's pending drafts with their
// name, source address and whether an agent has revised them.
func TestPendingDraftList(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	in := draftInput(0)
	in.Document = []byte(`{"name":{"v":"Pie"},"source_url":"https://example.test/pie","components":[]}`)
	token, err := s.ProposeDraft(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	gone, err := s.ProposeDraft(ctx, draftInput(0))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RejectProposal(ctx, gone); err != nil {
		t.Fatal(err)
	}
	if err := s.ReviseDraft(ctx, token, in.Document, DraftRevision(in.Document), "why"); err != nil {
		t.Fatal(err)
	}
	got, err := s.PendingDraftList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Token != token || got[0].Name != "Pie" || got[0].SourceURL != "https://example.test/pie" || got[0].Reason != "why" || got[0].Reimport || got[0].CreatedAt.IsZero() {
		t.Errorf("pending drafts = %+v", got)
	}
}
