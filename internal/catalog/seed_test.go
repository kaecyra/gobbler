package catalog

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/catalog/seed"
	"github.com/kaecyra/gobbler/internal/quantity"
)

func TestSeedIngredientsHaveAisleAndAliases(t *testing.T) {
	ings, err := seed.Ingredients()
	if err != nil {
		t.Fatal(err)
	}
	if len(ings) == 0 {
		t.Fatal("seed has no ingredients")
	}
	s := newTestStore(t)
	ctx := context.Background()
	for _, want := range ings {
		got, ok, err := s.MatchIngredient(ctx, want.Name)
		if err != nil || !ok {
			t.Errorf("seeded ingredient %q not in migrated database (ok=%v err=%v)", want.Name, ok, err)
			continue
		}
		if got.Name != normalize(want.Name) {
			t.Errorf("%q matched %q", want.Name, got.Name)
		}
		if got.Aisle == "" {
			t.Errorf("seeded ingredient %q has no aisle", want.Name)
		}
		if len(got.Aliases) == 0 {
			t.Errorf("seeded ingredient %q has no alias", want.Name)
		}
		if !got.Reviewed {
			t.Errorf("seeded ingredient %q is not reviewed", want.Name)
		}
		if want.Aisle != got.Aisle {
			t.Errorf("%q aisle = %q, seed says %q", want.Name, got.Aisle, want.Aisle)
		}
		for _, a := range want.Aliases {
			if !slices.Contains(got.Aliases, normalize(a)) {
				t.Errorf("seed alias %q of %q is not stored (stored: %v)", a, want.Name, got.Aliases)
			}
		}
		if (want.DensityGPerML == nil) != (got.DensityGPerML == nil) || (want.DensityGPerML != nil && *want.DensityGPerML != *got.DensityGPerML) {
			t.Errorf("%q density = %v, seed says %v", want.Name, got.DensityGPerML, want.DensityGPerML)
		}
	}
}

func TestSeedUnitsAgreeWithQuantity(t *testing.T) {
	units, err := seed.Units()
	if err != nil {
		t.Fatal(err)
	}
	s := newTestStore(t)
	ctx := context.Background()
	seen := map[string]bool{}
	for _, want := range units {
		seen[want.Name] = true
		got, ok, err := s.MatchUnit(ctx, want.Name)
		if err != nil || !ok {
			t.Errorf("seeded unit %q not in migrated database (ok=%v err=%v)", want.Name, ok, err)
			continue
		}
		if got.Unit.Dimension == "" || got.Unit.System == "" {
			t.Errorf("unit %q lacks dimension or system", want.Name)
		}
		if want.Other {
			if got.Unit.Dimension != quantity.DimOther {
				t.Errorf("unit %q dimension = %q, want other", want.Name, got.Unit.Dimension)
			}
		} else {
			q := quantity.MustUnit(want.Name)
			if got.Unit.System != q.System || got.Unit.Dimension != q.Dimension || !got.Unit.Factor.Equal(q.Factor) || !got.Unit.Converts() {
				t.Errorf("unit %q = %+v, quantity says %+v", want.Name, got.Unit, q)
			}
		}
		for _, a := range want.Aliases {
			if !slices.Contains(got.Aliases, normalize(a)) {
				t.Errorf("seed alias %q of unit %q is not stored (stored: %v)", a, want.Name, got.Aliases)
			}
		}
	}
	for _, q := range quantity.Units() {
		if !seen[q.Name] {
			t.Errorf("quantity unit %q is missing from the seed", q.Name)
		}
	}
}

func TestSeedEquipmentPresent(t *testing.T) {
	eq, err := seed.EquipmentList()
	if err != nil {
		t.Fatal(err)
	}
	s := newTestStore(t)
	for _, want := range eq {
		got, ok, err := s.MatchEquipment(context.Background(), want.Name)
		if err != nil || !ok {
			t.Errorf("seeded equipment %q missing (ok=%v err=%v)", want.Name, ok, err)
			continue
		}
		for _, a := range want.Aliases {
			if !slices.Contains(got.Aliases, normalize(a)) {
				t.Errorf("seed alias %q of equipment %q is not stored (stored: %v)", a, want.Name, got.Aliases)
			}
		}
	}
}

// The first migration is the seed rendered as SQL. If the JSON or the
// renderer changes, this fails until the migration (or a new one) is updated.
func TestMigrationHoldsRenderedSeed(t *testing.T) {
	raw, err := os.ReadFile("../db/migrations/00001_catalog.sql")
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := seed.SQL()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), rendered) {
		t.Error("migration 00001 does not contain the output of seed.SQL(); regenerate with go run ./internal/catalog/seed/genseed")
	}
}

func TestSeedSQLCanSelectEntries(t *testing.T) {
	got, err := seed.SQL("kale")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "INSERT INTO ingredients (name, aisle, density_g_per_ml, reviewed) VALUES ('kale'") || strings.Contains(got, "'egg'") {
		t.Errorf("SQL(kale) = %q", got)
	}
}

func TestSeedFilesAreWellFormed(t *testing.T) {
	ings, err := seed.Ingredients()
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range ings {
		if strings.TrimSpace(i.Aisle) == "" || len(i.Aliases) == 0 {
			t.Errorf("seed ingredient %q needs an aisle and an alias", i.Name)
		}
		if i.DensityGPerML != nil && *i.DensityGPerML <= 0 {
			t.Errorf("seed ingredient %q has non-positive density", i.Name)
		}
	}
}
