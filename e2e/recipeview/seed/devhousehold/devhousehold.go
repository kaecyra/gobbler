// Package devhousehold puts the dev household on a seed program's context. It
// is test tooling (ADR-00013) shared by the e2e seed programs, which write
// recipes through the real store: the store scopes every call to the household
// on the context (ADR-00018), and the server resolves the same household for
// the dev identity, so what a seed writes is what the spec's browser sees.
package devhousehold

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/household"
)

// name is the dev household's name. It must equal the name the dev bypass
// seeds (devSeeded in internal/web); Seed does nothing for an email that
// already belongs to a household, so a mismatch would not fail, only drift.
const name = "Dev household"

// Context seeds the dev household in d, if it is not there, and returns ctx
// carrying it.
func Context(ctx context.Context, d *sql.DB) (context.Context, error) {
	hs := household.New(d)
	if err := hs.Seed(ctx, name, auth.DevEmail); err != nil {
		return nil, fmt.Errorf("seed dev household: %w", err)
	}
	h, err := hs.ForEmail(ctx, auth.DevEmail)
	if err != nil {
		return nil, fmt.Errorf("find dev household: %w", err)
	}
	return household.WithContext(ctx, h), nil
}
