package ingest

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/config"
	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/llm/claude"
	"github.com/kaecyra/gobbler/internal/llm/gemini"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Retrier asks the LLM to read a draft's source again; *ingestcore.Fallback
// satisfies it.
type Retrier interface {
	Retry(ctx context.Context, d ingestcore.Draft) (ingestcore.Draft, error)
	Configured() bool
}

// providerFactories maps each configured provider name to its constructor. It
// is the one place that knows the provider packages; selecting a provider or a
// model is configuration only (ADR-00007).
func providerFactories() map[string]ingestcore.ProviderFactory {
	return map[string]ingestcore.ProviderFactory{
		config.ProviderClaude: func(ctx context.Context, cfg config.LLM, log *slog.Logger) (ingestcore.Provider, error) {
			p, err := claude.New(ctx, cfg, log)
			if err != nil {
				return nil, err
			}
			return p, nil
		},
		config.ProviderGemini: func(ctx context.Context, cfg config.LLM, log *slog.Logger) (ingestcore.Provider, error) {
			p, err := gemini.New(ctx, cfg, log)
			if err != nil {
				return nil, err
			}
			return p, nil
		},
	}
}

// retry asks the LLM to read the pending draft's source, whatever the draft
// scored. Only the review screen's retry button reaches it. On success the
// pending draft is replaced and the user is sent back to review it; nothing is
// saved. On any failure the rule-based draft stays as it was and the review
// screen shows the error, so a provider problem never blocks saving the draft
// (ADR-00007).
func (h *handler) retry(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	d, ok := h.pending.get(token)
	if !ok {
		layout.WriteError(w, r, errGone)
		return
	}
	fail := func(status int, msg string) {
		d.LLMError = msg
		layout.Render(w, r, status, reviewPage(h.reviewView(token, d)))
	}
	if h.llmInitErr != nil {
		h.log.ErrorContext(r.Context(), "llm provider unavailable", "error", h.llmInitErr.Error())
		fail(http.StatusServiceUnavailable, "The LLM provider could not be set up: "+h.llmInitErr.Error())
		return
	}
	out, err := h.retrier.Retry(r.Context(), d)
	switch {
	case errors.Is(err, ingestcore.ErrNoProvider):
		fail(http.StatusServiceUnavailable, "No LLM provider is configured.")
		return
	case err != nil:
		h.log.WarnContext(r.Context(), "llm retry failed", "error", err.Error())
		fail(http.StatusBadGateway, "The LLM could not read this recipe: "+err.Error())
		return
	}
	h.pending.put(token, out)
	http.Redirect(w, r, "/ingest/"+token, http.StatusSeeOther)
}

// reviewView is the view of the review screen for a pending draft.
func (h *handler) reviewView(token string, d ingestcore.Draft) reviewView {
	return reviewView{Token: token, Draft: d, CanRetry: h.retrier != nil && h.retrier.Configured()}
}

var _ Retrier = (*ingestcore.Fallback)(nil)
