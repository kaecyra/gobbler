package gemini_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/llm/gemini"
	"github.com/kaecyra/gobbler/internal/llm/llmtest"
)

// Placeholder model names: real identifiers live only in configuration.
const (
	textModel   = "test-text-model"
	visionModel = "test-vision-model"
)

// recorded is what the fake backend saw for one request.
type recorded struct {
	path   string
	apiKey string
	body   map[string]any
}

// fakeBackend serves Gemini's generateContent wire format and follows the
// scenario. seen receives each request it answers.
func fakeBackend(t *testing.T, sc llmtest.Scenario, seen chan<- recorded) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("fake backend: read request body: %v", err)
			http.Error(w, "unreadable body", http.StatusBadRequest)
			return
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("fake backend: request body is not JSON: %v", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if seen != nil {
			seen <- recorded{path: r.URL.Path, apiKey: r.Header.Get("x-goog-api-key"), body: body}
		}
		switch sc.Behavior {
		case llmtest.Hang:
			<-r.Context().Done()
		case llmtest.Unavailable:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"code":503,"message":"overloaded","status":"UNAVAILABLE"}}`)
		case llmtest.Respond:
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]any{
				"candidates": []any{map[string]any{
					"content":      map[string]any{"role": "model", "parts": []any{map[string]any{"text": string(sc.Payload)}}},
					"finishReason": "STOP",
				}},
				"usageMetadata": map[string]any{"promptTokenCount": sc.Input, "candidatesTokenCount": sc.Output},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// llmConfig loads the LLM settings the way production does, from the
// environment, so the API key arrives as a config.Secret.
func llmConfig(t *testing.T, key string, timeout time.Duration) config.LLM {
	t.Helper()
	t.Setenv(config.EnvDevAuthBypass, "true")
	t.Setenv(config.EnvLLMProvider, config.ProviderGemini)
	t.Setenv(config.EnvLLMTextModel, textModel)
	t.Setenv(config.EnvLLMVisionModel, visionModel)
	t.Setenv(config.EnvLLMTimeout, timeout.String())
	t.Setenv(config.EnvGeminiAPIKey, key)
	c, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return c.LLM
}

func newProvider(t *testing.T, env llmtest.Env, srv *httptest.Server) ingest.Provider {
	t.Helper()
	p, err := gemini.New(context.Background(), llmConfig(t, env.APIKey, env.Timeout), env.Logger, gemini.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("gemini.New: %v", err)
	}
	return p
}

func TestContract(t *testing.T) {
	llmtest.RunContract(t, func(t *testing.T, env llmtest.Env) ingest.Provider {
		return newProvider(t, env, fakeBackend(t, env.Scenario, nil))
	})
}

func structure(t *testing.T, req ingest.Request) recorded {
	t.Helper()
	payload := `{"name":"Toast","components":[{"ingredients":["2 slices bread"]}]}`
	seen := make(chan recorded, 1)
	sc := llmtest.Scenario{Behavior: llmtest.Respond, Payload: []byte(payload), Input: 1, Output: 1}
	env := llmtest.Env{Scenario: sc, Timeout: 5 * time.Second, Logger: discardLogger(), APIKey: llmtest.SentinelAPIKey}
	p := newProvider(t, env, fakeBackend(t, sc, seen))
	if _, err := p.Structure(context.Background(), req); err != nil {
		t.Fatalf("Structure: %v", err)
	}
	return <-seen
}

func TestRequestShape(t *testing.T) {
	t.Run("text uses the text model and constrains the response to a JSON schema", func(t *testing.T) {
		got := structure(t, ingest.Request{Text: "Toast. 2 slices bread."})
		if !strings.Contains(got.path, textModel+":generateContent") {
			t.Errorf("path %q does not name the text model", got.path)
		}
		if got.apiKey != llmtest.SentinelAPIKey {
			t.Errorf("API key header = %q, want the configured key", got.apiKey)
		}
		cfg, _ := got.body["generationConfig"].(map[string]any)
		if cfg["responseMimeType"] != "application/json" {
			t.Errorf("responseMimeType = %v, want application/json", cfg["responseMimeType"])
		}
		schema, _ := cfg["responseSchema"].(map[string]any)
		if schema == nil {
			t.Fatalf("no responseSchema in generationConfig: %v", cfg)
		}
		req, _ := schema["required"].([]any)
		if len(req) == 0 {
			t.Errorf("schema marks no field required: %v", schema)
		}
	})
	t.Run("image uses the vision model and sends the bytes inline", func(t *testing.T) {
		got := structure(t, ingest.Request{Image: []byte{1, 2, 3}, ImageMIME: "image/png"})
		if !strings.Contains(got.path, visionModel+":generateContent") {
			t.Errorf("path %q does not name the vision model", got.path)
		}
		if !strings.Contains(mustJSON(t, got.body), `"inlineData"`) {
			t.Errorf("request carries no inlineData image part: %s", mustJSON(t, got.body))
		}
	})
	t.Run("the schema offers an optional equipment list and the rule-based equipment is sent", func(t *testing.T) {
		got := structure(t, ingest.Request{Text: "x", RuleDraft: ingest.Draft{
			Name:      ingest.Field[string]{Value: "Rule Name"},
			Equipment: []ingest.Equipment{{Name: "rule dutch oven"}},
		}})
		cfg, _ := got.body["generationConfig"].(map[string]any)
		schema, _ := cfg["responseSchema"].(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		eq, _ := props["equipment"].(map[string]any)
		if eq["type"] != "ARRAY" && eq["type"] != "array" {
			t.Fatalf("schema has no equipment array: %v", props)
		}
		for _, r := range schema["required"].([]any) {
			if r == "equipment" {
				t.Error("equipment is required: a source may name none")
			}
		}
		if !strings.Contains(mustJSON(t, got.body), "rule dutch oven") {
			t.Error("the rule-based reading's equipment was not sent")
		}
	})
	t.Run("a gap-fill sends the gaps and not a rule-based draft", func(t *testing.T) { // ADR-00020 decision 2
		got := structure(t, ingest.Request{Text: "x", Gaps: ingest.Gaps{
			Author: true,
			Lines:  []ingest.GapLine{{Raw: "WEAK-LINE-MARKER"}},
		}})
		body := mustJSON(t, got.body)
		for _, want := range []string{"WEAK-LINE-MARKER", "author", "Fill only these gaps"} {
			if !strings.Contains(body, want) {
				t.Errorf("gap detail %q not sent: %s", want, body)
			}
		}
		if strings.Contains(body, "Rule-based draft") {
			t.Error("a gap-fill request must not carry the whole rule-based draft")
		}
	})
	t.Run("the rule-based draft is sent as context", func(t *testing.T) {
		got := structure(t, ingest.Request{Text: "x", RuleDraft: ingest.Draft{Name: ingest.Field[string]{Value: "Rule Name Marker"}}})
		if !strings.Contains(mustJSON(t, got.body), "Rule Name Marker") {
			t.Errorf("rule-based draft not in the prompt: %s", mustJSON(t, got.body))
		}
	})
}

func TestNewRejectsIncompleteConfig(t *testing.T) {
	good := llmConfig(t, "k", time.Second)
	noKey := good
	noKey.APIKey = config.LLM{}.APIKey
	noText := good
	noText.TextModel = ""
	noVision := good
	noVision.VisionModel = ""
	noTimeout := good
	noTimeout.Timeout = 0
	wrongProvider := good
	wrongProvider.Provider = config.ProviderClaude
	for name, cfg := range map[string]config.LLM{
		"no key": noKey, "no text model": noText, "no vision model": noVision,
		"no timeout": noTimeout, "wrong provider": wrongProvider,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := gemini.New(context.Background(), cfg, discardLogger()); err == nil {
				t.Fatal("New accepted an incomplete configuration")
			}
		})
	}
}

// TestRejectsWhatTheFixturesDoNot covers invalid answers the shared fixtures
// do not: fields outside the schema, and Gemini responses that carry no usable
// answer at all.
func TestRejectsWhatTheFixturesDoNot(t *testing.T) {
	const valid = `{"name":"Toast","components":[{"ingredients":["2 slices bread"]}]}`
	text := func(finish, body string) string {
		return `{"candidates":[{"finishReason":"` + finish + `","content":{"role":"model","parts":[{"text":` + mustJSON(t, body) + `}]}}]}`
	}
	for name, resp := range map[string]string{
		"unknown field":                 text("STOP", `{"name":"Toast","calories":3,"components":[{"ingredients":["x"]}]}`),
		"cut off at max":                text("MAX_TOKENS", valid),
		"blocked for safety":            text("SAFETY", valid),
		"component without ingredients": text("STOP", `{"name":"x","components":[{}]}`),
		"null ingredients":              text("STOP", `{"name":"x","components":[{"ingredients":null}]}`),
		"no candidates":                 `{}`,
		"prompt blocked":                `{"promptFeedback":{"blockReason":"SAFETY"}}`,
		"candidate no answer":           `{"candidates":[{"finishReason":"STOP"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, resp)
			}))
			t.Cleanup(srv.Close)
			p, err := gemini.New(context.Background(), llmConfig(t, "k", 5*time.Second), discardLogger(), gemini.WithBaseURL(srv.URL))
			if err != nil {
				t.Fatal(err)
			}
			got, err := p.Structure(context.Background(), ingest.Request{Text: "Toast"})
			if err == nil {
				t.Fatalf("accepted as %+v", got)
			}
			if !errors.Is(err, gemini.ErrInvalidResponse) {
				t.Errorf("error does not wrap ErrInvalidResponse: %v", err)
			}
		})
	}
}

func TestOutputTokensIncludeThinking(t *testing.T) {
	body := `{"candidates":[{"finishReason":"STOP","content":{"role":"model","parts":[{"text":` +
		mustJSON(t, `{"name":"Toast","components":[{"ingredients":["x"]}]}`) +
		`}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":20,"thoughtsTokenCount":5}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	var buf bytes.Buffer
	p, err := gemini.New(context.Background(), llmConfig(t, "k", 5*time.Second), slog.New(slog.NewJSONHandler(&buf, nil)), gemini.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Structure(context.Background(), ingest.Request{Text: "Toast"}); err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log record: %v: %s", err, buf.String())
	}
	if rec["input_tokens"] != float64(10) || rec["output_tokens"] != float64(25) {
		t.Errorf("logged input/output tokens = %v/%v, want 10/25 (output includes thinking)", rec["input_tokens"], rec["output_tokens"])
	}
}
