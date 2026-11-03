package layout

import (
	"strconv"
	"time"

	"github.com/kaecyra/gobbler/internal/apikey"
)

// The API key components (apikey.templ) are shared by the profile page, which
// manages a person's keys, and the household page, which manages the
// household's (ADR-00031). Each caller supplies its own paths and wording.

// APIKeyRow is one key as a list shows it. It never holds the secret.
type APIKeyRow struct {
	ID   int64
	Name string
	// Detail is the non-secret prefix and when the key was made, expires and
	// was used.
	Detail string
	// RevokePath is where the row's revoke form posts.
	RevokePath string
}

// NewAPIKeyRows turns keys into rows as of now. revokePath gives the revoke
// path of a key id; withCreator adds who made each key, for a household's list
// where the creator is not the viewer's own.
func NewAPIKeyRows(keys []apikey.Key, now time.Time, withCreator bool, revokePath func(id int64) string) []APIKeyRow {
	rows := make([]APIKeyRow, 0, len(keys))
	for _, k := range keys {
		made := "made " + k.CreatedText()
		if withCreator {
			made = "made by " + k.CreatedBy + " on " + k.CreatedText()
		}
		rows = append(rows, APIKeyRow{
			ID: k.ID, Name: k.Name, RevokePath: revokePath(k.ID),
			Detail: k.Prefix + "… " + made + ". " + k.ExpiryText(now) + ". " + k.UsedText() + ".",
		})
	}
	return rows
}

// APIKeyCreated is the page that shows a new key's secret, once.
type APIKeyCreated struct {
	Name   string
	Secret string
	// For says whose key it is when that is not the viewer's, such as "for the
	// household Alpha"; it is empty for a personal key.
	For       string
	BackHref  string
	BackLabel string
}

func (c APIKeyCreated) nameMaxLength() string { return strconv.Itoa(apikey.MaxNameRunes) }

// forText is For with the space that joins it to the name, or nothing.
func (c APIKeyCreated) forText() string {
	if c.For == "" {
		return ""
	}
	return " " + c.For
}
