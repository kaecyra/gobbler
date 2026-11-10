package ingest

import (
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/kaecyra/gobbler/internal/db"
)

// Migration 00037 gives draft candidates offer IDs and rewrites a waiting
// draft's picture choice from a position to the ID of the candidate at it
// (ADR-00052), so a choice made before the upgrade survives it. No picture and
// Keep current picture become "none" and "keep", and a position with no
// candidate behind it is dropped. Its Down puts the numbers back.
func TestCandidateOfferMigrationKeepsTheChoiceBothWays(t *testing.T) {
	e := newReimportEnv(t)
	srv := imageSite(t)
	keep, _ := e.start(t)
	second, held := e.hold(t, e.urlDraft(t, srv.URL+"/a.png", srv.URL+"/b.png"))
	if err := e.drafts.ChoosePicture(e.ctx, second, choice(held, 1)); err != nil {
		t.Fatal(err)
	}
	none, _ := e.hold(t, e.urlDraft(t, srv.URL+"/a.png"))
	if err := e.drafts.ChoosePicture(e.ctx, none, PictureNone); err != nil {
		t.Fatal(err)
	}
	unchosen, _ := e.hold(t, e.urlDraft(t, srv.URL+"/a.png"))
	// A position past the draft's one candidate, as a draft held before the
	// migration could carry once its candidates were gone.
	dangling, _ := e.hold(t, e.urlDraft(t, srv.URL+"/a.png"))
	if _, err := e.db.ExecContext(e.ctx, `UPDATE proposals SET document = json_set(document, '$.picture', 9) WHERE token = ?`, dangling); err != nil {
		t.Fatal(err)
	}
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
	for token, want := range map[string]any{second: int64(2), none: int64(-1), keep: int64(-2), unchosen: nil, dangling: int64(9)} {
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
	if len(held.Candidates) != 2 || held.Picture != choice(held, 1) || keyOfSecond() != want {
		t.Errorf("after Up: picture %q, candidates %q; want the second candidate chosen by its new ID, under key %s", held.Picture, held.Candidates, want)
	}
	if _, err := Confirm(held); err != nil {
		t.Errorf("Confirm after Up = %v", err)
	}
	if got, _ := e.drafts.Get(e.ctx, none); got.Picture != PictureNone {
		t.Errorf("No picture after Up = %q", got.Picture)
	}
	if got, _ := e.drafts.Get(e.ctx, keep); got.Picture != PictureKeep {
		t.Errorf("Keep current picture after Up = %q", got.Picture)
	}
	for name, token := range map[string]string{"an unchosen draft": unchosen, "a position with no candidate": dangling} {
		if got, err := e.drafts.Get(e.ctx, token); err != nil || got.Picture != PictureUnchosen {
			t.Errorf("%s after Up = %q, %v; want no choice", name, got.Picture, err)
		}
		if got := stored(token); got != nil {
			t.Errorf("%s stores %v after Up, want no picture", name, got)
		}
	}
}
