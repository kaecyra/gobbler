package shopping

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	apishop "github.com/kaecyra/gobbler/internal/api/shopping"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// A request that reaches the routes without a verified member is refused
// before any handler runs, even if the middleware in front of them failed open.
func TestRoutesRefuseARequestWithoutAMember(t *testing.T) {
	f := newFixture(t)
	root := chi.NewRouter() // no asMember: no identity, no household
	root.Mount(Pattern, f.h.routes())
	for _, path := range []string{"/", "/pantry", "/1", "/1/export.txt"} {
		rec := httptest.NewRecorder()
		root.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, Pattern+path, nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s without a member: status %d, want 403", path, rec.Code)
		}
	}
}

// The service's errors map to statuses in layout's table, the one place the
// web turns a domain error into a status.
func TestServiceErrorsMapThroughTheStatusTable(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{apishop.ErrNotFound, http.StatusNotFound},
		{apishop.ErrInvalid, http.StatusBadRequest},
		{apishop.ErrUnknownReference, http.StatusBadRequest},
		{apishop.ErrDuplicate, http.StatusConflict},
		{apishop.ErrNotMember, http.StatusForbidden},
		{fmt.Errorf("add recipe 1 to list 2: %w", apishop.ErrDuplicate), http.StatusConflict},
		{errors.New("disk on fire"), http.StatusInternalServerError},
	} {
		if got := layout.StatusFor(tc.err); got != tc.want {
			t.Errorf("StatusFor(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
}
