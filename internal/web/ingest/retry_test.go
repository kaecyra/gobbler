package ingest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
)

// fakeRetrier answers Retry with a fixed draft or error and records calls.
type fakeRetrier struct {
	out        ingestcore.Draft
	err        error
	configured bool
	got        []ingestcore.Draft
}

func (f *fakeRetrier) Retry(_ context.Context, d ingestcore.Draft) (ingestcore.Draft, error) {
	f.got = append(f.got, d)
	return f.out, f.err
}

func (f *fakeRetrier) Configured() bool { return f.configured }

func llmDraft() ingestcore.Draft {
	d := sampleDraft()
	d.UsedLLM = true
	d.BelowThreshold = false
	d.Overall = 0.9
	d.Name = ingestcore.Field[string]{Value: "LLM pie", Confidence: 1, FromLLM: true}
	d.Components[0].Lines[0].FromLLM = true
	d.Components[0].Steps[0].FromLLM = true
	return d
}

func withRetrier(t *testing.T, r *fakeRetrier) (*fixture, string) {
	t.Helper()
	f := newFixture(t)
	f.h.retrier = r
	return f, f.ingestText(t)
}

func TestRetryReplacesDraftAndSendsBackToReviewWithoutSaving(t *testing.T) {
	r := &fakeRetrier{configured: true, out: llmDraft()}
	f, token := withRetrier(t, r)

	rec := f.do(http.MethodPost, "/"+token+"/retry", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ingest/"+token {
		t.Fatalf("status %d location %q; want 303 back to the review", rec.Code, rec.Header().Get("Location"))
	}
	if len(r.got) != 1 || r.got[0].Name.Value != "Apple pie" {
		t.Fatalf("retrier calls = %+v, want one with the pending rule-based draft", r.got)
	}
	body := f.do(http.MethodGet, "/"+token, nil).Body.String()
	if !strings.Contains(body, "LLM pie") {
		t.Error("review does not show the LLM draft")
	}
	if len(f.store.recipes) != 0 || len(f.store.created) != 0 {
		t.Error("a retry saved something; only confirm may")
	}
	// The save path still needs the user's confirmation.
	if rec := f.do(http.MethodPost, "/"+token+"/confirm", reviewForm()); rec.Code != http.StatusSeeOther {
		t.Errorf("confirm after retry: status %d", rec.Code)
	}
	if len(f.store.recipes) != 1 {
		t.Errorf("recipes saved = %d, want 1 after confirm", len(f.store.recipes))
	}
}

func TestRetryFailureShowsErrorKeepsRuleDraftAndStaysSaveable(t *testing.T) { // ADR-00007 obligation 4
	r := &fakeRetrier{configured: true, err: fmt.Errorf("%w: timed out", ingestcore.ErrLLMFailed)}
	f, token := withRetrier(t, r)

	rec := f.do(http.MethodPost, "/"+token+"/retry", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "timed out") || !strings.Contains(body, "The draft below is unchanged") {
		t.Errorf("error not shown with the unchanged-draft note: %s", between(t, body, "<h1>", "<form"))
	}
	if !strings.Contains(body, "Apple pie") || strings.Contains(body, "LLM pie") {
		t.Error("the rule-based draft must be what is shown")
	}
	if strings.Contains(body, "llm-mark") && strings.Contains(between(t, body, "<h1>", "<form id=\"editor\""), "filled the gaps") {
		t.Error("a failed retry must not be presented as an LLM draft")
	}
	// A second look at the pending draft has no partial LLM state.
	again := f.do(http.MethodGet, "/"+token, nil).Body.String()
	if strings.Contains(again, "timed out") {
		t.Error("the failure must not stick to the pending draft as if it were part of it")
	}
	if rec := f.do(http.MethodPost, "/"+token+"/confirm", reviewForm()); rec.Code != http.StatusSeeOther {
		t.Errorf("confirm after a failed retry: status %d", rec.Code)
	}
	if len(f.store.recipes) != 1 {
		t.Errorf("recipes saved = %d, want the rule-based draft saved", len(f.store.recipes))
	}
}

