// Package seed holds the curated catalog data shipped with the binary
// (ADR-00008): common ingredients with aisle and aliases, units and
// equipment. The data is embedded as JSON and decoded on demand.
//
// The seed reaches the database through the first schema migration, which
// carries it as SQL. SQL (and `go run ./internal/catalog/seed/genseed`) renders
// it; a test asserts the migration contains exactly that output. Changing a
// JSON file does not change an existing database: an addition to the seed
// ships as a new migration whose INSERTs come from `genseed <name>...`. Units
// and unit aliases added after the first migration are listed in
// unit_additions.json instead, so units.json keeps matching migration 00001;
// `genseed -unit-additions` renders the migration that ships them.
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

//go:embed ingredients.json units.json unit_additions.json equipment.json equipment_additions.json
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
	// Sized marks pans, tins and dishes whose size matters (ADR-00019). Only
	// equipment additions set it; the first seed predates the flag and the
	// operator marked those rows by hand.
	Sized bool `json:"sized,omitempty"`
}

// UnitAdditions returns the units the first seed lacked: an entry whose name is
// in Units() adds aliases to that unit, any other entry is a new unit. They
// ship in a later migration, rendered by AdditionsSQL, because the first
// migration's SQL must stay what Units() rendered when it was written.
func UnitAdditions() ([]Unit, error) { return load[Unit]("unit_additions.json") }

// Ingredients returns the curated ingredient list.
func Ingredients() ([]Ingredient, error) { return load[Ingredient]("ingredients.json") }

// Units returns the seeded unit list.
func Units() ([]Unit, error) { return load[Unit]("units.json") }

// EquipmentList returns the seeded equipment list.
func EquipmentList() ([]Equipment, error) { return load[Equipment]("equipment.json") }

// EquipmentAdditions returns the equipment added after the first seed
// (ADR-00024). They ship in a later migration, rendered by
// EquipmentAdditionsSQL, because the first migration's SQL must stay what
// EquipmentList() rendered when it was written.
func EquipmentAdditions() ([]Equipment, error) { return load[Equipment]("equipment_additions.json") }

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
