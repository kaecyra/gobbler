// Command genseed prints the seed as SQL INSERT statements, for pasting into
// a migration. With arguments it prints only the named entries:
//
//	go run ./internal/catalog/seed/genseed            # everything
//	go run ./internal/catalog/seed/genseed "kale"     # just kale
//	go run ./internal/catalog/seed/genseed -unit-additions
//	                                                   # units and aliases added after the first migration
//	go run ./internal/catalog/seed/genseed -equipment-additions
//	                                                   # equipment added after the first migration
package main

import (
	"fmt"
	"os"

	"github.com/kaecyra/gobbler/internal/catalog/seed"
)

func main() {
	render := func() (string, error) { return seed.SQL(os.Args[1:]...) }
	if len(os.Args) == 2 && os.Args[1] == "-unit-additions" {
		render = seed.AdditionsSQL
	}
	if len(os.Args) == 2 && os.Args[1] == "-equipment-additions" {
		render = seed.EquipmentAdditionsSQL
	}
	out, err := render()
	if err != nil {
		fmt.Fprintln(os.Stderr, "genseed:", err)
		os.Exit(1)
	}
	fmt.Print(out)
}
