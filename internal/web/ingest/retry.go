package ingest

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apiingest "github.com/kaecyra/gobbler/internal/api/ingest"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Retrier asks the LLM to read a draft's source again; *apiingest.Bound
// satisfies it.
type Retrier interface {
	Retry(ctx context.Context, d apiingest.Draft) (apiingest.Draft, error)
	Configured(ctx context.Context, d apiingest.Draft) bool
}

// retry asks the LLM to read the pending draft's source, whatever the draft
// scored. Only the review screen's retry button reaches it. A draft read from a
// photo that has left memory is read again from its OCR text alone. On success
// the pending draft is replaced and the user is sent back to review it; nothing is
// saved. On any failure the rule-based draft stays as it was and the review
// screen shows the error, so a provider problem never blocks saving the draft
// (ADR-00007).
func (h *handler) retry(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	d, ok := h.draft(w, r, token)
	if !ok {
		return
	}
	fail := func(status int, msg string) {
		d.LLMError = msg
		h.renderReview(w, r, status, token, d, nil)
	}
	out, err := h.retrier.Retry(r.Context(), d)
	switch {
	case errors.Is(err, apiingest.ErrNoProvider):
		fail(http.StatusServiceUnavailable, "No LLM provider is configured.")
		return
	case err != nil && !errors.Is(err, apiingest.ErrLLMFailed):
		// The pipeline broke (the catalog could not be read), which is not an
		// LLM failure.
		layout.WriteError(w, r, fmt.Errorf("retry with LLM: %w", err))
		return
	case err != nil:
		h.log.WarnContext(r.Context(), "llm retry failed", "error", err.Error())
		fail(http.StatusBadGateway, "The LLM could not read this recipe: "+err.Error())
		return
	}
	if err := h.recipes.ReplaceDraft(r.Context(), caller.FromContext(r.Context()), token, out); err != nil {
		if errors.Is(err, apiingest.ErrDraftGone) {
			layout.WriteError(w, r, errGone)
			return
		}
		layout.WriteError(w, r, fmt.Errorf("keep the LLM's reading: %w", err))
		return
	}
	http.Redirect(w, r, "/ingest/"+token, http.StatusSeeOther)
}

var _ Retrier = (*apiingest.Bound)(nil)
