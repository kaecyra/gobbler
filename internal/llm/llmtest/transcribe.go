package llmtest

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/ingest"
)

// RunTranscribeContract runs the suite for ingest.Provider.Transcribe
// (ADR-00053) against providers built by newProvider, over a fake backend that
// follows env.Scenario. Scenario.Payload is the text the model answered, which
// is not JSON here. The suite checks that:
//
//   - a text answer comes back as the transcription, trimmed and otherwise
//     unchanged;
//   - a blank answer, a timeout, a cancellation and an unavailable backend are
//     errors, and the first two return no text; a timeout wraps
//     context.DeadlineExceeded and a cancellation context.Canceled, within the
//     configured timeout plus timeoutMargin;
//   - every call logs the keys RunContract requires, and the sentinel API key
//     is in no log output and no error text.
func RunTranscribeContract(t *testing.T, newProvider Factory) {
	t.Helper()
	t.Run("transcribe valid response", func(t *testing.T) {
		payload := readFixture(t, "transcribe", "page.txt")
		sc := Scenario{Behavior: Respond, Payload: payload, Input: fixtureInputTokens, Output: fixtureOutputTokens}
		got, err := invokeTranscribe(context.Background(), t, newProvider, sc, callTimeout, 0)
		if err != nil {
			t.Fatalf("valid transcription was an error: %v", err)
		}
		if want := strings.TrimSpace(string(payload)); got != want {
			t.Errorf("transcription differs\n got: %q\nwant: %q", got, want)
		}
	})
	t.Run("transcribe blank response", func(t *testing.T) {
		sc := Scenario{Behavior: Respond, Payload: []byte("  \n "), Input: 1, Output: 1}
		got, err := invokeTranscribe(context.Background(), t, newProvider, sc, callTimeout, 0)
		if err == nil || got != "" {
			t.Errorf("a blank answer gave %q and error %v, want an error and no text", got, err)
		}
	})
	t.Run("transcribe timeout", func(t *testing.T) {
		got, err := invokeTranscribe(context.Background(), t, newProvider, Scenario{Behavior: Hang}, callTimeout, callTimeout+timeoutMargin)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("a transcription that never answered must wrap context.DeadlineExceeded, got: %v", err)
		}
		if got != "" {
			t.Errorf("an errored transcription returned text %q", got)
		}
	})
	t.Run("transcribe caller cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(callTimeout, cancel)
		defer cancel()
		_, err := invokeTranscribe(ctx, t, newProvider, Scenario{Behavior: Hang}, time.Minute, callTimeout+timeoutMargin)
		if !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("cancellation must wrap context.Canceled and not report a timeout, got: %v", err)
		}
	})
	t.Run("transcribe backend unavailable", func(t *testing.T) {
		got, err := invokeTranscribe(context.Background(), t, newProvider, Scenario{Behavior: Unavailable}, callTimeout, 0)
		if err == nil {
			t.Fatal("an unavailable backend returned no error")
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			t.Errorf("a backend failure reported as a timeout or cancellation: %v", err)
		}
		if got != "" {
			t.Errorf("an errored transcription returned text %q", got)
		}
	})
}

// invokeTranscribe is invoke for Transcribe: one call, with the bounds and the
// log and key checks every call owes.
func invokeTranscribe(ctx context.Context, t *testing.T, newProvider Factory, sc Scenario, timeout, maxElapsed time.Duration) (string, error) {
	t.Helper()
	logs := newCapture()
	p := newProvider(t, Env{Scenario: sc, Timeout: timeout, Logger: slog.New(logs), APIKey: SentinelAPIKey})

	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		text, err := p.Transcribe(ctx, ingest.TranscribeRequest{Image: []byte{0x89, 'P', 'N', 'G'}, ImageMIME: "image/png"})
		done <- result{text, err}
	}()
	var r result
	select {
	case r = <-done:
	case <-time.After(returnBound):
		t.Fatalf("provider did not return within %s: it ignores its context or its timeout", returnBound)
	}
	if elapsed := time.Since(start); maxElapsed > 0 && elapsed > maxElapsed {
		t.Errorf("call took %s, over the %s allowed: the provider is not applying its configured timeout or honouring cancellation", elapsed, maxElapsed)
	}
	if r.err != nil && strings.Contains(r.err.Error(), SentinelAPIKey) {
		t.Errorf("the API key appears in the error text: %v", r.err)
	}
	checkLogs(t, logs.records(), sc, r.err)
	return r.text, r.err
}
