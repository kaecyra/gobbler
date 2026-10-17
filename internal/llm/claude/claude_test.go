package claude_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/llm/claude"
	"github.com/kaecyra/gobbler/internal/llm/llmtest"
)

// Placeholder model names: model identifiers live only in configuration.
const (
	textModel   = "test-text-model"
	visionModel = "test-vision-model"
)

// captured is what the fake Messages API saw on its last request.
type captured struct {
	mu     sync.Mutex
	header http.Header
	body   map[string]any
}

func (c *captured) set(h http.Header, body map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.header, c.body = h, body
}

func (c *captured) get() (http.Header, map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.header, c.body
}

// wire serves scenario sc in Anthropic's Messages API wire format.
func wire(sc llmtest.Scenario, seen *captured) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		seen.set(r.Header.Clone(), body)

		switch sc.Behavior {
		case llmtest.Hang:
			// Bounded, so a provider that ignores its timeout fails the suite
			// instead of hanging server shutdown.
			select {
			case <-r.Context().Done():
			case <-time.After(3 * time.Second):
			}
		case llmtest.Unavailable:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, message(sc.Payload, "tool_use", sc.Input, sc.Output))
		}
	})
}

// message is a Messages API response whose tool call carries payload as its
// input when the payload is JSON, and whose text carries it when it is not (a
// model that answered in prose instead of calling the tool).
func message(payload []byte, stop string, in, out int) string {
	content := fmt.Sprintf(`{"type":"text","text":%q}`, string(payload))
	if json.Valid(payload) {
		content = fmt.Sprintf(`{"type":"tool_use","id":"toolu_test","name":"record_recipe","input":%s}`, payload)
	}
	return fmt.Sprintf(`{"id":"msg_test","type":"message","role":"assistant","model":%q,"content":[%s],`+
		`"stop_reason":%q,"stop_sequence":null,"usage":{"input_tokens":%d,"output_tokens":%d}}`,
		textModel, content, stop, in, out)
}

// llmConfig loads the LLM settings the way production does, from the
// environment, so the API key arrives as a config.Secret.
func llmConfig(t *testing.T, key string, timeout time.Duration) config.LLM {
	t.Helper()
	t.Setenv(config.EnvDevAuthBypass, "true")
	t.Setenv(config.EnvLLMProvider, config.ProviderClaude)
	t.Setenv(config.EnvLLMTextModel, textModel)
	t.Setenv(config.EnvLLMVisionModel, visionModel)
	t.Setenv(config.EnvLLMTimeout, timeout.String())
	t.Setenv(config.EnvAnthropicAPIKey, key)
	c, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return c.LLM
}

func newProvider(t *testing.T, srv *httptest.Server, key string, timeout time.Duration, logger *slog.Logger) ingest.Provider {
	t.Helper()
	p, err := claude.New(context.Background(), llmConfig(t, key, timeout), logger, claude.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("claude.New: %v", err)
	}
	return p
}

func TestContract(t *testing.T) {
	llmtest.RunContract(t, func(t *testing.T, env llmtest.Env) ingest.Provider {
		srv := httptest.NewServer(wire(env.Scenario, &captured{}))
		t.Cleanup(srv.Close)
		return newProvider(t, srv, env.APIKey, env.Timeout, env.Logger)
	})
}

const minimalPayload = `{"name":"Toast","components":[{"ingredients":["2 slices bread"]}]}`

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestRequestShape(t *testing.T) {
	tests := []struct {
		name      string
		req       ingest.Request
		wantModel string
		wantImage bool
	}{
		{"text uses the text model", ingest.Request{Text: "Toast. 2 slices bread."}, textModel, false},
		{"image uses the vision model", ingest.Request{Image: []byte{0x89, 'P', 'N', 'G'}, ImageMIME: "image/png"}, visionModel, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			seen := &captured{}
			srv := serve(t, wire(llmtest.Scenario{Behavior: llmtest.Respond, Payload: []byte(minimalPayload)}, seen).ServeHTTP)
			p := newProvider(t, srv, "key-123", time.Second, slog.New(slog.DiscardHandler))
			if _, err := p.Structure(context.Background(), tc.req); err != nil {
				t.Fatalf("Structure: %v", err)
			}
			h, body := seen.get()
			if got := h.Get("X-Api-Key"); got != "key-123" {
				t.Errorf("x-api-key = %q, want the configured key", got)
			}
			if body["model"] != tc.wantModel {
				t.Errorf("model = %v, want %s", body["model"], tc.wantModel)
			}
			tools, _ := body["tools"].([]any)
			if len(tools) != 1 || tools[0].(map[string]any)["name"] != "record_recipe" {
				t.Fatalf("tools = %v, want the single record_recipe tool", body["tools"])
			}
			if _, forced := body["tool_choice"]; forced {
				t.Errorf("tool_choice set: forced tool use is rejected by some current models, steer from the prompt")
			}
			raw, _ := json.Marshal(body["messages"])
			if hasImage := strings.Contains(string(raw), `"type":"image"`); hasImage != tc.wantImage {
				t.Errorf("image block present = %v, want %v", hasImage, tc.wantImage)
			}
			if !tc.wantImage && !strings.Contains(string(raw), "Toast. 2 slices bread.") {
				t.Errorf("source text missing from the request: %s", raw)
			}
		})
	}
}

