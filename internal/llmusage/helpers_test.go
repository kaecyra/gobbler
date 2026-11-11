package llmusage_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/llm"
	"github.com/kaecyra/gobbler/internal/llmusage"
)

// Placeholder identifiers: no real model name or price is written here. The
// fixture listing in testdata names the same placeholders.
const (
	claudeModel = "fixture-claude-model"
	geminiModel = "fixture-gemini-model"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return d
}

// fixedModels is a ModelSource that returns refs.
type fixedModels []llmusage.ModelRef

func (f fixedModels) Configured(context.Context) ([]llmusage.ModelRef, error) { return f, nil }

func newStore(t *testing.T, configured ...llmusage.ModelRef) (*llmusage.Store, *sql.DB) {
	t.Helper()
	d := openDB(t)
	return llmusage.New(d, fixedModels(configured)), d
}

// listing serves testdata/openrouter_models.json, the recorded-shape listing,
// and counts the requests it answers.
func listing(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	body, err := os.ReadFile("testdata/openrouter_models.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	hits := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		if r.URL.Path != "/api/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("the listing was fetched with credentials")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func okCall(model string) llmusage.Call {
	return llmusage.Call{Task: llm.TaskText, Provider: llm.ProviderClaude, Model: model, Input: 1000, Output: 2000}
}
