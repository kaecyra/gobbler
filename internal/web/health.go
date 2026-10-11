package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

// HealthPath is the only route served without authentication (ADR-00003).
const HealthPath = "/healthz"

type healthBody struct {
	Status string `json:"status"`
	// Details is the extension point for richer health output; omitted when
	// no HealthDetails func is configured.
	Details map[string]string `json:"details,omitempty"`
}

func healthHandler(details func(ctx context.Context) map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := healthBody{Status: "ok"}
		if details != nil {
			body.Details = details(r.Context())
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err := json.NewEncoder(w).Encode(body); err != nil {
			slog.WarnContext(r.Context(), "write health response", "error", err.Error())
		}
	}
}
