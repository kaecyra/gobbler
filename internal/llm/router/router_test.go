package router_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/llm"
	"github.com/kaecyra/gobbler/internal/llm/claude"
	"github.com/kaecyra/gobbler/internal/llm/gemini"
	"github.com/kaecyra/gobbler/internal/llm/router"
	"github.com/kaecyra/gobbler/internal/settings"
)

// Placeholder identifiers: real model names live only in saved settings.
const (
	claudeModel = "fixture-claude-model"
	geminiModel = "fixture-gemini-model"
	claudeKey   = "key-for-claude"
	geminiKey   = "key-for-gemini"
)

// backend is a fake provider API that records the requests it answers.
type backend struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits []hit
}

type hit struct{ path, key string }

func (b *backend) record(path, key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hits = append(b.hits, hit{path, key})
}

func (b *backend) count() int { b.mu.Lock(); defer b.mu.Unlock(); return len(b.hits) }

func (b *backend) last() hit {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.hits) == 0 {
		return hit{}
	}
	return b.hits[len(b.hits)-1]
}

const recipeJSON = `{"name":"Toast","components":[{"ingredients":["2 slices bread"],"steps":["Toast it."]}]}`

// fakeClaude answers the Messages API with the recipe tool call, or with text
// for a transcription (a request without tools).
func fakeClaude(t *testing.T) *backend {
	t.Helper()
	b := &backend{}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		b.record(r.URL.Path, r.Header.Get("X-Api-Key"))
		content := fmt.Sprintf(`{"type":"tool_use","id":"toolu_x","name":"record_recipe","input":%s}`, recipeJSON)
		stop := "tool_use"
		if !bytes.Contains(raw, []byte(`"tools"`)) {
			content, stop = `{"type":"text","text":"transcript from claude"}`, "end_turn"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"msg_x","type":"message","role":"assistant","model":"m","content":[%s],"stop_reason":%q,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`, content, stop)
	}))
	t.Cleanup(b.srv.Close)
	return b
}

// fakeGemini answers generateContent with the recipe JSON, or with plain text
// for a request that sets no response schema (a transcription).
func fakeGemini(t *testing.T) *backend {
	t.Helper()
	b := &backend{}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		b.record(r.URL.Path, r.Header.Get("x-goog-api-key"))
		text := recipeJSON
		if !bytes.Contains(raw, []byte("responseSchema")) {
			text = "transcript from gemini"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":%q}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`, text)
	}))
	t.Cleanup(b.srv.Close)
	return b
}

func openStore(t *testing.T, key []byte) (*settings.Store, *sql.DB) {
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
	s, err := settings.New(d, key)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	return s, d
}

var testKey = bytes.Repeat([]byte{7}, settings.KeyBytes)

func field(name string) settings.Field {
	return settings.Field{Integration: llm.Integration, Name: name}
}

func setValue(t *testing.T, s *settings.Store, name, v string) {
	t.Helper()
	if err := s.SetValue(context.Background(), field(name), v); err != nil {
		t.Fatalf("save %s: %v", name, err)
	}
}

func setSecret(t *testing.T, s *settings.Store, name, v string) {
	t.Helper()
	if err := s.SetSecret(context.Background(), field(name), v); err != nil {
		t.Fatalf("save %s: %v", name, err)
	}
}

type env struct {
	store          *settings.Store
	rt             *router.Router
	claude, gemini *backend
}

func newEnv(t *testing.T) *env {
	t.Helper()
	s, _ := openStore(t, testKey)
	e := &env{store: s, claude: fakeClaude(t), gemini: fakeGemini(t)}
	e.rt = router.New(s, slog.New(slog.DiscardHandler),
		router.WithClaudeOptions(claude.WithBaseURL(e.claude.srv.URL)),
		router.WithGeminiOptions(gemini.WithBaseURL(e.gemini.srv.URL)))
	setSecret(t, s, llm.FieldAnthropicKey, claudeKey)
	setSecret(t, s, llm.FieldGeminiKey, geminiKey)
	return e
}

var photo = ingest.Request{Image: []byte{0x89, 'P', 'N', 'G'}, ImageMIME: "image/png"}

