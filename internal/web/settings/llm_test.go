package settings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/llm"
	"github.com/kaecyra/gobbler/internal/llm/claude"
	"github.com/kaecyra/gobbler/internal/llm/router"
	store "github.com/kaecyra/gobbler/internal/settings"
)

// fakeLister answers Models from a table, or fails.
type fakeLister struct {
	models map[string][]llm.Model // by provider
	err    error
}

func (f *fakeLister) Models(_ context.Context, provider string, _ llm.Task) ([]llm.Model, error) {
	if f.err != nil {
		return nil, f.err
	}
	if _, ok := f.models[provider]; !ok {
		return nil, fmt.Errorf("claude API key unavailable: %w", llm.ErrNotConfigured)
	}
	return f.models[provider], nil
}

func listing(ids ...string) []llm.Model {
	var out []llm.Model
	for _, id := range ids {
		out = append(out, llm.Model{ID: id, Name: id})
	}
	return out
}

func llmField(name string) store.Field { return store.Field{Integration: llm.Integration, Name: name} }

func llmHandler(t *testing.T, lister ModelLister) (http.Handler, *store.Store) {
	t.Helper()
	_, s, _ := integrationsFixture(t, bytes.Repeat([]byte{1}, store.KeyBytes))
	sec := LLMSection(LLMDeps{Values: s, Models: lister})
	return Routes(Deps{Backup: &fakeBackup{}, Integrations: Integrations{Store: s, Sections: []Section{sec}}}), s
}