func TestRetryWithoutProviderSaysSoAndOffersNoButton(t *testing.T) {
	r := &fakeRetrier{configured: false, err: ingestcore.ErrNoProvider}
	f, token := withRetrier(t, r)
	if body := f.do(http.MethodGet, "/"+token, nil).Body.String(); strings.Contains(body, "Retry with LLM") {
		t.Error("retry offered with no provider configured")
	}
	rec := f.do(http.MethodPost, "/"+token+"/retry", nil)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "No LLM provider is configured") {
		t.Errorf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestRetryOfferedForAnyDraftWhenConfigured(t *testing.T) {
	r := &fakeRetrier{configured: true}
	f, token := withRetrier(t, r)
	body := f.do(http.MethodGet, "/"+token, nil).Body.String()
	if !strings.Contains(body, `action="/ingest/`+token+`/retry"`) || !strings.Contains(body, "Retry with LLM") {
		t.Error("the review screen offers no retry action")
	}
	// Also for a draft the rules were confident about.
	f.h.pending.put(householdA.ID, token, func() ingestcore.Draft { d := sampleDraft(); d.BelowThreshold = false; return d }())
	if body := f.do(http.MethodGet, "/"+token, nil).Body.String(); !strings.Contains(body, "Retry with LLM") {
		t.Error("no retry for a high-confidence draft")
	}
}

func TestRetryUnknownTokenIs404(t *testing.T) {
	f, _ := withRetrier(t, &fakeRetrier{configured: true})
	if rec := f.do(http.MethodPost, "/nope/retry", url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestProviderSetupFailureIsReportedOnRetryNotAsOutage(t *testing.T) {
	r := &fakeRetrier{configured: false}
	f, token := withRetrier(t, r)
	f.h.llmInitErr = errors.New("claude: API key is required")
	rec := f.do(http.MethodPost, "/"+token+"/retry", nil)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "API key is required") {
		t.Errorf("status %d body %s", rec.Code, rec.Body)
	}
	if rec := f.do(http.MethodGet, "/"+token, nil); rec.Code != http.StatusOK {
		t.Errorf("review must still work: %d", rec.Code)
	}
}

func TestReviewMarksLLMSuppliedValues(t *testing.T) { // ADR-00006 obligation 4
	f := newFixture(t)
	f.drafter.draft = llmDraft()
	body := f.do(http.MethodGet, "/"+f.ingestText(t), nil).Body.String()
	name := between(t, body, `<label for="f-name">`, "</div>")
	if !strings.Contains(name, `llm-mark">LLM`) {
		t.Errorf("LLM name not marked: %s", name)
	}
	line := between(t, body, `<li class="item line llm" data-llm>`, "</li>")
	if !strings.Contains(line, "LLM") || !strings.Contains(line, "butter") {
		t.Errorf("LLM line not marked: %s", line)
	}
	if !strings.Contains(body, "The LLM filled the gaps the rules left") {
		t.Error("no summary of LLM origin")
	}
	// A rule-based draft carries no marker.
	g := newFixture(t)
	if b := g.do(http.MethodGet, "/"+g.ingestText(t), nil).Body.String(); strings.Contains(b, `class="llm-mark"`) {
		t.Error("a rule-based draft shows an LLM marker")
	}
}

func TestReviewShowsAutomaticFallbackFailure(t *testing.T) {
	f := newFixture(t)
	f.drafter.draft.LLMError = "LLM provider: timed out"
	rec := f.do(http.MethodGet, "/"+f.ingestText(t), nil)
	body := rec.Body.String()
	if !strings.Contains(body, "timed out") || !strings.Contains(body, "You can still review and save it") {
		t.Errorf("fallback failure not shown: %s", body)
	}
	if rec2 := f.do(http.MethodPost, "/"+f.ingestText(t)+"/confirm", reviewForm()); rec2.Code != http.StatusSeeOther {
		t.Errorf("a draft with an LLM error must stay saveable: %d", rec2.Code)
	}
}

func TestProviderFactoriesCoverEveryConfiguredProvider(t *testing.T) {
	for _, name := range []string{config.ProviderClaude, config.ProviderGemini} {
		f, ok := providerFactories()[name]
		if !ok {
			t.Fatalf("no factory for %q", name)
		}
		// An incomplete configuration fails loudly instead of yielding a
		// nil-wrapping provider.
		p, err := f(context.Background(), config.LLM{Provider: name}, nil)
		if err == nil || p != nil {
			t.Errorf("%s: got %v, %v; want an error and a nil provider", name, p, err)
		}
	}
}

func TestRetryCatalogFailureIsAPipelineError(t *testing.T) {
	r := &fakeRetrier{configured: true, err: errors.New("read catalog while drafting: disk on fire")}
	f, token := withRetrier(t, r)
	rec := f.do(http.MethodPost, "/"+token+"/retry", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "The LLM could not read") {
		t.Error("a catalog failure is reported as an LLM failure")
	}
}

func TestFailedRetryOfAnLLMDraftDoesNotCallItRuleBased(t *testing.T) {
	r := &fakeRetrier{configured: true, err: fmt.Errorf("%w: timed out", ingestcore.ErrLLMFailed)}
	f, token := withRetrier(t, r)
	f.h.pending.put(householdA.ID, token, llmDraft())
	body := f.do(http.MethodPost, "/"+token+"/retry", nil).Body.String()
	if strings.Contains(body, "rule-based") {
		t.Error("the alert calls an LLM-filled draft rule-based")
	}
}
