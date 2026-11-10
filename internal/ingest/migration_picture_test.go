package ingest

import (
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/kaecyra/gobbler/internal/db"
)

// Migration 00037 gives draft candidates offer IDs and rewrites a waiting
// draft's picture choice from a position to the ID of the candidate at it
// (ADR-00052), so a choice made before the upgrade survives it. Its Down puts
// the positions back.
func TestCandidateOfferMigrationKeepsTheChoiceBothWays(t *testing.T) {
	e := newPictureEnv(t)
	srv := imageSite(t)
	second, held := e.hold(t, e.urlDraft(t, srv.URL+"/a.png", srv.URL+"/b.png"))
	if err := e.drafts.ChoosePicture(e.ctx, second, PictureChoice(held.Candidates[1])); err != nil {
		t.Fatal(err)
	}
	none, _ := e.hold(t, e.urlDraft(t, srv.URL+"/a.png"))
	if err := e.drafts.ChoosePicture(e.ctx, none, PictureNone); err != nil {
		t.Fatal(err)
	}
	unchosen, _ := e.hold(t, e.urlDraft(t, srv.URL+"/a.png"))
	keyOfSecond := func() string {
		t.Helper()
		var key string
		if err := e.db.QueryRowContext(e.ctx, `SELECT c.key FROM draft_candidates c JOIN proposals p ON p.id = c.proposal_id
			WHERE p.token = ? AND c.position = 2`, second).Scan(&key); err != nil {
			t.Fatal(err)
		}
		return key
	}
	want := keyOfSecond()
	stored := func(token string) any {
		t.Helper()
		var v any
		if err := e.db.QueryRowContext(e.ctx, `SELECT json_extract(document, '$.picture') FROM proposals WHERE token = ?`, token).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	p, err := goose.NewProvider(goose.DialectSQLite3, e.db, db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(e.ctx, 36); err != nil {
		t.Fatalf("down: %v", err)
	}
	for token, want := range map[string]any{second: int64(2), none: int64(-1), unchosen: nil} {
		if got := stored(token); got != want {
			t.Errorf("picture after Down = %v (%T), want %v", got, got, want)
		}
	}
	if _, err := p.UpTo(e.ctx, 37); err != nil {
		t.Fatalf("up: %v", err)
	}

	held, err = e.drafts.Get(e.ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if len(held.Candidates) != 2 || held.Picture != PictureChoice(held.Candidates[1]) || keyOfSecond() != want {
		t.Errorf("after Up: picture %q, candidates %q; want the second candidate chosen by its new ID, under key %s", held.Picture, held.Candidates, want)
	}
	if _, err := Confirm(held); err != nil {
		t.Errorf("Confirm after Up = %v", err)
	}
	if got, _ := e.drafts.Get(e.ctx, none); got.Picture != PictureNone {
		t.Errorf("No picture after Up = %q", got.Picture)
	}
	if got, _ := e.drafts.Get(e.ctx, unchosen); got.Picture != PictureUnchosen {
		t.Errorf("an unchosen draft after Up = %q", got.Picture)
	}
}
