package mcp

import (
	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/store"
)

// provenance says who is behind a proposal an agent made with tool: the
// credential, as snapshotted when it was verified (the same snapshot the write
// logs keep), and the person it acts as. A household credential acts for no one
// person, so it is attributed to its household, as the key write log does.
func provenance(c Credential, hid int64, tool string) store.Provenance {
	owner := apikey.OwnerName(c.Email, hid)
	return store.Provenance{
		Person: owner,
		Credential: &store.Credential{
			Kind: c.Kind, ID: c.ID, Ref: c.Ref, Owner: owner, CreatedAt: c.CreatedAt,
		},
		Tool: tool,
	}
}