// ADR-00056 obligation 2: with Claude saved for text and Gemini for vision,
// each call reaches the provider its task names, with that provider's key and
// the task's model, and the other provider is not touched.
func TestEachTaskReachesItsOwnProvider(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	setValue(t, e.store, llm.FieldTextProvider, llm.ProviderClaude)
	setValue(t, e.store, llm.FieldTextModel, claudeModel)
	setValue(t, e.store, llm.FieldVisionProvider, llm.ProviderGemini)
	setValue(t, e.store, llm.FieldVisionModel, geminiModel)

	if _, err := e.rt.Structure(ctx, ingest.Request{Text: "Toast"}); err != nil {
		t.Fatalf("text Structure: %v", err)
	}
	if e.claude.count() != 1 || e.gemini.count() != 0 {
		t.Fatalf("text call: claude=%d gemini=%d, want 1 and 0", e.claude.count(), e.gemini.count())
	}
	if h := e.claude.last(); h.key != claudeKey {
		t.Errorf("claude saw key %q, want Claude's own", h.key)
	}

	if _, err := e.rt.Structure(ctx, photo); err != nil {
		t.Fatalf("image Structure: %v", err)
	}
	if e.claude.count() != 1 || e.gemini.count() != 1 {
		t.Fatalf("image call: claude=%d gemini=%d, want 1 and 1", e.claude.count(), e.gemini.count())
	}
	if h := e.gemini.last(); h.key != geminiKey || !strings.Contains(h.path, geminiModel) {
		t.Errorf("gemini saw %+v, want Gemini's key and the vision model", h)
	}

	got, err := e.rt.Transcribe(ctx, ingest.TranscribeRequest{Image: photo.Image, ImageMIME: "image/png"})
	if err != nil || got != "transcript from gemini" {
		t.Fatalf("Transcribe = %q, %v; want the vision task's provider to answer", got, err)
	}
	if e.claude.count() != 1 || e.gemini.count() != 2 {
		t.Errorf("after transcribe: claude=%d gemini=%d, want 1 and 2", e.claude.count(), e.gemini.count())
	}
}

// ADR-00056 obligation 3: a setting saved while the router is running is used
// by the next call. The same Router is used throughout; nothing is rebuilt.
func TestASavedSettingIsUsedByTheNextCall(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	req := ingest.Request{Text: "Toast"}

	if e.rt.Available(ctx, ingest.TaskText) {
		t.Fatal("available with nothing saved")
	}
	if _, err := e.rt.Structure(ctx, req); !errors.Is(err, ingest.ErrNoProvider) {
		t.Fatalf("with nothing saved: err = %v, want ErrNoProvider", err)
	}

	setValue(t, e.store, llm.FieldTextProvider, llm.ProviderClaude)
	setValue(t, e.store, llm.FieldTextModel, claudeModel)
	if !e.rt.Available(ctx, ingest.TaskText) || e.rt.Available(ctx, ingest.TaskVision) {
		t.Error("after saving the text task only, want text available and vision not")
	}
	if _, err := e.rt.Structure(ctx, req); err != nil {
		t.Fatalf("after saving Claude: %v", err)
	}
	if e.claude.count() != 1 || e.gemini.count() != 0 {
		t.Fatalf("claude=%d gemini=%d, want 1 and 0", e.claude.count(), e.gemini.count())
	}

	setValue(t, e.store, llm.FieldTextProvider, llm.ProviderGemini)
	setValue(t, e.store, llm.FieldTextModel, geminiModel)
	if _, err := e.rt.Structure(ctx, req); err != nil {
		t.Fatalf("after switching to Gemini: %v", err)
	}
	if e.claude.count() != 1 || e.gemini.count() != 1 {
		t.Errorf("claude=%d gemini=%d, want the switch to take effect: 1 and 1", e.claude.count(), e.gemini.count())
	}

	if err := e.store.Clear(ctx, field(llm.FieldTextProvider)); err != nil {
		t.Fatal(err)
	}
	if e.rt.Available(ctx, ingest.TaskText) {
		t.Error("still available after the provider was cleared")
	}
}

