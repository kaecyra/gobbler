package layout

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/kaecyra/gobbler/internal/catalog"
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
	{ErrNotFound, http.StatusNotFound},
	{catalog.ErrNotFound, http.StatusNotFound},
	{ErrInvalid, http.StatusBadRequest},
	{catalog.ErrInvalid, http.StatusBadRequest},
	{ErrConflict, http.StatusConflict},
	{catalog.ErrConflict, http.StatusConflict},
}

// StatusFor returns the HTTP status for err: the mapped status for a known
// domain error, 500 for anything else.
func StatusFor(err error) int {
	for _, e := range statusTable {
		if errors.Is(err, e.target) {
			return e.status
		}
	}
	return http.StatusInternalServerError
}

// WriteError writes err as an error page with the status StatusFor gives it.
// The text of a 4xx error is shown, since domain errors are written for the
// reader; a 500 is logged in full and shown as a generic message so internals
// never reach the page.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	status := StatusFor(err)
	message := err.Error()
	if status >= http.StatusInternalServerError {
		slog.ErrorContext(r.Context(), "request failed",
			"error", err.Error(), "method", r.Method, "path", r.URL.Path)
		message = "Something went wrong. Try again."
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if rerr := ErrorPage(status, message).Render(r.Context(), w); rerr != nil {
		// The status line is already sent; the client sees a truncated page.
		slog.ErrorContext(r.Context(), "render error page", "error", rerr.Error())
	}
}

func statusText(status int) string { return http.StatusText(status) }
