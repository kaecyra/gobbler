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
		if err := unitInserts(&units, u, true, false); err != nil {
			return "", err
		}
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
		aliasInserts(&ings, "ingredient_aliases", "ingredient_id", "ingredients", i.Name, i.Aliases, false)
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
		aliasInserts(&equip, "equipment_aliases", "equipment_id", "equipment", e.Name, e.Aliases, false)
	}
	return units.String() + "\n" + ings.String() + "\n" + equip.String(), nil
}

// unitInserts renders one unit. With create set it inserts the unit row first;
// without, the unit already exists and only its aliases are inserted. With
// rerunnable set the statements tolerate rows that already exist (a later
// migration runs against databases the application has written to): an
// existing unit of that name is adopted by marking it reviewed, and an
// existing alias is left as it is.
func unitInserts(b *strings.Builder, u Unit, create, rerunnable bool) error {
	if create {
		qu := quantity.OtherUnit(u.Name)
		factor := "NULL"
		if !u.Other {
			var ok bool
			if qu, ok = quantity.LookupUnit(u.Name); !ok {
				return fmt.Errorf("seed unit %q is not in quantity's unit table; mark it other or add it there", u.Name)
			}
			factor = quote(qu.Factor.String())
		}
		onConflict := ""
		if rerunnable {
			onConflict = " ON CONFLICT (name) DO UPDATE SET reviewed = 1"
		}
		fmt.Fprintf(b, "INSERT INTO units (name, system, dimension, factor, reviewed) VALUES (%s, %s, %s, %s, 1)%s;\n",
			quote(qu.Name), quote(string(qu.System)), quote(string(qu.Dimension)), factor, onConflict)
	}
	aliasInserts(b, "unit_aliases", "unit_id", "units", u.Name, u.Aliases, rerunnable)
	return nil
}

// AdditionsSQL renders UnitAdditions as the body of the migration that ships
// them: an INSERT for each unit the first seed did not have, then the aliases.
// Both tolerate rows the application already created (see unitInserts).
func AdditionsSQL() (string, error) {
	base, err := Units()
	if err != nil {
		return "", err
	}
	existing := map[string]bool{}
	for _, u := range base {
		existing[u.Name] = true
	}
	adds, err := UnitAdditions()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, u := range adds {
		if err := unitInserts(&b, u, !existing[u.Name], true); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

func aliasInserts(b *strings.Builder, aliasTable, fk, table, name string, aliases []string, rerunnable bool) {
	onConflict := ""
	if rerunnable {
		onConflict = " ON CONFLICT (alias) DO NOTHING"
	}
	for _, a := range aliases {
		fmt.Fprintf(b, "INSERT INTO %s (alias, %s) VALUES (%s, (SELECT id FROM %s WHERE name = %s))%s;\n",
			aliasTable, fk, quote(strings.ToLower(a)), table, quote(name), onConflict)
	}
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
