// Package llmtest is the contract test suite every LLM provider passes
// (ADR-00007, obligation 2). A provider package calls RunContract from its own
// test with a factory that builds its real implementation over a fake backend;
// the suite drives that backend with recorded responses and failures and
// checks what ingest.Provider promises: a valid response becomes the expected
// ingest.Structured, and anything else (an invalid response, a timeout, a
// cancelled call, an unavailable backend) is an error and never a partial
// Structured (ADR-00006: an error is not a quietly low-scored draft).
//
// The suite never calls a live API and holds no model identifier.
//
// # What the suite checks
//
// Beyond the outcomes above, every call is checked for: a return within the
// configured timeout plus timeoutMargin (the timeout comes from configuration,
// ADR-00007); the caller's RuleDraft left deep-equal to what was passed in
// (the rule-based draft stays in place when a provider fails); and one log
// record carrying the keys LogProvider, LogModel and LogLatencyMS, plus
// LogInputTokens and LogOutputTokens when the call succeeds, with the
// sentinel API key in no log attribute, message or error text.
//
// # Recording a fixture
//
// A fixture is the text a model returned for a recipe, saved by hand once.
//
//  1. Pick a public-domain or self-written recipe. Do not use a page or a
//     photo from the owner's library.
//  2. Call the provider once from a scratch program or its SDK's console, with
//     the API key in the environment, and copy only the model's answer text
//     (the JSON object the tool call or structured output carried). Never copy
//     request headers, the SDK's raw HTTP dump, a response id tied to the
//     account, or the key.
//  3. Save it as testdata/valid/<name>.payload.json, and write the
//     ingest.Structured it must decode to as testdata/valid/<name>.want.json.
//     An answer that must be refused goes in testdata/invalid/ as
//     <name>.payload.json alone.
//  4. Check before committing: grep -ri -e key -e token -e bearer testdata
//     finds nothing, and the file holds no model identifier.
package llmtest

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"maps"
	"path"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/ingest"
)

//go:embed testdata
var fixtures embed.FS

// Behavior is what the fake backend does when the provider calls it.
type Behavior int

const (
	// Respond answers with Scenario.Payload.
	Respond Behavior = iota
	// Hang never answers; the call ends only when the caller's context does.
	Hang
	// Unavailable fails at once, as a refused connection or a 5xx would.
	Unavailable
)

// Scenario tells the fake backend how to behave for one provider call.
type Scenario struct {
	Behavior Behavior
	// Payload is the recorded text the model produced: the JSON body of an
	// ingest.Structured, or whatever invalid text the model produced instead.
	// The factory wraps it in its provider's own wire format (a Messages API
	// tool_use block, a Gemini candidate part) when it serves it. Only
	// meaningful for Respond.
	Payload []byte
	// Input and Output are the token counts the backend reports with its
	// answer (usage metadata in the provider's wire format). The provider must
	// log them as LogInputTokens and LogOutputTokens when the call succeeds. Set
	// for Respond.
	Input, Output int
}

// Env is everything the suite hands a factory for one provider call.
type Env struct {
	Scenario Scenario
	// Timeout is the call timeout the provider must apply (ADR-00007: every
	// call has one, from configuration).
	Timeout time.Duration
	// Logger is the logger the provider must log its calls to.
	Logger *slog.Logger
	// APIKey is a sentinel key the provider must be configured with, exactly
	// as it would read one from the environment. It must appear in no log
	// output and no error text. The backend may require it.
	APIKey string
}

// Log keys a provider writes on every call (ADR-00007). The model name logged
// is whatever the provider's configuration holds; the suite only requires it
// to be non-empty.
const (
	LogProvider     = "provider"
	LogModel        = "model"
	LogLatencyMS    = "latency_ms"
	LogInputTokens  = "input_tokens"
	LogOutputTokens = "output_tokens"
)

// SentinelAPIKey is the APIKey every Env carries.
const SentinelAPIKey = "sk-llmtest-sentinel-0000"

// Factory builds the provider under test over a fake backend that follows
// env.Scenario. Build the provider through its real constructor so the suite
// exercises the production path, and register any server clean-up with
// t.Cleanup.
type Factory func(t *testing.T, env Env) ingest.Provider

