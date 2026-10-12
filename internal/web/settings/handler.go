package settings

import (
	"context"
	"net/http"
	"sync/atomic"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/backup"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes.
const Pattern = "/settings"

// Backup is what the page uses from the backup job. *backup.Runner satisfies
// it; the live Runner must be passed, never a second one, so manual and
// scheduled runs are serialised and the status is the real one (ADR-00010).
type Backup interface {
	StatusReader
	Run(ctx context.Context) (backup.Result, error)
}

// Deps is what the routes need.
type Deps struct {
	Backup Backup
}

// Routes returns the routes of the settings page, relative to Pattern.
func Routes(d Deps) chi.Router {
	h := &handler{Deps: d}
	r := chi.NewRouter()
	r.Get("/", h.show)
	r.Post("/backup", h.backupNow)
	return r
}

type handler struct {
	Deps
	// running is set while a request-started backup runs, so a double submit
	// does not queue a second run behind the first.
	running atomic.Bool
}

func (h *handler) show(w http.ResponseWriter, r *http.Request) {
	layout.Render(w, r, http.StatusOK, settingsPage(newBackupView(h.Backup.Status(), outcomeNone)))
}

func (h *handler) backupNow(w http.ResponseWriter, r *http.Request) {
	outcome := outcomeRunning
	if h.running.CompareAndSwap(false, true) {
		outcome = h.run(r.Context())
	}
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, Pattern, http.StatusSeeOther)
		return
	}
	// A failed backup is a 200: htmx 2 does not swap a 4xx, and the outcome
	// is the content of the response, not a fault of the request.
	layout.Render(w, r, http.StatusOK, backupSection(newBackupView(h.Backup.Status(), outcome)))
}

// run does one backup and releases the guard. The job outlives the request: a
// client that gives up must not leave a half-taken snapshot, so the request
// context's cancellation is detached.
func (h *handler) run(ctx context.Context) string {
	defer h.running.Store(false)
	if _, err := h.Backup.Run(context.WithoutCancel(ctx)); err != nil {
		// The job has already logged the failure and recorded it in Status,
		// which the page shows beside this outcome.
		return outcomeFailed
	}
	return outcomeOK
}
