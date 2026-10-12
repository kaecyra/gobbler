package layout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"

	"github.com/kaecyra/gobbler/internal/catalog"
	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/parse"
	"github.com/kaecyra/gobbler/internal/recipe/display"
	"github.com/kaecyra/gobbler/internal/store"
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
	{ingestcore.ErrFetch, http.StatusBadGateway},
	{ErrNotFound, http.StatusNotFound},
	{catalog.ErrNotFound, http.StatusNotFound},
	{ErrInvalid, http.StatusBadRequest},
	{catalog.ErrInvalid, http.StatusBadRequest},
	{ErrConflict, http.StatusConflict},
	{catalog.ErrConflict, http.StatusConflict},
	{parse.ErrEmptyText, http.StatusBadRequest},
	{extract.ErrEmptyInput, http.StatusBadRequest},
	{store.ErrNotFound, http.StatusNotFound},
	{display.ErrInvalid, http.StatusBadRequest},
	{store.ErrStaleVersion, http.StatusConflict},
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
// The page shows only the text of the matched sentinel for a 4xx and a fixed
// message for a 500, never the wrapped chain: a store may wrap a raw driver
// error under a sentinel, and its text must not reach the page. The full error
// is always logged.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	message := "Something went wrong. Try again."
	if row, ok := classify(err); ok {
		status, message = row.status, row.target.Error()
		slog.WarnContext(r.Context(), "request rejected",
			"error", err.Error(), "status", status, "method", r.Method, "path", r.URL.Path)
	} else {
		slog.ErrorContext(r.Context(), "request failed",
			"error", err.Error(), "method", r.Method, "path", r.URL.Path)
	}
	body, rerr := renderToBytes(r.Context(), ErrorPage(status, message))
	if rerr != nil {
		slog.ErrorContext(r.Context(), "render error page", "error", rerr.Error())
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeHTML(r.Context(), w, status, body)
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

func statusText(status int) string { return http.StatusText(status) }
