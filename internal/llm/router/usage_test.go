package router_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/llm"
	"github.com/kaecyra/gobbler/internal/llm/claude"
	"github.com/kaecyra/gobbler/internal/llm/gemini"
	"github.com/kaecyra/gobbler/internal/llm/router"
	"github.com/kaecyra/gobbler/internal/llmusage"
	"github.com/kaecyra/gobbler/internal/settings"
)

// usageEnv is a router that records its calls in a real llmusage store.
type usageEnv struct {
	store *settings.Store
	db    *sql.DB
	usage *llmusage.Store
	rt    *router.Router
	log   *bytes.Buffer
}

func newUsageEnv(t *testing.T, claudeURL, geminiURL string, rec func(*llmusage.Store) router.Recorder) *usageEnv {
	t.Helper()
	s, d := openStore(t, testKey)
	e := &usageEnv{store: s, db: d, usage: llmusage.New(d, nil), log: &bytes.Buffer{}}
	e.rt = router.New(s, slog.New(slog.NewJSONHandler(e.log, nil)),
		router.WithClaudeOptions(claude.WithBaseURL(claudeURL)),
		router.WithGeminiOptions(gemini.WithBaseURL(geminiURL)),
		router.WithRecorder(rec(e.usage)))
	setSecret(t, s, llm.FieldAnthropicKey, claudeKey)
	setSecret(t, s, llm.FieldGeminiKey, geminiKey)
	setValue(t, s, llm.FieldTextProvider, llm.ProviderClaude)
	setValue(t, s, llm.FieldTextModel, claudeModel)
	setValue(t, s, llm.FieldVisionProvider, llm.ProviderGemini)
	setValue(t, s, llm.FieldVisionModel, geminiModel)
	return e
}

func realRecorder(s *llmusage.Store) router.Recorder { return s }

type calls struct {
	task, provider, model, outcome, errText string
	in, out                                 int64
	household                               sql.NullInt64
	cost                                    sql.NullInt64
}

func (e *usageEnv) calls(t *testing.T) []calls {
	t.Helper()
	rows, err := e.db.Query(`SELECT task, provider, model, outcome, COALESCE(error, ''), input_tokens, output_tokens, household_id, cost_micros FROM llm_calls ORDER BY id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []calls
	for rows.Next() {
		var c calls
		if err := rows.Scan(&c.task, &c.provider, &c.model, &c.outcome, &c.errText, &c.in, &c.out, &c.household, &c.cost); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, c)
	}
	return out
}

// ADR-00058 obligation 2: a successful call and a failed call each produce a
// record, the failed one with its error; the record carries the task, provider,
// model, tokens and the household the call ran for, and a cost from the price
// in force.
func TestEveryCallIsRecorded(t *testing.T) {
	claudeOK, geminiOK := fakeClaude(t), fakeGemini(t)
	e := newUsageEnv(t, claudeOK.srv.URL, geminiOK.srv.URL, realRecorder)
	ctx := household.WithContext(context.Background(), household.Household{ID: 1})
	if _, err := e.db.Exec(`INSERT INTO households (id, name, created_at) VALUES (1, 'Fixture', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := e.usage.SetPrice(ctx, llm.ProviderClaude, claudeModel, "1", "2"); err != nil {
		t.Fatal(err)
	}

	if _, err := e.rt.Structure(ctx, ingest.Request{Text: "Toast"}); err != nil {
		t.Fatalf("Structure: %v", err)
	}
	got := e.calls(t)
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1: %+v", len(got), got)
	}
	want := calls{task: "text", provider: "claude", model: claudeModel, outcome: "ok", in: 1, out: 1}
	c := got[0]
	if c.task != want.task || c.provider != want.provider || c.model != want.model || c.outcome != "ok" || c.in != 1 || c.out != 1 {
		t.Errorf("record = %+v, want %+v", c, want)
	}
	if !c.household.Valid || c.household.Int64 != 1 {
		t.Errorf("household = %v, want 1 from the context", c.household)
	}
	// One token at $1 per million is 1 millionth of a dollar, and one at $2 is 2.
	if !c.cost.Valid || c.cost.Int64 != 3 {
		t.Errorf("cost = %v, want 3 millionths of a dollar", c.cost)
	}

	// A vision call goes to Gemini, which has no price: unpriced, not zero.
	if _, err := e.rt.Transcribe(context.Background(), ingest.TranscribeRequest{Image: photo.Image, ImageMIME: "image/png"}); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	got = e.calls(t)
	if len(got) != 2 || got[1].task != "vision" || got[1].provider != "gemini" || got[1].cost.Valid || got[1].household.Valid || got[1].in != 1 || got[1].out != 1 {
		t.Errorf("transcribe record = %+v, want vision, gemini, 1 and 1 tokens, unpriced and no household", got)
	}
}

func TestAFailedCallIsRecordedWithItsErrorAndNoKey(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		// A provider that echoes the key it was sent.
		_, _ = fmt.Fprintf(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad key %s"}}`, r.Header.Get("X-Api-Key"))
	}))
	defer failing.Close()
	e := newUsageEnv(t, failing.URL, fakeGemini(t).srv.URL, realRecorder)

	_, err := e.rt.Structure(context.Background(), ingest.Request{Text: "Toast"})
	if err == nil {
		t.Fatal("Structure succeeded against a failing provider")
	}
	got := e.calls(t)
	if len(got) != 1 || got[0].outcome != "error" || got[0].in != 0 || got[0].out != 0 {
		t.Fatalf("records = %+v, want one failed record with no tokens", got)
	}
	if !strings.Contains(got[0].errText, "bad key") {
		t.Errorf("recorded error = %q, want the provider's message", got[0].errText)
	}
	if strings.Contains(got[0].errText, claudeKey) {
		t.Errorf("recorded error carries the API key: %q", got[0].errText)
	}
	if !strings.Contains(got[0].errText, "[REDACTED]") {
		t.Errorf("recorded error = %q, want the key replaced", got[0].errText)
	}
}

