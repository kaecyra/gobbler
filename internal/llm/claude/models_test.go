package claude_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/llm"
	"github.com/kaecyra/gobbler/internal/llm/claude"
)

// modelsServer serves the two pages in testdata/. They are fixtures in the
// wire shape of the Models API's list response, with placeholder identifiers;
// no test reaches the network. A request carrying after_id gets the second
// page.
func modelsServer(t *testing.T, seenKey *string) *httptest.Server {
	t.Helper()
	page1, err := os.ReadFile("testdata/models_page1.json")
	if err != nil {
		t.Fatal(err)
	}
	page2, err := os.ReadFile("testdata/models_page2.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected request to %s", r.URL.Path)
		}
		if seenKey != nil {
			*seenKey = r.Header.Get("X-Api-Key")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after_id") != "" {
			_, _ = w.Write(page2)
			return
		}
		_, _ = w.Write(page1)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func ids(ms []llm.Model) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}

// ADR-00056: Claude's list keeps active models whose listing marks structured
// output as supported, and for the vision task image input too.
func TestListModelsNarrowsPerTask(t *testing.T) {
	var key string
	srv := modelsServer(t, &key)
	for _, tc := range []struct {
		task llm.Task
		want []string
	}{
		// Across both pages: the deprecated, retired and no-structured-output
		// models are out; the text-only model is in for text and out for vision.
		{llm.TaskText, []string{"fixture-large", "fixture-text-only", "fixture-small"}},
		{llm.TaskVision, []string{"fixture-large", "fixture-small"}},
	} {
		t.Run(string(tc.task), func(t *testing.T) {
			got, err := claude.ListModels(context.Background(), config.NewSecret("key-123"), tc.task, claude.WithBaseURL(srv.URL))
			if err != nil {
				t.Fatalf("ListModels: %v", err)
			}
			if !slices.Equal(ids(got), tc.want) {
				t.Errorf("models = %v, want %v", ids(got), tc.want)
			}
			if got[0].Name != "Fixture Large" {
				t.Errorf("name = %q, want the display name", got[0].Name)
			}
		})
	}
	if key != "key-123" {
		t.Errorf("x-api-key = %q, want the key it was called with", key)
	}
}

// A listing that fails is an error and no partial list, so a caller cannot
// mistake it for an empty one.
func TestListModelsFailureIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`))
	}))
	t.Cleanup(srv.Close)
	got, err := claude.ListModels(context.Background(), config.NewSecret("bad"), llm.TaskText, claude.WithBaseURL(srv.URL))
	if err == nil || got != nil {
		t.Fatalf("got %v, %v; want an error and no models", got, err)
	}
	if _, err := claude.ListModels(context.Background(), config.Secret{}, llm.TaskText); err == nil {
		t.Error("a missing key must be an error")
	}
}
