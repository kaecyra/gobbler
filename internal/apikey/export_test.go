package apikey

import (
	"database/sql"
	"io"
)

// NewWithRand is New with the secret source replaced, for a test that needs it
// to fail.
func NewWithRand(d *sql.DB, r io.Reader) *Store {
	s := New(d)
	s.rand = r
	return s
}
