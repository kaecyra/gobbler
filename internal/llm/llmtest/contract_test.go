package llmtest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	timeoutNotWrapped   mutant = "timeout-not-wrapped"  // timeout is an opaque error
	cancelAsTimeout     mutant = "cancel-as-timeout"    // cancellation reported as a timeout
	unavailableNoError  mutant = "unavailable-no-error" // backend failure returns an empty draft
	unavailableAsFetch  mutant = "unavailable-as-fetch" // backend failure mapped to ErrFetch
	unavailableTimeout  mutant = "unavailable-timeout"  // backend failure mapped to a timeout
	dropsComponentSteps mutant = "drops-steps"          // loses data from a valid response
)

// fakeProvider is a reference provider over a Scenario, with optional faults.
type fakeProvider struct {
	s       Scenario
	timeout time.Duration
	fault   mutant
}

func (f fakeProvider) Structure(ctx context.Context, _ ingest.Request) (ingest.Structured, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	switch f.s.Behavior {
	case Hang:
		return f.hang(ctx)
	case Unavailable:
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
	return f.respond()
}

func (f fakeProvider) hang(ctx context.Context) (ingest.Structured, error) {
	if f.fault == ignoresTimeout {
		time.Sleep(10 * returnBound)
	}
	<-ctx.Done()
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
	if f.fault == acceptsAnything {
		var s ingest.Structured
		_ = json.Unmarshal(f.s.Payload, &s)
		return s, nil
	}
	s, err := decodeStrict(f.s.Payload)
	if err != nil {
		switch f.fault {
		case swallowsInvalid:
			return ingest.Structured{}, nil
		case partialWithError:
			var partial ingest.Structured
			_ = json.Unmarshal(f.s.Payload, &partial)
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
	return func(_ *testing.T, s Scenario, timeout time.Duration) ingest.Provider {
		return fakeProvider{s: s, timeout: timeout, fault: fault}
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
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(string(c.fault), func(t *testing.T) {
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