// A provider chosen with no model yet, or with a key that cannot be opened, is
// off: nothing is called and the failure is the no-provider kind, not a fault.
func TestAHalfConfiguredTaskIsOff(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	setValue(t, e.store, llm.FieldTextProvider, llm.ProviderClaude)
	if e.rt.Available(ctx, ingest.TaskText) {
		t.Error("available with a provider and no model")
	}
	setValue(t, e.store, llm.FieldTextModel, claudeModel)
	if err := e.store.Clear(ctx, field(llm.FieldAnthropicKey)); err != nil {
		t.Fatal(err)
	}
	if e.rt.Available(ctx, ingest.TaskText) {
		t.Error("available with no key")
	}
	if _, err := e.rt.Structure(ctx, ingest.Request{Text: "x"}); !errors.Is(err, ingest.ErrNoProvider) {
		t.Errorf("err = %v, want ErrNoProvider", err)
	}
	if e.claude.count() != 0 {
		t.Error("a call was made with no key")
	}
}

// Secrets sealed under another key read as unset (ADR-00057): the router starts,
// the task is off.
func TestAnUnreadableKeyTurnsTheTaskOff(t *testing.T) {
	s, d := openStore(t, testKey)
	setSecret(t, s, llm.FieldAnthropicKey, claudeKey)
	setValue(t, s, llm.FieldTextProvider, llm.ProviderClaude)
	setValue(t, s, llm.FieldTextModel, claudeModel)
	wrong, err := settings.New(d, bytes.Repeat([]byte{9}, settings.KeyBytes))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(wrong, nil)
	if rt.Available(context.Background(), ingest.TaskText) {
		t.Error("available although the key does not open")
	}
}

// A saved value nothing can use is a fault, not an off switch: the call fails
// with the reason so it can be seen.
func TestAnUnusableSavedValueFailsLoud(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	setValue(t, e.store, llm.FieldTextProvider, llm.ProviderClaude)
	setValue(t, e.store, llm.FieldTextModel, claudeModel)
	setValue(t, e.store, llm.FieldTimeout, "soon")
	if !e.rt.Available(ctx, ingest.TaskText) {
		t.Error("a fault should not read as the LLM being off")
	}
	_, err := e.rt.Structure(ctx, ingest.Request{Text: "x"})
	if err == nil || errors.Is(err, ingest.ErrNoProvider) || !strings.Contains(err.Error(), "timeout") {
		t.Errorf("err = %v, want a timeout fault", err)
	}
}

func TestModelsListsWithTheSavedKey(t *testing.T) {
	var key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"type":"model","id":"fixture-a","display_name":"A","lifecycle":"active","capabilities":{"structured_outputs":{"supported":true},"image_input":{"supported":true}}}],"has_more":false,"first_id":"fixture-a","last_id":"fixture-a"}`)
	}))
	t.Cleanup(srv.Close)
	s, _ := openStore(t, testKey)
	rt := router.New(s, nil, router.WithClaudeOptions(claude.WithBaseURL(srv.URL)))
	ctx := context.Background()

	if _, err := rt.Models(ctx, llm.ProviderClaude, llm.TaskText); !errors.Is(err, router.ErrNoKey) {
		t.Fatalf("no key saved: err = %v, want ErrNoKey", err)
	}
	setSecret(t, s, llm.FieldAnthropicKey, claudeKey)
	got, err := rt.Models(ctx, llm.ProviderClaude, llm.TaskVision)
	if err != nil || len(got) != 1 || got[0].ID != "fixture-a" {
		t.Fatalf("Models = %v, %v", got, err)
	}
	if key != claudeKey {
		t.Errorf("listing used key %q, want the saved one", key)
	}
	if _, err := rt.Models(ctx, "gpt", llm.TaskText); err == nil {
		t.Error("an unknown provider must be an error")
	}
}

// A provider that echoes the key in its error must not get it onto the page
// the message is shown on (ADR-00057).
func TestModelsErrorNeverCarriesTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key %s"}}`, r.Header.Get("X-Api-Key"))
	}))
	t.Cleanup(srv.Close)
	s, _ := openStore(t, testKey)
	setSecret(t, s, llm.FieldAnthropicKey, claudeKey)
	rt := router.New(s, nil, router.WithClaudeOptions(claude.WithBaseURL(srv.URL)))
	_, err := rt.Models(context.Background(), llm.ProviderClaude, llm.TaskText)
	if err == nil || strings.Contains(err.Error(), claudeKey) || !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Fatalf("err = %v, want the failure with the key redacted", err)
	}
}
