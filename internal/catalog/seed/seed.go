// Package seed holds the curated catalog data shipped with the binary
// (ADR-00008): common ingredients with aisle and aliases, units and
// equipment. The data is embedded as JSON and decoded on demand.
//
// The seed reaches the database through the first schema migration, which
// carries it as SQL. Changing a JSON file here does not change an existing
// database: an addition to the seed ships as a new migration, and the catalog
// tests assert that every entry in these files is present after migrating.
//
// Unit factors, systems and dimensions are not repeated here. Units that
// convert take them from internal/quantity, the single source of truth
// (ADR-00005); this package lists only their names and aliases.
package seed

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed ingredients.json units.json equipment.json
var files embed.FS

// Ingredient is one curated canonical ingredient. DensityGPerML is grams per
// millilitre and is set only where the value can be justified; it is omitted
// rather than guessed (ADR-00005).
type Ingredient struct {
	Name          string   `json:"name"`
	Aisle         string   `json:"aisle"`
	Aliases       []string `json:"aliases"`
	DensityGPerML *float64 `json:"density_g_per_ml,omitempty"`
}

// Unit is one seeded unit. A unit with Other unset is a convertible unit
// whose system, dimension and factor come from internal/quantity under the
// same name. A unit with Other set never converts.
type Unit struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	Other   bool     `json:"other,omitempty"`
}

// Equipment is one seeded piece of kitchen equipment.
type Equipment struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
}

// Ingredients returns the curated ingredient list.
func Ingredients() ([]Ingredient, error) { return load[Ingredient]("ingredients.json") }

// Units returns the seeded unit list.
func Units() ([]Unit, error) { return load[Unit]("units.json") }

// EquipmentList returns the seeded equipment list.
func EquipmentList() ([]Equipment, error) { return load[Equipment]("equipment.json") }

func load[T any](name string) ([]T, error) {
	raw, err := files.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read embedded seed %s: %w", name, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var out []T
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("decode embedded seed %s: %w", name, err)
	}
	return out, nil
}
