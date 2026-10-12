package seed

import (
	"os"
	"strings"
	"testing"
)

// The unit additions ship in migration 00003. If the JSON or the renderer
// changes, this fails until the migration is regenerated with
// `go run ./internal/catalog/seed/genseed -unit-additions`.
func TestMigrationHoldsRenderedUnitAdditions(t *testing.T) {
	raw, err := os.ReadFile("../../db/migrations/00003_unit_additions.sql")
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := AdditionsSQL()
	if err != nil {
		t.Fatal(err)
	}
	if rendered == "" || !strings.Contains(string(raw), rendered) {
		t.Error("migration 00003 does not contain the output of AdditionsSQL()")
	}
}

func TestAdditionsSQLCreatesOnlyNewUnits(t *testing.T) {
	got, err := AdditionsSQL()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"INSERT INTO units (name, system, dimension, factor, reviewed) VALUES ('bag'",
		"INSERT INTO unit_aliases (alias, unit_id) VALUES ('c', (SELECT id FROM units WHERE name = 'cup'))",
		"INSERT INTO unit_aliases (alias, unit_id) VALUES ('gr', (SELECT id FROM units WHERE name = 'g'))",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("AdditionsSQL() lacks %q", want)
		}
	}
	for _, stmt := range strings.Split(got, "\n") {
		if !strings.Contains(stmt, "INSERT INTO units") {
			continue
		}
		for _, bad := range []string{"VALUES ('cup'", "VALUES ('g',"} {
			if strings.Contains(stmt, bad) {
				t.Errorf("AdditionsSQL() re-creates an existing unit: %q", stmt)
			}
		}
	}
}
