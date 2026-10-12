package llmtest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/ingest"
)

// mutant is one way a provider can break the contract. The zero value is a
// correct provider.
type mutant string

const (
	correct             mutant = ""
	acceptsAnything     mutant = "accepts-anything"     // skips schema validation
	swallowsInvalid     mutant = "swallows-invalid"     // invalid response becomes an empty draft, no error
	partialWithError    mutant = "partial-with-error"   // errors but still returns the half-decoded recipe
	ignoresTimeout      mutant = "ignores-timeout"      // blocks past its timeout and the caller's context
	lateTimeout         mutant = "late-timeout"         // returns after the timeout, well under the suite bound
	timeoutNotWrapped   mutant = "timeout-not-wrapped"  // timeout is an opaque error
	cancelAsTimeout     mutant = "cancel-as-timeout"    // cancellation reported as a timeout
	unavailableNoError  mutant = "unavailable-no-error" // backend failure returns an empty draft
	unavailableAsFetch  mutant = "unavailable-as-fetch" // backend failure mapped to ErrFetch
	unavailableTimeout  mutant = "unavailable-timeout"  // backend failure mapped to a timeout
	dropsComponentSteps mutant = "drops-steps"          // loses data from a valid response
	mutatesRuleDraft    mutant = "mutates-rule-draft"   // edits the caller's draft in place
	mutatesSnapshot     mutant = "mutates-snapshot"     // edits the caller's source snapshot
	logsKey             mutant = "logs-key"             // writes the API key to a log attribute
	errorLeaksKey       mutant = "error-leaks-key"      // puts the API key in an error
	omitsLatency        mutant = "omits-latency"        // no latency in the call log
	omitsModel          mutant = "omits-model"          // empty model in the call log
	omitsTokens         mutant = "omits-tokens"         // no token counts in the call log
	noLog               mutant = "no-log"               // logs nothing
)

// fakeProvider is a reference provider over an Env, with optional faults.
type fakeProvider struct {
	env   Env
	fault mutant
}

func (f fakeProvider) Structure(ctx context.Context, req ingest.Request) (ingest.Structured, error) {
	start := time.Now()
	if f.fault == mutatesRuleDraft {
		req.RuleDraft.Issues[0] = "changed by provider"
	}
	if f.fault == mutatesSnapshot {
		req.RuleDraft.Source.Content[0] = 'X'
	}
	ctx, cancel := context.WithTimeout(ctx, f.env.Timeout)
	defer cancel()
	var (
		s   ingest.Structured
		err error
	)
	switch f.env.Scenario.Behavior {
	case Hang:
		s, err = f.hang(ctx)
	case Unavailable:
		s, err = f.unavailable()
	default:
		s, err = f.respond()
	}
	f.log(start, err)
	if err != nil && f.fault == errorLeaksKey {
		err = fmt.Errorf("%w (key %s)", err, f.env.APIKey)
	}
	return s, err
}

func (f fakeProvider) log(start time.Time, err error) {
	if f.fault == noLog {
		return
	}
	attrs := []any{LogProvider, "fake", LogModel, "configured-model"}
	if f.fault == omitsModel {
		attrs[3] = ""
	}
	if f.fault != omitsLatency {
		attrs = append(attrs, LogLatencyMS, time.Since(start).Milliseconds())
	}
	if err == nil && f.fault != omitsTokens {
		attrs = append(attrs, LogInputTokens, f.env.Scenario.Input, LogOutputTokens, f.env.Scenario.Output)
	}
	if f.fault == logsKey {
		attrs = append(attrs, "api_key", f.env.APIKey)
	}
	f.env.Logger.Info("llm call", attrs...)
}

func (f fakeProvider) unavailable() (ingest.Structured, error) {
	switch f.fault {
	case unavailableNoError:
		return ingest.Structured{}, nil
	case unavailableAsFetch:
		return ingest.Structured{}, fmt.Errorf("%w: backend down", ingest.ErrFetch)
	case unavailableTimeout:
		return ingest.Structured{}, fmt.Errorf("backend down: %w", context.DeadlineExceeded)
	}
	return ingest.Structured{}, errors.New("backend unavailable: 503")
}

func (f fakeProvider) hang(ctx context.Context) (ingest.Structured, error) {
	if f.fault == ignoresTimeout {
		time.Sleep(10 * returnBound)
	}
	<-ctx.Done()
	if f.fault == lateTimeout {
		time.Sleep(4 * timeoutMargin)
	}
	err := ctx.Err()
	switch f.fault {
	case timeoutNotWrapped:
		return ingest.Structured{}, errors.New("call took too long")
	case cancelAsTimeout:
		return ingest.Structured{}, fmt.Errorf("structure: %w", context.DeadlineExceeded)
	}
	return ingest.Structured{}, fmt.Errorf("structure: %w", err)
}

func (f fakeProvider) respond() (ingest.Structured, error) {
	payload := f.env.Scenario.Payload
	if f.fault == acceptsAnything {
		var s ingest.Structured
		_ = json.Unmarshal(payload, &s)
		return s, nil
	}
	s, err := decodeStrict(payload)
	if err != nil {
		switch f.fault {
		case swallowsInvalid:
			return ingest.Structured{}, nil
		case partialWithError:
			var partial ingest.Structured
			_ = json.Unmarshal(payload, &partial)
			return partial, err
		}
		return ingest.Structured{}, err
	}
	if f.fault == dropsComponentSteps {
		for i := range s.Components {
			s.Components[i].Steps = nil
		}
	}
	return s, nil
}

