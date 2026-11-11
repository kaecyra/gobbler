package layout_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	apicatalog "github.com/kaecyra/gobbler/internal/api/catalog"
	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
	apiingest "github.com/kaecyra/gobbler/internal/api/ingest"
	apiprofile "github.com/kaecyra/gobbler/internal/api/profile"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	apishop "github.com/kaecyra/gobbler/internal/api/shopping"
	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/oauth"
	"github.com/kaecyra/gobbler/internal/profile"
	"github.com/kaecyra/gobbler/internal/shopping"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// A paste-only link wraps ErrFetch so callers that only report a failed fetch
// still do, but it is the person's link to change: 400, not the 502 a fetch
// failure gets. The table order decides it (the first match wins).
func TestAPasteOnlyLinkIs400NotTheFetchFailure502(t *testing.T) {
	err := fmt.Errorf("ingest: %w", ingest.PasteOnlyError{Advice: "paste the text"})
	if !errors.Is(err, apiingest.ErrFetch) {
		t.Fatal("the paste-only error should also be an ErrFetch")
	}
	if got := layout.StatusFor(err); got != http.StatusBadRequest {
		t.Errorf("StatusFor = %d, want 400", got)
	}
	if got := layout.StatusFor(fmt.Errorf("%w: boom", ingest.ErrFetch)); got != http.StatusBadGateway {
		t.Errorf("a plain fetch failure = %d, want 502", got)
	}
}

// The status table names the api packages' re-exports. Each must stay the
// same value as the domain sentinel, so a store that returns the domain error
// still maps; a re-export turned into a fresh errors.New would map nothing.
func TestStatusTableReExportsAreTheDomainSentinels(t *testing.T) {
	cases := []struct {
		name   string
		api    error
		domain error
		status int
	}{
		{"ingest fetch", apiingest.ErrFetch, ingest.ErrFetch, http.StatusBadGateway},
		{"ingest paste only", apiingest.ErrPasteOnly, ingest.ErrPasteOnly, http.StatusBadRequest},
		{"ingest no source url", apiingest.ErrNoSourceURL, ingest.ErrNoSourceURL, http.StatusConflict},
		{"ingest empty input", apiingest.ErrEmptyInput, extract.ErrEmptyInput, http.StatusBadRequest},
		{"catalog not found", apicatalog.ErrNotFound, catalog.ErrNotFound, http.StatusNotFound},
		{"catalog invalid", apicatalog.ErrInvalid, catalog.ErrInvalid, http.StatusBadRequest},
		{"catalog conflict", apicatalog.ErrConflict, catalog.ErrConflict, http.StatusConflict},
		{"recipes not found", apirecipes.ErrNotFound, store.ErrNotFound, http.StatusNotFound},
		{"recipes stale version", apirecipes.ErrStaleVersion, store.ErrStaleVersion, http.StatusConflict},
		{"recipes not pending", apirecipes.ErrNotPending, store.ErrNotPending, http.StatusConflict},
		{"recipes on shopping list", apirecipes.ErrOnShoppingList, store.ErrOnShoppingList, http.StatusConflict},
		{"households not found", apihouseholds.ErrNotFound, household.ErrNotFound, http.StatusNotFound},
		{"households invalid", apihouseholds.ErrInvalid, household.ErrInvalid, http.StatusBadRequest},
		{"households confirmation", apihouseholds.ErrConfirmation, household.ErrConfirmation, http.StatusBadRequest},
		{"households already member", apihouseholds.ErrAlreadyMember, household.ErrAlreadyMember, http.StatusConflict},
		{"households last member", apihouseholds.ErrLastMember, household.ErrLastMember, http.StatusConflict},
		{"households not last member", apihouseholds.ErrNotLastMember, household.ErrNotLastMember, http.StatusConflict},
		{"households expired", apihouseholds.ErrExpired, household.ErrExpired, http.StatusGone},
		{"households used", apihouseholds.ErrUsed, household.ErrUsed, http.StatusGone},
		{"households revoked", apihouseholds.ErrRevoked, household.ErrRevoked, http.StatusGone},
		{"profile not found", apiprofile.ErrNotFound, profile.ErrNotFound, http.StatusNotFound},
		{"profile invalid", apiprofile.ErrInvalid, profile.ErrInvalid, http.StatusBadRequest},
		{"profile key not found", apiprofile.ErrKeyNotFound, apikey.ErrNotFound, http.StatusNotFound},
		{"profile key invalid", apiprofile.ErrKeyInvalid, apikey.ErrInvalid, http.StatusBadRequest},
		{"profile grant not found", apiprofile.ErrGrantNotFound, oauth.ErrNotFound, http.StatusNotFound},
		{"shopping not found", apishop.ErrNotFound, shopping.ErrNotFound, http.StatusNotFound},
		{"shopping invalid", apishop.ErrInvalid, shopping.ErrInvalid, http.StatusBadRequest},
		{"shopping unknown reference", apishop.ErrUnknownReference, shopping.ErrUnknownReference, http.StatusBadRequest},
		{"shopping duplicate", apishop.ErrDuplicate, shopping.ErrDuplicate, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.api, tc.domain) {
				t.Fatalf("api re-export %q is not the domain sentinel %q", tc.api, tc.domain)
			}
			wrapped := fmt.Errorf("load thing 7: %w", tc.domain)
			if got := layout.StatusFor(wrapped); got != tc.status {
				t.Errorf("StatusFor(domain error %q) = %d, want %d", tc.domain, got, tc.status)
			}
		})
	}
	if got := layout.StatusFor(apishop.ErrNotMember); got != http.StatusForbidden {
		t.Errorf("StatusFor(ErrNotMember) = %d, want %d", got, http.StatusForbidden)
	}
}
