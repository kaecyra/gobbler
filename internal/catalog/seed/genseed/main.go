// Command genseed prints the seed as SQL INSERT statements, for pasting into
// a migration. With arguments it prints only the named entries:
//
//	go run ./internal/catalog/seed/genseed            # everything
//	go run ./internal/catalog/seed/genseed "kale"     # just kale
package main

import (
	"fmt"
	"os"

	"github.com/kaecyra/gobbler/internal/catalog/seed"
)

func main() {
	out, err := seed.SQL(os.Args[1:]...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "genseed:", err)
		os.Exit(1)
	}
	fmt.Print(out)
}