// decodeStrict is the reference schema check: one JSON object, a string name,
// and at least one component.
func decodeStrict(payload []byte) (ingest.Structured, error) {
	dec := json.NewDecoder(bytes.NewReader(payload))
	var s ingest.Structured
	if err := dec.Decode(&s); err != nil {
		return ingest.Structured{}, fmt.Errorf("response is not a recipe object: %w", err)
	}
	if dec.More() {
		return ingest.Structured{}, errors.New("response has content after the recipe object")
	}
	if strings.TrimSpace(s.Name) == "" {
		return ingest.Structured{}, errors.New("recipe has no name")
	}
	if len(s.Components) == 0 {
		return ingest.Structured{}, errors.New("recipe has no components")
	}
	return s, nil
}

func factoryFor(fault mutant) Factory {
	return func(_ *testing.T, env Env) ingest.Provider {
		return fakeProvider{env: env, fault: fault}
	}
}

// TestRunContractAcceptsCorrectProvider shows the suite is usable by a provider
// package and passes a provider that keeps the contract.
func TestRunContractAcceptsCorrectProvider(t *testing.T) {
	RunContract(t, factoryFor(correct))
}

// TestContractMutant is the body the subprocess in TestSuiteCatchesBrokenProviders
// runs; on its own it is skipped.
func TestContractMutant(t *testing.T) {
	m, ok := os.LookupEnv("LLMTEST_MUTANT")
	if !ok {
		t.Skip("run by TestSuiteCatchesBrokenProviders")
	}
	RunContract(t, factoryFor(mutant(m)))
}

// TestSuiteCatchesBrokenProviders runs the suite against each broken provider
// in a child test process and requires the named part of the suite to fail.
func TestSuiteCatchesBrokenProviders(t *testing.T) {
	cases := []struct {
		fault    mutant
		failsIn  string
		failsMsg string
	}{
		{acceptsAnything, "--- FAIL: TestContractMutant/invalid_response/", "was accepted"},
		{swallowsInvalid, "--- FAIL: TestContractMutant/invalid_response/", "was accepted"},
		{partialWithError, "--- FAIL: TestContractMutant/invalid_response/", "partial Structured"},
		{ignoresTimeout, "--- FAIL: TestContractMutant/timeout", "did not return within"},
		{timeoutNotWrapped, "--- FAIL: TestContractMutant/timeout", "DeadlineExceeded"},
		{cancelAsTimeout, "--- FAIL: TestContractMutant/caller_cancellation", "context.Canceled"},
		{unavailableNoError, "--- FAIL: TestContractMutant/backend_unavailable", "no error"},
		{unavailableAsFetch, "--- FAIL: TestContractMutant/backend_unavailable", "page fetch"},
		{unavailableTimeout, "--- FAIL: TestContractMutant/backend_unavailable", "timeout or cancellation"},
		{dropsComponentSteps, "--- FAIL: TestContractMutant/valid_response/", "differs"},
		{lateTimeout, "--- FAIL: TestContractMutant/timeout", "not applying its configured timeout"},
		{mutatesRuleDraft, "--- FAIL: TestContractMutant/valid_response/", "rule-based draft was modified"},
		{mutatesRuleDraft, "--- FAIL: TestContractMutant/backend_unavailable", "rule-based draft was modified"},
		{mutatesSnapshot, "--- FAIL: TestContractMutant/timeout", "rule-based draft was modified"},
		{logsKey, "--- FAIL: TestContractMutant/valid_response/", "API key appears in log attribute"},
		{errorLeaksKey, "--- FAIL: TestContractMutant/backend_unavailable", "API key appears in the error"},
		{omitsLatency, "--- FAIL: TestContractMutant/valid_response/", "no log record carries"},
		{omitsModel, "--- FAIL: TestContractMutant/valid_response/", `no non-empty "model"`},
		{omitsTokens, "--- FAIL: TestContractMutant/valid_response/", `no "input_tokens"`},
		{noLog, "--- FAIL: TestContractMutant/backend_unavailable", "no log record carries"},
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(string(c.fault)+"/"+strings.TrimPrefix(c.failsIn, "--- FAIL: TestContractMutant/"), func(t *testing.T) {
			cmd := exec.Command(exe, "-test.run=^TestContractMutant$", "-test.v")
			cmd.Env = append(os.Environ(), "LLMTEST_MUTANT="+string(c.fault))
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("suite passed a provider with fault %q\n%s", c.fault, out)
			}
			if !strings.Contains(string(out), c.failsIn) {
				t.Errorf("fault %q did not fail %q\n%s", c.fault, c.failsIn, out)
			}
			if !strings.Contains(string(out), c.failsMsg) {
				t.Errorf("fault %q did not report %q\n%s", c.fault, c.failsMsg, out)
			}
		})
	}
}

func TestFixturesPairUp(t *testing.T) {
	for _, name := range fixtureNames(t, "valid") {
		readFixture(t, "valid", name+".want.json")
	}
}

// TestCaptureGroupsFollowSlogSemantics: only attributes added after WithGroup
// take the group prefix.
func TestCaptureGroupsFollowSlogSemantics(t *testing.T) {
	c := newCapture()
	l := slog.New(c).With("before", 1).WithGroup("g").With("after", 2)
	l.Info("m", "own", 3)
	recs := c.records()
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	for _, key := range []string{"before", "g.after", "g.own"} {
		if _, ok := recs[0].attrs[key]; !ok {
			t.Errorf("missing key %q in %v", key, recs[0].attrs)
		}
	}
	if _, ok := recs[0].attrs["g.before"]; ok {
		t.Error("attribute added before WithGroup was prefixed")
	}
}
