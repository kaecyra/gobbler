package seed

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// SQL renders the seed as INSERT statements: units, then ingredients, then
// equipment, each group followed by its aliases, separated by blank lines.
// With names, only entries whose canonical name is listed are emitted, which
// is how a later migration adds just the new entries. Unit systems,
// dimensions and factors come from internal/quantity. Inserted rows are
// marked reviewed. The first schema migration holds exactly this output for
// the full seed; `go run ./internal/catalog/seed/genseed` prints it.
func SQL(names ...string) (string, error) {
	want := func(name string) bool { return len(names) == 0 || slices.Contains(names, name) }

	var units, ings, equip strings.Builder
	us, err := Units()
	if err != nil {
		return "", err
	}
	for _, u := range us {
		if !want(u.Name) {
			continue
		}
		qu := quantity.OtherUnit(u.Name)
		factor := "NULL"
		if !u.Other {
			var ok bool
			if qu, ok = quantity.LookupUnit(u.Name); !ok {
				return "", fmt.Errorf("seed unit %q is not in quantity's unit table; mark it other or add it there", u.Name)
			}
			factor = quote(qu.Factor.String())
		}
		fmt.Fprintf(&units, "INSERT INTO units (name, system, dimension, factor, reviewed) VALUES (%s, %s, %s, %s, 1);\n",
			quote(qu.Name), quote(string(qu.System)), quote(string(qu.Dimension)), factor)
		aliasInserts(&units, "unit_aliases", "unit_id", "units", u.Name, u.Aliases)
	}

	is, err := Ingredients()
	if err != nil {
		return "", err
	}
	for _, i := range is {
		if !want(i.Name) {
			continue
		}
		density := "NULL"
		if i.DensityGPerML != nil {
			density = fmt.Sprint(*i.DensityGPerML)
		}
		fmt.Fprintf(&ings, "INSERT INTO ingredients (name, aisle, density_g_per_ml, reviewed) VALUES (%s, %s, %s, 1);\n",
			quote(i.Name), quote(i.Aisle), density)
		aliasInserts(&ings, "ingredient_aliases", "ingredient_id", "ingredients", i.Name, i.Aliases)
	}

	es, err := EquipmentList()
	if err != nil {
		return "", err
	}
	for _, e := range es {
		if !want(e.Name) {
			continue
		}
		fmt.Fprintf(&equip, "INSERT INTO equipment (name, reviewed) VALUES (%s, 1);\n", quote(e.Name))
		aliasInserts(&equip, "equipment_aliases", "equipment_id", "equipment", e.Name, e.Aliases)
	}
	return units.String() + "\n" + ings.String() + "\n" + equip.String(), nil
}

func aliasInserts(b *strings.Builder, aliasTable, fk, table, name string, aliases []string) {
	for _, a := range aliases {
		fmt.Fprintf(b, "INSERT INTO %s (alias, %s) VALUES (%s, (SELECT id FROM %s WHERE name = %s));\n",
			aliasTable, fk, quote(strings.ToLower(a)), table, quote(name))
	}
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
