package llmusage_test

import (
	"context"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/db/dbtest"
)

// usageMigration is the migration that adds the call record and the price
// table (ADR-00058 obligation 1): the only place its number is written in a
// test, so renumbering it changes one literal.
const usageMigration = "99999"

// The migration applies over the schema as it stood before it, and the schema
// refuses the rows the package must never write: an error with no message or a
// message on a success, a negative cost, and a price whose source and sync time
// disagree.
func TestUsageMigrationAddsTablesThatRefuseInconsistentRows(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, dbtest.MigrationsBelow(usageMigration)); err != nil {
		t.Fatalf("migrate to before %s: %v", usageMigration, err)
	}
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("migrate over %s: %v", usageMigration, err)
	}
	const call = `INSERT INTO llm_calls (called_at, task, provider, model, input_tokens, output_tokens, latency_ms, outcome, error, cost_micros) VALUES ('2026-01-01T00:00:00Z', 'text', 'claude', 'm', 0, 0, 0, `
	for name, stmt := range map[string]string{
		"a success with an error":    call + `'ok', 'boom', NULL)`,
		"a failure with no error":    call + `'error', NULL, NULL)`,
		"a negative cost":            call + `'ok', NULL, -1)`,
		"an unknown outcome":         call + `'maybe', NULL, NULL)`,
		"a manual price with a sync": `INSERT INTO llm_prices VALUES ('claude', 'm', 1, 1, 'manual', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		"a synced price with none":   `INSERT INTO llm_prices VALUES ('claude', 'm', 1, 1, 'openrouter', NULL, '2026-01-01T00:00:00Z')`,
	} {
		if _, err := d.ExecContext(ctx, stmt); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	for name, stmt := range map[string]string{
		"a success":       call + `'ok', NULL, 12)`,
		"a failure":       call + `'error', 'boom', NULL)`,
		"an unpriced one": call + `'ok', NULL, NULL)`,
	} {
		if _, err := d.ExecContext(ctx, stmt); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}
}