func postLLM(h http.Handler, kv ...string) *httptest.ResponseRecorder {
	form := url.Values{}
	for i := 0; i < len(kv); i += 2 {
		form.Set(kv[i], kv[i+1])
	}
	req := httptest.NewRequest(http.MethodPost, "/values/llm", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func saved(t *testing.T, s *store.Store, name string) string {
	t.Helper()
	v, err := s.Value(context.Background(), llmField(name))
	if errors.Is(err, store.ErrUnset) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func seed(t *testing.T, s *store.Store, kv ...string) {
	t.Helper()
	for i := 0; i < len(kv); i += 2 {
		if err := s.SetValue(context.Background(), llmField(kv[i]), kv[i+1]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLLMSectionShowsEachTasksProviderAndModelTheTimeoutAndBothKeys(t *testing.T) {
	h, _ := llmHandler(t, &fakeLister{})
	body := page(h)
	for _, want := range []string{
		`data-testid="integration-llm"`,
		`data-testid="value-text_provider"`, `data-testid="value-text_model"`,
		`data-testid="value-vision_provider"`, `data-testid="value-vision_model"`,
		`data-testid="value-timeout"`,
		`data-testid="secret-llm-anthropic_key"`, `data-testid="secret-llm-gemini_key"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	// With no provider saved there is nothing to list, and nothing is said about it.
	if strings.Contains(body, "could not be listed") || strings.Contains(body, "to list its models") {
		t.Error("a problem is shown before any provider is chosen")
	}
}

// Choosing a provider saves it; the page then offers that provider's models for
// the task, and the model chosen from them is saved.
func TestChoosingAProviderThenAModel(t *testing.T) {
	h, s := llmHandler(t, &fakeLister{models: map[string][]llm.Model{llm.ProviderClaude: listing("m-one", "m-two")}})
	rec := postLLM(h, "text_provider", "claude", "text_model", "", "vision_provider", "", "vision_model", "", "timeout", "")
	if rec.Code != http.StatusOK || saved(t, s, "text_provider") != "claude" {
		t.Fatalf("provider not saved: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<option value="m-one"`) || !strings.Contains(body, `<option value="m-two"`) {
		t.Errorf("the saved provider's models are not offered: %s", body)
	}
	rec = postLLM(h, "text_provider", "claude", "text_model", "m-two", "vision_provider", "", "vision_model", "", "timeout", "")
	if got := saved(t, s, "text_model"); got != "m-two" {
		t.Fatalf("model = %q, want m-two; response: %s", got, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `<option value="m-two" selected`) {
		t.Errorf("the saved model is not selected: %s", rec.Body)
	}
}

// A model the provider does not list for the task is refused and nothing is
// saved; there is no free-text entry (ADR-00056).
func TestAModelNotInTheListingIsRefused(t *testing.T) {
	h, s := llmHandler(t, &fakeLister{models: map[string][]llm.Model{llm.ProviderClaude: listing("m-one")}})
	seed(t, s, "text_provider", "claude", "text_model", "m-one")
	rec := postLLM(h, "text_provider", "claude", "text_model", "typo-model", "vision_provider", "", "timeout", "30s")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Text model is not one Claude offers") {
		t.Fatalf("not refused: %d %s", rec.Code, rec.Body)
	}
	if got := saved(t, s, "text_model"); got != "m-one" {
		t.Errorf("model = %q, want m-one unchanged", got)
	}
	if got := saved(t, s, "timeout"); got != "" {
		t.Errorf("timeout = %q: a refused form must save nothing", got)
	}
}

// Changing a task's provider clears its model, so a model never outlives the
// provider that listed it.
func TestChangingTheProviderClearsTheModel(t *testing.T) {
	h, s := llmHandler(t, &fakeLister{models: map[string][]llm.Model{llm.ProviderClaude: listing("m-one"), llm.ProviderGemini: listing("g-one")}})
	seed(t, s, "text_provider", "claude", "text_model", "m-one")
	// The form still carries the old provider's model, as the browser sends it.
	postLLM(h, "text_provider", "gemini", "text_model", "m-one", "vision_provider", "", "timeout", "")
	if got := saved(t, s, "text_provider"); got != "gemini" {
		t.Fatalf("provider = %q", got)
	}
	if got := saved(t, s, "text_model"); got != "" {
		t.Errorf("model = %q, want cleared with the provider change", got)
	}
	// Clearing the provider turns the task off, model included.
	postLLM(h, "text_provider", "", "text_model", "g-one", "vision_provider", "", "timeout", "")
	if saved(t, s, "text_provider") != "" || saved(t, s, "text_model") != "" {
		t.Error("a task with no provider keeps a model")
	}
}

// ADR-00056 obligation 4: a listing that cannot be fetched is explained, no
// input is offered for the model, and the saved model stays as it is even when
// the rest of the form is saved.
func TestAFailedListingLeavesTheSavedModelUnchanged(t *testing.T) {
	lister := &fakeLister{models: map[string][]llm.Model{llm.ProviderClaude: listing("m-one")}}
	h, s := llmHandler(t, lister)
	seed(t, s, "text_provider", "claude", "text_model", "m-one")
	lister.err = errors.New("claude: list models: 503 overloaded")

	body := page(h)
	if !strings.Contains(body, "The models could not be listed: claude: list models: 503 overloaded") {
		t.Errorf("the failure is not explained: %s", body)
	}
	if !strings.Contains(body, `data-testid="saved-text_model">m-one<`) {
		t.Errorf("the saved model is not shown: %s", body)
	}
	if strings.Contains(body, `<select name="text_model"`) {
		t.Error("an input is offered for a model that cannot be checked")
	}
	// Saving the rest of the form posts no model; it must not clear it.
	rec := postLLM(h, "text_provider", "claude", "vision_provider", "", "timeout", "45s")
	if rec.Code != http.StatusOK || saved(t, s, "timeout") != "45s" {
		t.Fatalf("form not saved: %d %s", rec.Code, rec.Body)
	}
	if got := saved(t, s, "text_model"); got != "m-one" {
		t.Errorf("model = %q after a save during a failed listing, want m-one", got)
	}
}

// ADR-00056 obligation 4: a saved model the provider no longer lists is shown
// as such so it can be replaced, and stays selected until it is.
func TestASavedModelMissingFromTheListingIsMarked(t *testing.T) {
	h, s := llmHandler(t, &fakeLister{models: map[string][]llm.Model{llm.ProviderClaude: listing("m-new")}})
	seed(t, s, "text_provider", "claude", "text_model", "m-old")
	body := page(h)
	if !strings.Contains(body, `<option value="m-old" selected>m-old (no longer offered)</option>`) {
		t.Errorf("the missing model is not marked: %s", body)
	}
	if !strings.Contains(body, `<option value="m-new">`) {
		t.Errorf("the listed model is not offered: %s", body)
	}
	// Saving it unchanged is allowed, so an unrelated edit is not blocked.
	if rec := postLLM(h, "text_provider", "claude", "text_model", "m-old", "vision_provider", "", "timeout", "20s"); saved(t, s, "timeout") != "20s" {
		t.Errorf("an unrelated edit was blocked by the missing model: %s", rec.Body)
	}
	// Replacing it with a listed one works.
	postLLM(h, "text_provider", "claude", "text_model", "m-new", "vision_provider", "", "timeout", "20s")
	if got := saved(t, s, "text_model"); got != "m-new" {
		t.Errorf("model = %q, want m-new", got)
	}
}

func TestAProviderWithNoKeySavedSaysSoInsteadOfListing(t *testing.T) {
	h, s := llmHandler(t, &fakeLister{models: map[string][]llm.Model{}})
	seed(t, s, "text_provider", "gemini")
	body := page(h)
	if !strings.Contains(body, "Save the Gemini API key to list its models.") {
		t.Errorf("missing-key problem not shown: %s", body)
	}
}

// The timeout is a runtime setting, so the page enforces the bound the web
// server's write timeout is sized from.
func TestTheTimeoutIsBoundedAtSave(t *testing.T) {
	h, s := llmHandler(t, &fakeLister{})
	for _, bad := range []string{"3m", "soon", "0s", "-1s"} {
		rec := postLLM(h, "text_provider", "", "vision_provider", "", "timeout", bad)
		if !strings.Contains(rec.Body.String(), "Timeout must be a duration such as 60s, up to "+llm.MaxTimeout.String()) {
			t.Errorf("%q not refused: %s", bad, rec.Body)
		}
		if saved(t, s, "timeout") != "" {
			t.Errorf("%q was saved", bad)
		}
	}
	postLLM(h, "text_provider", "", "vision_provider", "", "timeout", llm.MaxTimeout.String())
	if got := saved(t, s, "timeout"); got != llm.MaxTimeout.String() {
		t.Errorf("the maximum was not accepted: %q", got)
	}
}

func TestAnUnknownProviderIsRefused(t *testing.T) {
	h, s := llmHandler(t, &fakeLister{})
	rec := postLLM(h, "text_provider", "gpt", "vision_provider", "", "timeout", "")
	if !strings.Contains(rec.Body.String(), "Text provider is not one of the providers offered") || saved(t, s, "text_provider") != "" {
		t.Errorf("not refused: %s", rec.Body)
	}
}

// ADR-00057 obligation 5: no response from the page contains a saved LLM API
// key; the page shows only whether each is set.
func TestNoResponseContainsASavedLLMKey(t *testing.T) {
	const anthropicKey, geminiKey = "sk-ant-Distinct-0123456789", "AIza-Distinct-9876543210"
	h, _ := llmHandler(t, &fakeLister{models: map[string][]llm.Model{llm.ProviderClaude: listing("m-one")}})
	for name, key := range map[string]string{"anthropic_key": anthropicKey, "gemini_key": geminiKey} {
		for _, htmx := range []bool{false, true} {
			if rec := post(h, "/secrets/llm/"+name, htmx, key); rec.Code >= 400 {
				t.Fatalf("save %s: %d %s", name, rec.Code, rec.Body)
			}
		}
	}
	postLLM(h, "text_provider", "claude", "vision_provider", "gemini", "timeout", "")
	responses := map[string]*httptest.ResponseRecorder{
		"page":     serve(h, http.MethodGet, "/", false),
		"fragment": postLLM(h, "text_provider", "claude", "text_model", "m-one", "vision_provider", "gemini", "timeout", "30s"),
		"refusal":  postLLM(h, "text_provider", "claude", "text_model", "not-listed", "vision_provider", "gemini", "timeout", ""),
	}
	for name, rec := range responses {
		for _, key := range []string{anthropicKey, geminiKey} {
			if strings.Contains(rec.Body.String(), key) || strings.Contains(rec.Header().Get("Location"), key) {
				t.Errorf("%s: the response holds a saved key:\n%s", name, rec.Body)
			}
		}
	}
	if body := responses["page"].Body.String(); strings.Count(body, ">Set<") != 2 {
		t.Errorf("both keys should read Set: %s", body)
	}
}

// The page over the real router and a recorded Models API listing: the
// narrowing of ADR-00056 reaches the select. The text task offers every active
// model with structured output; the vision task also needs image input.
func TestThePageOffersTheNarrowedListingPerTask(t *testing.T) {
	page1, err := os.ReadFile("../../llm/claude/testdata/models_page1.json")
	if err != nil {
		t.Fatal(err)
	}
	page2, err := os.ReadFile("../../llm/claude/testdata/models_page2.json")
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after_id") != "" {
			_, _ = w.Write(page2)
			return
		}
		_, _ = w.Write(page1)
	}))
	t.Cleanup(api.Close)

	_, s, _ := integrationsFixture(t, bytes.Repeat([]byte{1}, store.KeyBytes))
	rt := router.New(s, nil, router.WithClaudeOptions(claude.WithBaseURL(api.URL)))
	sec := LLMSection(LLMDeps{Values: s, Models: rt})
	h := Routes(Deps{Backup: &fakeBackup{}, Integrations: Integrations{Store: s, Sections: []Section{sec}}})
	if err := s.SetSecret(context.Background(), llmField(llm.FieldAnthropicKey), "key"); err != nil {
		t.Fatal(err)
	}
	seed(t, s, "text_provider", "claude", "vision_provider", "claude")

	body := page(h)
	text := between(t, body, `name="text_model"`, `</select>`)
	vision := between(t, body, `name="vision_model"`, `</select>`)
	for _, id := range []string{"fixture-large", "fixture-text-only", "fixture-small"} {
		if !strings.Contains(text, `value="`+id+`"`) {
			t.Errorf("text task lacks %s", id)
		}
	}
	for _, id := range []string{"fixture-old", "fixture-no-structure", "fixture-gone"} {
		if strings.Contains(text, id) || strings.Contains(vision, id) {
			t.Errorf("%s should be narrowed out of both lists", id)
		}
	}
	if strings.Contains(vision, "fixture-text-only") || !strings.Contains(vision, `value="fixture-large"`) {
		t.Errorf("vision task must drop the model without image input: %s", vision)
	}
}

func between(t *testing.T, s, from, to string) string {
	t.Helper()
	i := strings.Index(s, from)
	if i < 0 {
		t.Fatalf("no %q in page", from)
	}
	j := strings.Index(s[i:], to)
	if j < 0 {
		t.Fatalf("no %q after %q", to, from)
	}
	return s[i : i+j]
}

// barrierLister holds each call until both tasks' calls are in flight, so it
// can only finish if the page lists the two tasks at the same time. It counts
// the calls.
type barrierLister struct {
	mu      sync.Mutex
	calls   int
	arrived chan struct{}
	timeout time.Duration
}

func (b *barrierLister) Models(_ context.Context, _ string, _ llm.Task) ([]llm.Model, error) {
	b.mu.Lock()
	b.calls++
	if b.calls == len(llm.Tasks) {
		close(b.arrived)
	}
	b.mu.Unlock()
	select {
	case <-b.arrived:
		return listing("m-one"), nil
	case <-time.After(b.timeout):
		return nil, errors.New("the other task was not listed at the same time")
	}
}

// One render lists each task once, and the two listings run together, so the
// page waits for one provider call and not two in a row.
func TestRenderListsEachTaskOnceAndConcurrently(t *testing.T) {
	b := &barrierLister{arrived: make(chan struct{}), timeout: 3 * time.Second}
	h, s := llmHandler(t, b)
	seed(t, s, "text_provider", "claude", "vision_provider", "claude")
	body := page(h)
	if strings.Contains(body, "could not be listed") {
		t.Fatalf("the tasks were listed one after the other: %s", between(t, body, "could not be listed", "</span>"))
	}
	if b.calls != len(llm.Tasks) {
		t.Errorf("listing calls = %d, want one per task (%d)", b.calls, len(llm.Tasks))
	}
}
