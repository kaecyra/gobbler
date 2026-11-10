package db_test

import (
	"context"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/db/dbtest"
)

// Migration 00038 clears the sentence "Re-imported from <host>" that a
// re-import's draft used to carry as its reason, since a reason is only an
// agent's own words (ADR-00048). It touches nothing else: an agent's reason on
// a re-import, and the same words on a draft that is not a re-import, stay.
func TestReimportReasonMigrationClearsOnlyTheSentence(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, dbtest.MigrationsBelow("00038")); err != nil {
		t.Fatalf("migrate to before 00038: %v", err)
	}
	seed := []struct {
		id       int
		document string
		reason   string
	}{
		{1, `{"reimport":{"recipe":7,"base_version":1}}`, "Re-imported from example.test"},
		{2, `{"reimport":{"recipe":7,"base_version":1}}`, "Fixed the yield"},
		{3, `{"name":"x"}`, "Re-imported from example.test"},
		{4, `{"reimport":{"recipe":7,"base_version":1}}`, ""},
	}
	if _, err := d.Exec(`INSERT INTO households (id, name, created_at) VALUES (1, 'H', 't0')`); err != nil {
		t.Fatal(err)
	}
	for _, s := range seed {
		if _, err := d.Exec(`INSERT INTO proposals (id, household_id, token, kind, state, document, reason, proposed_by, created_at, state_at)
			VALUES (?, 1, ?, 'draft', 'pending', ?, ?, 'a@example.test', 't', 't')`, s.id, "token"+string(rune('0'+s.id)), s.document, s.reason); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(ctx, d, dbtest.MigrationsBelow("00039")); err != nil {
		t.Fatalf("up: %v", err)
	}
	want := map[int]string{1: "", 2: "Fixed the yield", 3: "Re-imported from example.test", 4: ""}
	for id, w := range want {
		var got string
		if err := d.QueryRow(`SELECT reason FROM proposals WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != w {
			t.Errorf("proposal %d reason = %q, want %q", id, got, w)
		}
	}
}
