// Package dbtest holds database helpers that only tests call, kept out of
// package db so the binary does not link testing/fstest for them.
package dbtest

import (
	"fmt"
	"io/fs"
	"strings"
	"testing/fstest"

	"github.com/kaecyra/gobbler/internal/db"
)

// MigrationsBelow returns the embedded migrations numbered below prefix (such
// as "00018"): the schema as it stood before that migration, for a test that
// writes rows in the older shape and then migrates over them. Later
// migrations are left out too, since goose refuses to apply one out of order.
// It panics on an unreadable embed, as db.Migrations does.
func MigrationsBelow(prefix string) fs.FS {
	all := db.Migrations()
	entries, err := fs.ReadDir(all, ".")
	if err != nil {
		panic(fmt.Sprintf("read embedded migrations: %v", err))
	}
	out := fstest.MapFS{}
	for _, e := range entries {
		if e.Name() >= prefix || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := fs.ReadFile(all, e.Name())
		if err != nil {
			panic(fmt.Sprintf("read embedded migration %s: %v", e.Name(), err))
		}
		out[e.Name()] = &fstest.MapFile{Data: b}
	}
	return out
}
