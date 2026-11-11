package gemini_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/llm"
	"github.com/kaecyra/gobbler/internal/llm/gemini"
)

// ADR-00056: Gemini's list keeps the models that support content generation,
// for both tasks, with the "models/" prefix removed. The fixture is in the wire
// shape of the models list response, with placeholder identifiers.
func TestListModelsKeepsContentGenerators(t *testing.T) {
	body, err := os.ReadFile("testdata/models.json")
	if err != nil {
		t.Fatal(err)
	}
	var key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("x-goog-api-key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	for _, task := range llm.Tasks {
		t.Run(string(task), func(t *testing.T) {
			got, err := gemini.ListModels(context.Background(), config.NewSecret("key-123"), task, gemini.WithBaseURL(srv.URL))
			if err != nil {
				t.Fatalf("ListModels: %v", err)
			}
			ids := make([]string, len(got))
			for i, m := range got {
				ids[i] = m.ID
			}
			if want := []string{"fixture-flash", "fixture-pro"}; !slices.Equal(ids, want) {
				t.Errorf("models = %v, want %v", ids, want)
			}
			if got[0].Name != "Fixture Flash" {
				t.Errorf("name = %q, want the display name", got[0].Name)
			}
		})
	}
	if key != "key-123" {
		t.Errorf("API key header = %q", key)
	}
}

func TestListModelsFailureIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"API key not valid","status":"PERMISSION_DENIED"}}`))
	}))
	t.Cleanup(srv.Close)
	got, err := gemini.ListModels(context.Background(), config.NewSecret("bad"), llm.TaskText, gemini.WithBaseURL(srv.URL))
	if err == nil || got != nil {
		t.Fatalf("got %v, %v; want an error and no models", got, err)
	}
	if _, err := gemini.ListModels(context.Background(), config.Secret{}, llm.TaskText); err == nil {
		t.Error("a missing key must be an error")
	}
}
