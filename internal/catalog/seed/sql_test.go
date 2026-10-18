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

// Migration 00003 runs against databases the application has written to, so
// every statement it renders must tolerate rows that already exist.
func TestAdditionsSQLToleratesExistingRows(t *testing.T) {
	got, err := AdditionsSQL()
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range strings.Split(strings.TrimSpace(got), "\n") {
		switch {
		case strings.HasPrefix(stmt, "INSERT INTO units"):
			if !strings.HasSuffix(stmt, "ON CONFLICT (name) DO UPDATE SET reviewed = 1;") {
				t.Errorf("unit insert is not conflict-safe: %q", stmt)
			}
		case strings.HasPrefix(stmt, "INSERT INTO unit_aliases"):
			if !strings.HasSuffix(stmt, "ON CONFLICT (alias) DO NOTHING;") {
				t.Errorf("alias insert is not conflict-safe: %q", stmt)
			}
		default:
			t.Errorf("unexpected statement: %q", stmt)
		}
	}
}

// The equipment additions ship in migration 00015 (ADR-00024). If the JSON or
// the renderer changes, this fails until the migration is regenerated with
// `go run ./internal/catalog/seed/genseed -equipment-additions`.
func TestMigrationHoldsRenderedEquipmentAdditions(t *testing.T) {
	raw, err := os.ReadFile("../../db/migrations/00015_equipment_additions.sql")
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := EquipmentAdditionsSQL()
	if err != nil {
		t.Fatal(err)
	}
	if rendered == "" || !strings.Contains(string(raw), rendered) {
		t.Error("migration 00015 does not contain the output of EquipmentAdditionsSQL()")
	}
}

// An addition that repeats a name or alias of the first seed, or of another
// addition, would silently lose its alias to ON CONFLICT DO NOTHING.
func TestEquipmentAdditionsDoNotCollideWithTheSeed(t *testing.T) {
	base, err := EquipmentList()
	if err != nil {
		t.Fatal(err)
	}
	adds, err := EquipmentAdditions()
	if err != nil {
		t.Fatal(err)
	}
	if len(adds) == 0 {
		t.Fatal("no equipment additions")
	}
	owner := map[string]string{}
	claim := func(term, name string) {
		term = strings.ToLower(term)
		if prev, ok := owner[term]; ok {
			t.Errorf("%q is claimed by both %q and %q", term, prev, name)
		}
		owner[term] = name
	}
	for _, e := range base {
		claim(e.Name, e.Name)
		for _, a := range e.Aliases {
			claim(a, e.Name)
		}
	}
	for _, e := range adds {
		claim(e.Name, e.Name)
		for _, a := range e.Aliases {
			claim(a, e.Name)
		}
	}
}

func TestEquipmentAdditionsSQLSetsSizedAndToleratesExistingRows(t *testing.T) {
	got, err := EquipmentAdditionsSQL()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"INSERT INTO equipment (name, reviewed, sized) VALUES ('bundt pan', 1, 1) ON CONFLICT (name) DO UPDATE SET reviewed = 1, sized = max(sized, excluded.sized);",
		"INSERT INTO equipment (name, reviewed, sized) VALUES ('tongs', 1, 0) ON CONFLICT (name) DO UPDATE SET reviewed = 1, sized = max(sized, excluded.sized);",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("EquipmentAdditionsSQL() lacks %q", want)
		}
	}
	for _, stmt := range strings.Split(strings.TrimSpace(got), "\n") {
		if strings.HasPrefix(stmt, "INSERT INTO equipment_aliases") && !strings.HasSuffix(stmt, "ON CONFLICT (alias) DO NOTHING;") {
			t.Errorf("alias insert is not conflict-safe: %q", stmt)
		}
	}
}
