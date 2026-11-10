package layout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"

	apicatalog "github.com/kaecyra/gobbler/internal/api/catalog"
	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
	apiingest "github.com/kaecyra/gobbler/internal/api/ingest"
	apiprofile "github.com/kaecyra/gobbler/internal/api/profile"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	apishop "github.com/kaecyra/gobbler/internal/api/shopping"
	"github.com/kaecyra/gobbler/internal/parse"
	"github.com/kaecyra/gobbler/internal/recipe/display"
)

// Web-level error kinds. A feature that has no domain sentinel for a failure
// wraps one of these (fmt.Errorf("recipe %d: %w", id, layout.ErrNotFound)).
var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid request")
	ErrConflict = errors.New("conflict")
)

// statusTable is the one place a domain error becomes an HTTP status. Order
// matters only where one error wraps another; the first match wins. A new
// domain sentinel is mapped by adding a line here, never in a handler.
var statusTable = []struct {
	target error
	status int
}{
	// ErrFetch wraps whatever the HTTP client or the OS returned, so it is
	// above every row that cause could match.
	{apiingest.ErrFetch, http.StatusBadGateway},
	{apiingest.ErrNoSourceURL, http.StatusConflict},
	{ErrNotFound, http.StatusNotFound},
	{apicatalog.ErrNotFound, http.StatusNotFound},
	{ErrInvalid, http.StatusBadRequest},
	{apicatalog.ErrInvalid, http.StatusBadRequest},
	{ErrConflict, http.StatusConflict},
	{apicatalog.ErrConflict, http.StatusConflict},
	{parse.ErrEmptyText, http.StatusBadRequest},
	{apiingest.ErrEmptyInput, http.StatusBadRequest},
	{apirecipes.ErrNotFound, http.StatusNotFound},
	{display.ErrInvalid, http.StatusBadRequest},
	{apirecipes.ErrStaleVersion, http.StatusConflict},
	{apirecipes.ErrNotPending, http.StatusConflict},
	{apirecipes.ErrOnShoppingList, http.StatusConflict},
	{apihouseholds.ErrNotFound, http.StatusNotFound},
	{apihouseholds.ErrInvalid, http.StatusBadRequest},
	{apihouseholds.ErrConfirmation, http.StatusBadRequest},
	{apihouseholds.ErrAlreadyMember, http.StatusConflict},
	{apihouseholds.ErrLastMember, http.StatusConflict},
	{apihouseholds.ErrNotLastMember, http.StatusConflict},
	{apihouseholds.ErrExpired, http.StatusGone},
	{apihouseholds.ErrUsed, http.StatusGone},
	{apihouseholds.ErrRevoked, http.StatusGone},
	{apiprofile.ErrNotFound, http.StatusNotFound},
	{apiprofile.ErrInvalid, http.StatusBadRequest},
	{apiprofile.ErrKeyNotFound, http.StatusNotFound},
	{apiprofile.ErrKeyInvalid, http.StatusBadRequest},
	{apiprofile.ErrGrantNotFound, http.StatusNotFound},
	{apishop.ErrNotFound, http.StatusNotFound},
	{apishop.ErrInvalid, http.StatusBadRequest},
	{apishop.ErrUnknownReference, http.StatusBadRequest},
	{apishop.ErrDuplicate, http.StatusConflict},
	{apishop.ErrNotMember, http.StatusForbidden},
}

// classify returns the table row err matches. ok is false for an unmapped
// error, which is an internal failure.
func classify(err error) (row struct {
	target error
	status int
}, ok bool) {
	for _, e := range statusTable {
		if errors.Is(err, e.target) {
			return e, true
		}
	}
	return row, false
}

// StatusFor returns the HTTP status for err: the mapped status for a known
// domain error, 500 for anything else.
func StatusFor(err error) int {
	if row, ok := classify(err); ok {
		return row.status
	}
	return http.StatusInternalServerError
}

// WriteError writes err as an error page with the status StatusFor gives it.
// The page shows its own copy for the errors errorView knows, the matched
// sentinel's text for any other 4xx and fixed text for a 500, never the
// wrapped chain: a store may wrap a raw driver
// error under a sentinel, and its text must not reach the page. The full error
// is always logged.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	view := ErrorViewFor(http.StatusInternalServerError, "")
	if row, ok := classify(err); ok {
		status := row.status
		view = errorView(r, err, status, row.target.Error())
		WarnRejected(r, err, status)
	} else {
		slog.ErrorContext(r.Context(), "request failed",
			"error", err.Error(), "method", r.Method, "path", r.URL.Path)
	}
	body, rerr := renderToBytes(r.Context(), ErrorPageView(view))
	if rerr != nil {
		slog.ErrorContext(r.Context(), "render error page", "error", rerr.Error())
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeHTML(r.Context(), w, view.Status, body)
}

// WarnRejected logs a request refused with a 4xx, for a handler that writes
// its own error page rather than going through WriteError.
func WarnRejected(r *http.Request, err error, status int) {
	slog.WarnContext(r.Context(), "request rejected",
		"error", err.Error(), "status", status, "method", r.Method, "path", r.URL.Path)
}

// Render renders c into a buffer and only then writes status and body, so a
// component that fails part-way produces one clean error response rather than
// a partial page followed by a second status. Feature handlers use it for
// every full-page or fragment response.
func Render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	body, err := renderToBytes(r.Context(), c)
	if err != nil {
		WriteError(w, r, fmt.Errorf("render page: %w", err))
		return
	}
	writeHTML(r.Context(), w, status, body)
}

func renderToBytes(ctx context.Context, c templ.Component) ([]byte, error) {
	var buf bytes.Buffer
	if err := c.Render(ctx, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeHTML(ctx context.Context, w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		// The client went away after the status line; nothing else to send.
		slog.WarnContext(ctx, "write response body", "error", err.Error())
	}
}

// Fail writes err as an error page for the household and operator pages. Input
// the household store refused shows its own reason, so a mistyped email says
// so; any other error goes through WriteError, which shows only the mapped
// status text.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	if reason, ok := apihouseholds.InvalidReason(err); ok {
		Render(w, r, http.StatusBadRequest, ErrorPage(http.StatusBadRequest, reason))
		return
	}
	WriteError(w, r, err)
}