func TestRuleDraftSentWithoutSnapshot(t *testing.T) {
	seen := &captured{}
	srv := serve(t, wire(llmtest.Scenario{Behavior: llmtest.Respond, Payload: []byte(minimalPayload)}, seen).ServeHTTP)
	p := newProvider(t, srv, "k", time.Second, slog.New(slog.DiscardHandler))
	req := ingest.Request{Text: "x", RuleDraft: ingest.Draft{
		Source: ingest.Source{Content: []byte("SNAPSHOT-BYTES")},
		Name:   ingest.Field[string]{Value: "Rule Name"},
		Issues: []string{"rule issue text"},
		Components: []ingest.Component{{Name: "Rule Component", Lines: []ingest.Line{{Raw: "1 cup rule line"}},
			Steps: []ingest.Step{{Text: "Rule step."}}}},
	}}
	if _, err := p.Structure(context.Background(), req); err != nil {
		t.Fatalf("Structure: %v", err)
	}
	_, body := seen.get()
	raw, _ := json.Marshal(body)
	for _, want := range []string{"Rule Name", "Rule Component", "1 cup rule line", "Rule step.", "Known issue: rule issue text"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("rule draft detail %q not sent", want)
		}
	}
	if strings.Contains(string(raw), "SNAPSHOT-BYTES") {
		t.Error("the source snapshot was sent as part of the rule draft")
	}
}

func TestGapFillSendsOnlyTheGaps(t *testing.T) { // ADR-00020 decision 2
	seen := &captured{}
	srv := serve(t, wire(llmtest.Scenario{Behavior: llmtest.Respond, Payload: []byte(minimalPayload)}, seen).ServeHTTP)
	p := newProvider(t, srv, "k", time.Second, slog.New(slog.DiscardHandler))
	req := ingest.Request{Text: "x", Gaps: ingest.Gaps{
		Author: true,
		Lines:  []ingest.GapLine{{Raw: "WEAK-LINE-MARKER"}},
	}}
	if _, err := p.Structure(context.Background(), req); err != nil {
		t.Fatalf("Structure: %v", err)
	}
	_, body := seen.get()
	raw, _ := json.Marshal(body)
	for _, want := range []string{"WEAK-LINE-MARKER", "author", "Fill only these gaps"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("gap detail %q not sent", want)
		}
	}
	if strings.Contains(string(raw), "Rule-based reading") {
		t.Error("a gap-fill request must not carry the whole rule-based reading")
	}
}

func TestStepGraphGapIsSentWithTheSchemaToAnswerIt(t *testing.T) { // ADR-00020 decision 1
	seen := &captured{}
	srv := serve(t, wire(llmtest.Scenario{Behavior: llmtest.Respond, Payload: []byte(minimalPayload)}, seen).ServeHTTP)
	p := newProvider(t, srv, "k", time.Second, slog.New(slog.DiscardHandler))
	req := ingest.Request{Text: "x", Gaps: ingest.Gaps{
		Steps:     []ingest.GapStep{{Number: 2, Text: "STEP-TWO-MARKER"}},
		StepTexts: []string{"STEP-ONE-MARKER", "STEP-TWO-MARKER"},
	}}
	if _, err := p.Structure(context.Background(), req); err != nil {
		t.Fatalf("Structure: %v", err)
	}
	_, body := seen.get()
	raw, _ := json.Marshal(body)
	for _, want := range []string{"STEP-ONE-MARKER", "STEP-TWO-MARKER", "step_graph", "depends_on", "passive"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("%q not sent: %s", want, raw)
		}
	}
}

func TestUnusableResponsesAreErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"truncated by max_tokens", message([]byte(minimalPayload), "max_tokens", 1, 1)},
		{"refusal", message([]byte(minimalPayload), "refusal", 1, 1)},
		{"no tool call", `{"id":"m","type":"message","role":"assistant","model":"x","content":[],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			})
			p := newProvider(t, srv, "k", time.Second, slog.New(slog.DiscardHandler))
			got, err := p.Structure(context.Background(), ingest.Request{Text: "x"})
			if err == nil {
				t.Fatalf("accepted as %+v", got)
			}
		})
	}
}

// The contract fixtures cover shape errors; these cover what only this
// provider's decoder can be wrong about: fields outside the schema.
func TestSchemaDeviationsAreErrors(t *testing.T) {
	for name, payload := range map[string]string{
		"unknown top-level field": `{"name":"Toast","rating":5,"components":[{"ingredients":["2 slices bread"]}]}`,
		"unknown component field": `{"name":"Toast","components":[{"ingredients":["2 slices bread"],"notes":"x"}]}`,
		"blank name":              `{"name":"  ","components":[{"ingredients":["2 slices bread"]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := serve(t, wire(llmtest.Scenario{Behavior: llmtest.Respond, Payload: []byte(payload)}, &captured{}).ServeHTTP)
			p := newProvider(t, srv, "k", time.Second, slog.New(slog.DiscardHandler))
			if got, err := p.Structure(context.Background(), ingest.Request{Text: "x"}); err == nil {
				t.Fatalf("accepted as %+v", got)
			}
		})
	}
}

func TestBackendFailureIsNotATimeout(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`)
	})
	p := newProvider(t, srv, "k", time.Second, slog.New(slog.DiscardHandler))
	_, err := p.Structure(context.Background(), ingest.Request{Text: "x"})
	if err == nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want a plain backend error", err)
	}
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
	wrongProvider.Provider = config.ProviderGemini
	for name, cfg := range map[string]config.LLM{
		"no key": noKey, "no text model": noText, "no vision model": noVision,
		"no timeout": noTimeout, "wrong provider": wrongProvider,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := claude.New(context.Background(), cfg, slog.New(slog.DiscardHandler)); err == nil {
				t.Fatal("New accepted an incomplete configuration")
			}
		})
	}
	if _, err := claude.New(context.Background(), good, nil); err != nil {
		t.Fatalf("good configuration rejected: %v", err)
	}
}

// The schema offers an optional equipment list, and the rule-based reading's
// equipment is sent as context like its ingredients.
func TestEquipmentInSchemaAndContext(t *testing.T) {
	seen := &captured{}
	srv := serve(t, wire(llmtest.Scenario{Behavior: llmtest.Respond, Payload: []byte(minimalPayload)}, seen).ServeHTTP)
	p := newProvider(t, srv, "k", time.Second, slog.New(slog.DiscardHandler))
	req := ingest.Request{Text: "x", RuleDraft: ingest.Draft{
		Name:      ingest.Field[string]{Value: "Rule Name"},
		Equipment: []ingest.Equipment{{Name: "rule dutch oven"}},
	}}
	if _, err := p.Structure(context.Background(), req); err != nil {
		t.Fatalf("Structure: %v", err)
	}
	_, body := seen.get()
	tools, _ := body["tools"].([]any)
	schema, _ := tools[0].(map[string]any)["input_schema"].(map[string]any)
	props, _ := schema["properties"].(map[string]any)
	eq, _ := props["equipment"].(map[string]any)
	if eq["type"] != "array" {
		t.Fatalf("schema has no equipment array: %v", props)
	}
	if items, _ := eq["items"].(map[string]any); items["type"] != "string" {
		t.Errorf("equipment items = %v, want strings", eq["items"])
	}
	for _, r := range schema["required"].([]any) {
		if r == "equipment" {
			t.Error("equipment is required: a source may name none")
		}
	}
	raw, _ := json.Marshal(body)
	if !strings.Contains(string(raw), "rule dutch oven") {
		t.Error("the rule-based reading's equipment was not sent")
	}
	sys, _ := json.Marshal(body["system"])
	if !strings.Contains(string(sys), "equipment") {
		t.Errorf("the system prompt does not mention equipment: %s", sys)
	}
}
