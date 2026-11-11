package store

import (
	"fmt"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/kaecyra/gobbler/internal/db"
)

// Migration 99999 (ADR-00053) rebuilds source_snapshots to admit the
// ocr_check kind. Up accepts the kind and still refuses an unknown one; Down
// drops the rows the old CHECK cannot hold and keeps every other snapshot and
// every waiting draft's link; Up again restores the CHECK without losing rows.
func TestOCRCheckKindMigrationRoundTrips(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	rec, err := s.Get(ctx, newRecipe(ctx, t, s, ids.salt))
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateWithSnapshots(ctx, rec,
		SnapshotInput{Kind: SnapshotOCR, Content: []byte("transcript"), ParserVersion: "21"},
		SnapshotInput{Kind: SnapshotOCRCheck, Content: []byte("tesseract text"), ParserVersion: "21"})
	if err != nil {
		t.Fatalf("a recipe with an ocr_check snapshot after Up: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO source_snapshots (household_id, recipe_id, kind, content, sha256, created_at, created_by) VALUES (1, ?, 'tesseract', 'x', 'x', 'x', 'x')`, id); err == nil {
		t.Error("an unknown snapshot kind was accepted after Up, want the CHECK")
	}
	token, err := s.ProposeDraft(ctx, draftInput(ids.salt))
	if err != nil {
		t.Fatal(err)
	}
	snaps := func() int { return count(t, d, `SELECT COUNT(*) FROM source_snapshots`) }
	links := func() int {
		return count(t, d, `SELECT COUNT(*) FROM proposals WHERE snapshot_id IN (SELECT id FROM source_snapshots)`)
	}
	if snaps() != 3 || links() != 1 {
		t.Fatalf("before: %d snapshots, %d links, want 3 and 1", snaps(), links())
	}

	p, err := goose.NewProvider(goose.DialectSQLite3, d, db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 40); err != nil {
		t.Fatalf("down: %v", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM source_snapshots WHERE kind = 'ocr_check'`); n != 0 {
		t.Errorf("%d ocr_check rows after Down, want none", n)
	}
	if snaps() != 2 || links() != 1 {
		t.Errorf("after Down: %d snapshots, %d links, want 2 and 1", snaps(), links())
	}
	if _, err := d.Exec(`INSERT INTO source_snapshots (household_id, recipe_id, kind, content, sha256, created_at, created_by) VALUES (1, ?, 'ocr_check', 'x', 'x', 'x', 'x')`, id); err == nil {
		t.Error("ocr_check accepted after Down, want the old CHECK back")
	}
	if _, err := p.UpTo(ctx, 99999); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if snaps() != 2 || links() != 1 {
		t.Errorf("after Up again: %d snapshots, %d links, want 2 and 1", snaps(), links())
	}
	if _, err := d.Exec(`INSERT INTO source_snapshots (household_id, recipe_id, kind, content, sha256, created_at, created_by) VALUES (1, ?, 'ocr_check', 'x', 'x', 'x', 'x')`, id); err != nil {
		t.Errorf("ocr_check rejected after Up again: %v", err)
	}
	if got, err := s.GetSnapshot(ctx, id, SnapshotOCR); err != nil || string(got.Content) != "transcript" {
		t.Errorf("transcript after the round trip = %q (%v)", got.Content, err)
	}
	if _, err := s.GetProposal(ctx, token); err != nil {
		t.Errorf("waiting draft after the round trip: %v", err)
	}
}

// A held photo draft carries its tesseract text in the draft document, because
// a proposal has one snapshot slot. Confirming writes it as the recipe's
// ocr_check snapshot beside the attached transcript (ADR-00053).
func TestCreateFromDraftStoresTheTesseractTextBesideTheTranscript(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	in := draftInput(ids.salt)
	in.Source = SnapshotInput{Kind: SnapshotOCR, Content: []byte("transcript"), ParserVersion: "21"}
	in.Document = []byte(fmt.Sprintf(`{"name":"P","check_text":"tesseract text","components":[{"lines":[{"raw":"x","ingredient":%d,"confidence":0.5}]}]}`, ids.salt))
	token, err := s.ProposeDraft(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Get(ctx, newRecipe(ctx, t, s, ids.salt))
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateFromDraft(ctx, token, r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSnapshot(ctx, id, SnapshotOCRCheck)
	if err != nil || string(got.Content) != "tesseract text" || got.ParserVersion != "21" {
		t.Fatalf("ocr_check snapshot = %+v (%v), want the tesseract text at parser version 21", got, err)
	}
	if got, err := s.GetSnapshot(ctx, id, SnapshotOCR); err != nil || string(got.Content) != "transcript" {
		t.Errorf("ocr snapshot = %q (%v), want the transcript", got.Content, err)
	}
}

// A draft with no check text writes no ocr_check snapshot.
func TestCreateFromDraftWithoutCheckTextWritesNoCheckSnapshot(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	token, err := s.ProposeDraft(ctx, draftInput(ids.salt))
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Get(ctx, newRecipe(ctx, t, s, ids.salt))
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateFromDraft(ctx, token, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSnapshot(ctx, id, SnapshotOCRCheck); err == nil {
		t.Error("an ocr_check snapshot was written for a draft with no check text")
	}
}