const (
	// callTimeout is the provider timeout the suite configures.
	callTimeout = 100 * time.Millisecond
	// timeoutMargin is how far past the configured timeout a call may return:
	// the time to unwind and build the error, not to keep waiting.
	timeoutMargin = 150 * time.Millisecond
	// returnBound is how long the suite waits for a call that should already
	// have ended; a provider that ignores its context is reported, not hung on.
	returnBound = 2 * time.Second
)

// RunContract runs the whole suite against providers built by newProvider.
func RunContract(t *testing.T, newProvider Factory) {
	t.Helper()
	t.Run("valid response", func(t *testing.T) { runValid(t, newProvider) })
	t.Run("invalid response", func(t *testing.T) { runInvalid(t, newProvider) })
	t.Run("timeout", func(t *testing.T) { runTimeout(t, newProvider) })
	t.Run("caller cancellation", func(t *testing.T) { runCancel(t, newProvider) })
	t.Run("backend unavailable", func(t *testing.T) { runUnavailable(t, newProvider) })
}

const (
	fixtureInputTokens  = 1200
	fixtureOutputTokens = 340
)

func runValid(t *testing.T, newProvider Factory) {
	names := fixtureNames(t, "valid")
	for _, name := range names {
		payload := readFixture(t, "valid", name+".payload.json")
		var want ingest.Structured
		if err := json.Unmarshal(readFixture(t, "valid", name+".want.json"), &want); err != nil {
			t.Fatalf("fixture %s want: %v", name, err)
		}
		for _, req := range requests() {
			t.Run(name+"/"+req.label, func(t *testing.T) {
				sc := Scenario{Behavior: Respond, Payload: payload, Input: fixtureInputTokens, Output: fixtureOutputTokens}
				got, err := invoke(context.Background(), t, newProvider, sc, callTimeout, req.req, 0)
				if err != nil {
					t.Fatalf("valid response was an error: %v", err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("structured recipe differs\n got: %+v\nwant: %+v", got, want)
				}
			})
		}
	}
}

func runInvalid(t *testing.T, newProvider Factory) {
	for _, name := range fixtureNames(t, "invalid") {
		payload := readFixture(t, "invalid", name+".payload.json")
		t.Run(name, func(t *testing.T) {
			sc := Scenario{Behavior: Respond, Payload: payload, Input: fixtureInputTokens, Output: fixtureOutputTokens}
			got, err := invoke(context.Background(), t, newProvider, sc, callTimeout, requests()[0].req, 0)
			if err == nil {
				t.Fatalf("invalid response was accepted as %+v", got)
			}
			requireNoPartial(t, got)
		})
	}
}

func runTimeout(t *testing.T, newProvider Factory) {
	got, err := invoke(context.Background(), t, newProvider, Scenario{Behavior: Hang}, callTimeout, requests()[0].req, callTimeout+timeoutMargin)
	if err == nil {
		t.Fatal("a call that never answered returned no error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("timeout must wrap context.DeadlineExceeded so callers can tell it from a failure, got: %v", err)
	}
	requireNoPartial(t, got)
}

func runCancel(t *testing.T, newProvider Factory) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(callTimeout, cancel)
	defer cancel()
	got, err := invoke(ctx, t, newProvider, Scenario{Behavior: Hang}, time.Minute, requests()[0].req, callTimeout+timeoutMargin)
	if err == nil {
		t.Fatal("a cancelled call returned no error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancellation must wrap context.Canceled, got: %v", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("cancellation reported as a timeout: %v", err)
	}
	requireNoPartial(t, got)
}

func runUnavailable(t *testing.T, newProvider Factory) {
	got, err := invoke(context.Background(), t, newProvider, Scenario{Behavior: Unavailable}, callTimeout, requests()[0].req, 0)
	if err == nil {
		t.Fatal("an unavailable backend returned no error")
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Errorf("a backend failure reported as a timeout or cancellation: %v", err)
	}
	if errors.Is(err, ingest.ErrFetch) {
		t.Errorf("a provider failure reported as a page fetch failure: %v", err)
	}
	requireNoPartial(t, got)
}

type labelledRequest struct {
	label string
	req   ingest.Request
}

// requests are the two input shapes a provider accepts: pasted text, and an
// image with no text (a photo for a vision model, ADR-00007). Each carries a
// fresh rule-based draft, which the provider must leave as it found it.
func requests() []labelledRequest {
	return []labelledRequest{
		{"text", ingest.Request{Text: "Tomato soup. 2 tbsp olive oil, 1 onion. Simmer.", RuleDraft: newRuleDraft()}},
		{"image", ingest.Request{Image: []byte{0x89, 'P', 'N', 'G'}, ImageMIME: "image/png", RuleDraft: newRuleDraft()}},
	}
}

// newRuleDraft builds the same rule-based draft on every call, with slices and
// a source snapshot a careless provider could modify in place.
func newRuleDraft() ingest.Draft {
	return ingest.Draft{
		ParserVersion: "1",
		Source:        ingest.Source{URL: "https://example.test/soup", Content: []byte("<html>soup</html>"), SHA256: "abc"},
		Name:          ingest.Field[string]{Value: "Tomato Soup", Confidence: 0.4},
		Components: []ingest.Component{{
			Lines: []ingest.Line{{Raw: "2 tbsp olive oil", Name: "olive oil", Confidence: 0.9, Issues: []string{"unit guessed"}}},
			Steps: []ingest.Step{{Text: "Simmer.", Confidence: 0.8}},
		}},
		Overall:        0.4,
		Threshold:      0.7,
		BelowThreshold: true,
		Issues:         []string{"no ingredient section"},
		Proposed:       []ingest.ProposedIngredient{{Name: "olive oil", Aisle: "oils"}},
	}
}

// invoke builds a provider for sc, calls it once and checks what every call
// owes regardless of outcome: it returns within maxElapsed when that is set,
// leaves the caller's rule-based draft unchanged, logs one record with the
// required keys, and leaks the API key into neither logs nor the error. The
// caller checks the outcome itself.
func invoke(ctx context.Context, t *testing.T, newProvider Factory, sc Scenario, timeout time.Duration, req ingest.Request, maxElapsed time.Duration) (ingest.Structured, error) {
	t.Helper()
	logs := newCapture()
	p := newProvider(t, Env{Scenario: sc, Timeout: timeout, Logger: slog.New(logs), APIKey: SentinelAPIKey})

	start := time.Now()
	got, err := call(ctx, t, p, req)
	elapsed := time.Since(start)

	if maxElapsed > 0 && elapsed > maxElapsed {
		t.Errorf("call took %s, over the %s allowed (timeout or cancellation delay, plus %s margin): the provider is not applying its configured timeout or honouring cancellation", elapsed, maxElapsed, timeoutMargin)
	}
	if want := newRuleDraft(); !reflect.DeepEqual(req.RuleDraft, want) {
		t.Errorf("the caller's rule-based draft was modified\n got: %+v\nwant: %+v", req.RuleDraft, want)
	}
	if err != nil && strings.Contains(err.Error(), SentinelAPIKey) {
		t.Errorf("the API key appears in the error text: %v", err)
	}
	checkLogs(t, logs.records(), sc, err)
	return got, err
}

// checkLogs asserts the per-call log record (ADR-00007: provider, model,
// latency and token counts; never the API key).
func checkLogs(t *testing.T, recs []logRecord, sc Scenario, callErr error) {
	t.Helper()
	for _, r := range recs {
		if strings.Contains(r.msg, SentinelAPIKey) {
			t.Errorf("the API key appears in a log message: %q", r.msg)
		}
		for k, v := range r.attrs {
			if strings.Contains(k, SentinelAPIKey) || strings.Contains(v.String(), SentinelAPIKey) {
				t.Errorf("the API key appears in log attribute %q", k)
			}
		}
	}
	var rec *logRecord
	for i := range recs {
		if _, ok := recs[i].attrs[LogLatencyMS]; ok {
			rec = &recs[i]
		}
	}
	if rec == nil {
		t.Errorf("no log record carries %q: every call must log provider, model, latency and tokens", LogLatencyMS)
		return
	}
	for _, key := range []string{LogProvider, LogModel} {
		if v, ok := rec.attrs[key]; !ok || strings.TrimSpace(v.String()) == "" {
			t.Errorf("call log record has no non-empty %q", key)
		}
	}
	if sc.Behavior != Respond || callErr != nil {
		return
	}
	for key, want := range map[string]int{LogInputTokens: sc.Input, LogOutputTokens: sc.Output} {
		v, ok := rec.attrs[key]
		if !ok {
			t.Errorf("call log record has no %q", key)
			continue
		}
		if v.Kind() != slog.KindInt64 && v.Kind() != slog.KindUint64 || v.String() != strconv.Itoa(want) {
			t.Errorf("logged %s = %s, backend reported %d", key, v.String(), want)
		}
	}
}

// logRecord is a captured slog record with its attributes flattened; group
// names prefix keys with a dot.
type logRecord struct {
	msg   string
	attrs map[string]slog.Value
}

// capture is a slog.Handler that keeps every record at every level. Handlers
// derived with WithAttrs or WithGroup share the sink of the one they came from.
type capture struct {
	sink   *sink
	prefix string
	// fixed holds attributes added with WithAttrs, already keyed with the
	// group prefix in force when they were added.
	fixed map[string]slog.Value
}

type sink struct {
	mu   sync.Mutex
	recs []logRecord
}

func newCapture() *capture { return &capture{sink: &sink{}} }

func (c *capture) records() []logRecord {
	c.sink.mu.Lock()
	defer c.sink.mu.Unlock()
	return append([]logRecord(nil), c.sink.recs...)
}

func (c *capture) Enabled(context.Context, slog.Level) bool { return true }

func (c *capture) Handle(_ context.Context, r slog.Record) error {
	rec := logRecord{msg: r.Message, attrs: map[string]slog.Value{}}
	maps.Copy(rec.attrs, c.fixed)
	r.Attrs(func(a slog.Attr) bool {
		flatten(rec.attrs, c.prefix, a)
		return true
	})
	c.sink.mu.Lock()
	defer c.sink.mu.Unlock()
	c.sink.recs = append(c.sink.recs, rec)
	return nil
}

func (c *capture) WithAttrs(attrs []slog.Attr) slog.Handler {
	fixed := maps.Clone(c.fixed)
	if fixed == nil {
		fixed = map[string]slog.Value{}
	}
	for _, a := range attrs {
		flatten(fixed, c.prefix, a)
	}
	return &capture{sink: c.sink, prefix: c.prefix, fixed: fixed}
}

func (c *capture) WithGroup(name string) slog.Handler {
	return &capture{sink: c.sink, prefix: c.prefix + name + ".", fixed: c.fixed}
}

func flatten(into map[string]slog.Value, prefix string, a slog.Attr) {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		for _, g := range v.Group() {
			flatten(into, prefix+a.Key+".", g)
		}
		return
	}
	into[prefix+a.Key] = v
}

// call runs p.Structure and fails the test, rather than hanging it, when the
// provider does not return within returnBound.
func call(ctx context.Context, t *testing.T, p ingest.Provider, req ingest.Request) (ingest.Structured, error) {
	t.Helper()
	type result struct {
		s   ingest.Structured
		err error
	}
	done := make(chan result, 1)
	go func() {
		s, err := p.Structure(ctx, req)
		done <- result{s, err}
	}()
	select {
	case r := <-done:
		return r.s, r.err
	case <-time.After(returnBound):
		t.Fatalf("provider did not return within %s: it ignores its context or its timeout", returnBound)
		return ingest.Structured{}, nil
	}
}

// requireNoPartial fails when an errored call still returned recipe content:
// the caller would have to guess whether to trust it.
func requireNoPartial(t *testing.T, got ingest.Structured) {
	t.Helper()
	if !reflect.DeepEqual(got, ingest.Structured{}) {
		t.Errorf("an errored call returned a partial Structured: %+v", got)
	}
}

func fixtureNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := fs.ReadDir(fixtures, path.Join("testdata", dir))
	if err != nil {
		t.Fatalf("read fixtures %s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".payload.json"); ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		t.Fatalf("no %s fixtures found", dir)
	}
	return names
}

func readFixture(t *testing.T, dir, file string) []byte {
	t.Helper()
	b, err := fixtures.ReadFile(path.Join("testdata", dir, file))
	if err != nil {
		t.Fatalf("read fixture %s/%s: %v", dir, file, err)
	}
	return b
}