func TestACallThatWasNeverMadeIsNotRecorded(t *testing.T) {
	e := newUsageEnv(t, fakeClaude(t).srv.URL, fakeGemini(t).srv.URL, realRecorder)
	if err := e.store.Clear(context.Background(), field(llm.FieldTextProvider)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.rt.Structure(context.Background(), ingest.Request{Text: "x"}); !errors.Is(err, ingest.ErrNoProvider) {
		t.Fatalf("err = %v, want ErrNoProvider", err)
	}
	if got := e.calls(t); len(got) != 0 {
		t.Errorf("records = %+v, want none", got)
	}
}

type failingRecorder struct{ n int }

func (f *failingRecorder) Record(context.Context, llmusage.Call) error {
	f.n++
	return errors.New("disk is full")
}

// ADR-00058 obligation 2, second half: when recording fails the failure is
// logged and the call, and so the ingest, is not failed by it (ADR-00007).
func TestARecordThatFailsIsLoggedAndTheCallStands(t *testing.T) {
	rec := &failingRecorder{}
	e := newUsageEnv(t, fakeClaude(t).srv.URL, fakeGemini(t).srv.URL, func(*llmusage.Store) router.Recorder { return rec })

	got, err := e.rt.Structure(context.Background(), ingest.Request{Text: "Toast"})
	if err != nil {
		t.Fatalf("Structure failed because recording did: %v", err)
	}
	if got.Name != "Toast" {
		t.Errorf("Structure returned %+v, want the provider's recipe", got)
	}
	if text, err := e.rt.Transcribe(context.Background(), ingest.TranscribeRequest{Image: photo.Image, ImageMIME: "image/png"}); err != nil || text == "" {
		t.Errorf("Transcribe = %q, %v", text, err)
	}
	if rec.n != 2 {
		t.Errorf("recorder called %d times, want 2", rec.n)
	}
	if !strings.Contains(e.log.String(), "disk is full") || !strings.Contains(e.log.String(), "not recorded") {
		t.Errorf("the failure was not logged: %s", e.log.String())
	}
}

// A call whose context is cancelled while it runs still leaves its record.
func TestACancelledCallIsStillRecorded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		cancel()
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer slow.Close()
	e := newUsageEnv(t, slow.URL, fakeGemini(t).srv.URL, realRecorder)

	if _, err := e.rt.Structure(ctx, ingest.Request{Text: "Toast"}); err == nil {
		t.Fatal("Structure succeeded after its context was cancelled")
	}
	got := e.calls(t)
	if len(got) != 1 || got[0].outcome != "error" {
		t.Errorf("records = %+v, want one failed record", got)
	}
}

// A response that came back and then failed to decode was still billed: the
// record carries the tokens the provider reported and a cost from the price
// (ADR-00058). Each case is one way a billed call fails.
func TestABilledCallThatFailsIsRecordedWithItsTokensAndCost(t *testing.T) {
	cutOff := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"msg_x","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"partial"}],"stop_reason":"max_tokens","stop_sequence":null,"usage":{"input_tokens":7,"output_tokens":9}}`)
	}))
	defer cutOff.Close()
	notJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"this is not json"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":13}}`)
	}))
	defer notJSON.Close()

	for name, c := range map[string]struct {
		claudeURL, geminiURL string
		call                 func(*router.Router) error
		provider, model      string
		in, out, cost        int64
	}{
		"claude cut off at the token limit": {
			claudeURL: cutOff.URL, geminiURL: fakeGemini(t).srv.URL, provider: llm.ProviderClaude, model: claudeModel, in: 7, out: 9, cost: 25,
			call: func(r *router.Router) error {
				_, err := r.Structure(context.Background(), ingest.Request{Text: "Toast"})
				return err
			},
		},
		"claude transcription cut off": {
			claudeURL: cutOff.URL, geminiURL: fakeGemini(t).srv.URL, provider: llm.ProviderClaude, model: claudeModel, in: 7, out: 9, cost: 25,
			call: func(r *router.Router) error {
				_, err := r.Transcribe(context.Background(), ingest.TranscribeRequest{Image: photo.Image, ImageMIME: "image/png"})
				return err
			},
		},
		"gemini answer that does not decode": {
			claudeURL: fakeClaude(t).srv.URL, geminiURL: notJSON.URL, provider: llm.ProviderGemini, model: geminiModel, in: 11, out: 13, cost: 37,
			call: func(r *router.Router) error { _, err := r.Structure(context.Background(), photo); return err },
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newUsageEnv(t, c.claudeURL, c.geminiURL, realRecorder)
			if name == "claude transcription cut off" {
				// Transcribe is the vision task, which the fixture puts on Gemini.
				setValue(t, e.store, llm.FieldVisionProvider, llm.ProviderClaude)
				setValue(t, e.store, llm.FieldVisionModel, claudeModel)
			}
			if err := e.usage.SetPrice(context.Background(), c.provider, c.model, "1", "2"); err != nil {
				t.Fatal(err)
			}
			if err := c.call(e.rt); err == nil {
				t.Fatal("the call succeeded")
			}
			got := e.calls(t)
			if len(got) != 1 || got[0].outcome != "error" || got[0].in != c.in || got[0].out != c.out {
				t.Fatalf("records = %+v, want one error record with %d and %d tokens", got, c.in, c.out)
			}
			if !got[0].cost.Valid || got[0].cost.Int64 != c.cost {
				t.Errorf("cost = %v, want %d millionths of a dollar", got[0].cost, c.cost)
			}
		})
	}
}
