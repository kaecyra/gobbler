package store

import (
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// Migration 00032 rebuilds source_snapshots. Rebuilding drops the table, which
// would unlink every waiting draft from its snapshot if the migration did not put
// the links back; and its Down keeps the original snapshot only.
func TestSnapshotMigrationKeepsDraftLinksBothWays(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := recipeWithSource(ctx, t, s, ids.salt)
	waiting, err := s.ProposeDraft(ctx, draftInput(ids.salt))
	if err != nil {
		t.Fatal(err)
	}
	done, err := s.ProposeDraft(ctx, reimportInput(ids.salt, id, 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptReimport(ctx, done, editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "Fresh" })); err != nil {
		t.Fatal(err)
	}
	links := func() int {
		return count(t, d, `SELECT COUNT(*) FROM proposals WHERE snapshot_id IS NOT NULL AND snapshot_id IN (SELECT id FROM source_snapshots)`)
	}
	if links() != 2 {
		t.Fatalf("%d proposals linked before, want 2", links())
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, d, db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 31); err != nil {
		t.Fatalf("down: %v", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM source_snapshots WHERE recipe_id = ?`, id); n != 1 {
		t.Errorf("%d snapshots after Down, want only the original", n)
	}
	var content string
	if err := d.QueryRow(`SELECT content FROM source_snapshots WHERE recipe_id = ?`, id).Scan(&content); err != nil || content != "<html>first import</html>" {
		t.Errorf("snapshot after Down = %q (%v), want the original", content, err)
	}
	if _, err := d.Exec(`INSERT INTO source_snapshots (household_id, recipe_id, kind, content, sha256, created_at, created_by) VALUES (1, ?, 'html', 'x', 'x', 'x', 'x')`, id); err == nil {
		t.Error("a second html snapshot was accepted after Down, want UNIQUE (recipe_id, kind) back")
	}
	if links() != 1 {
		t.Errorf("%d proposals linked after Down, want the waiting draft's only", links())
	}
	if _, err := p.UpTo(ctx, 32); err != nil {
		t.Fatalf("up: %v", err)
	}
	if links() != 1 {
		t.Errorf("%d proposals linked after Up, want the waiting draft's kept", links())
	}
	if got, err := s.GetProposal(ctx, waiting); err != nil || got.Snapshot == nil {
		t.Errorf("waiting draft = %+v (%v), want its snapshot", got, err)
	}
}
