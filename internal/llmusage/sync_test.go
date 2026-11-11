package llmusage_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/llm"
	"github.com/kaecyra/gobbler/internal/llmusage"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func priceOf(t *testing.T, s *llmusage.Store, provider, model string) *llmusage.Price {
	t.Helper()
	rows, err := s.Prices(context.Background())
	if err != nil {
		t.Fatalf("Prices: %v", err)
	}
	for _, r := range rows {
		if r.Provider == provider && r.Model == model {
			return r.Price
		}
	}
	t.Fatalf("%s %s is not in the price list", provider, model)
	return nil
}

func TestTheSyncSetsTheSyncedPriceOfEveryModelInUseFromTheRecordedListing(t *testing.T) {
	ctx := context.Background()
	claude := llmusage.ModelRef{Provider: llm.ProviderClaude, Model: claudeModel}
	// The Gemini model is only configured, and the Claude one only used.
	s, _ := newStore(t, llmusage.ModelRef{Provider: llm.ProviderGemini, Model: geminiModel})
	must(t, s.Record(ctx, okCall(claude.Model)))
	srv, hits := listing(t)

	must(t, llmusage.NewSyncer(s, srv.URL, quiet()).Sync(ctx))

	if *hits != 1 {
		t.Errorf("listing fetched %d times, want 1", *hits)
	}
	c := priceOf(t, s, llm.ProviderClaude, claudeModel)
	if c == nil || c.InputMicros != 3_000_000 || c.OutputMicros != 15_000_000 || c.Source != llmusage.SourceOpenRouter || c.SyncedAt.IsZero() {
		t.Errorf("claude price = %+v, want $3 and $15 per million from openrouter", c)
	}
	g := priceOf(t, s, llm.ProviderGemini, geminiModel)
	if g == nil || g.InputMicros != 300_000 || g.OutputMicros != 2_500_000 {
		t.Errorf("gemini price = %+v, want $0.30 and $2.50 per million", g)
	}
	st, err := s.SyncStatus(ctx)
	must(t, err)
	if st.LastSuccess.IsZero() || !st.LastFailure.IsZero() {
		t.Errorf("status = %+v, want a success and no failure", st)
	}
}

func TestTheSyncUpdatesAPriceItSetBefore(t *testing.T) {
	ctx := context.Background()
	s, d := newStore(t)
	must(t, s.Record(ctx, okCall(claudeModel)))
	if _, err := d.Exec(`INSERT INTO llm_prices (provider, model, input_micros, output_micros, source, synced_at, updated_at)
		VALUES ('claude', ?, 1, 2, 'openrouter', '2020-01-01T00:00:00Z', '2020-01-01T00:00:00Z')`, claudeModel); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv, _ := listing(t)
	must(t, llmusage.NewSyncer(s, srv.URL, quiet()).Sync(ctx))
	if p := priceOf(t, s, llm.ProviderClaude, claudeModel); p.InputMicros != 3_000_000 || p.SyncedAt.Year() < 2026 {
		t.Errorf("price = %+v, want the listing's, synced now", p)
	}
}

func TestTheSyncLeavesAManualPriceAlone(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	must(t, s.Record(ctx, okCall(claudeModel)))
	must(t, s.SetPrice(ctx, llm.ProviderClaude, claudeModel, "1.25", "7.5"))
	srv, _ := listing(t)

	must(t, llmusage.NewSyncer(s, srv.URL, quiet()).Sync(ctx))

	p := priceOf(t, s, llm.ProviderClaude, claudeModel)
	if p.Source != llmusage.SourceManual || p.InputMicros != 1_250_000 || p.OutputMicros != 7_500_000 || !p.SyncedAt.IsZero() {
		t.Errorf("manual price changed by the sync: %+v", p)
	}
}

func TestAFailedFetchChangesNothingAndRecordsTheFailure(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	must(t, s.Record(ctx, okCall(claudeModel)))
	must(t, s.Record(ctx, okCall("fixture-manual-model")))
	must(t, s.SetPrice(ctx, llm.ProviderClaude, "fixture-manual-model", "2", "4"))
	srv, _ := listing(t)
	syncer := llmusage.NewSyncer(s, srv.URL, quiet())
	must(t, syncer.Sync(ctx))
	before, err := s.Prices(ctx)
	must(t, err)

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream down", http.StatusBadGateway)
	}))
	defer down.Close()
	err = llmusage.NewSyncer(s, down.URL, quiet()).Sync(ctx)
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("Sync error = %v, want one naming status 502", err)
	}

	after, err := s.Prices(ctx)
	must(t, err)
	if len(before) != len(after) {
		t.Fatalf("price list changed length: %d to %d", len(before), len(after))
	}
	for i := range before {
		if *before[i].Price != *after[i].Price {
			t.Errorf("price of %s changed from %+v to %+v", before[i].Model, *before[i].Price, *after[i].Price)
		}
	}
	st, err := s.SyncStatus(ctx)
	must(t, err)
	if st.LastFailure.IsZero() || !strings.Contains(st.LastError, "502") || st.LastSuccess.IsZero() {
		t.Errorf("status = %+v, want the failure recorded and the earlier success kept", st)
	}
}

func TestAnUnreadableListingIsAFailureToo(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	must(t, s.Record(ctx, okCall(claudeModel)))
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<html>`)) }))
	defer bad.Close()
	if err := llmusage.NewSyncer(s, bad.URL, quiet()).Sync(ctx); err == nil {
		t.Fatal("Sync of an unreadable listing succeeded")
	}
	if p := priceOf(t, s, llm.ProviderClaude, claudeModel); p != nil {
		t.Errorf("price = %+v, want none", p)
	}
}

func TestAModelMatchesOnlyTheExactIDWithItsProviderPrefix(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	for _, m := range []struct{ provider, model string }{
		{llm.ProviderClaude, "fixture-dotted-4-5"},             // listed as fixture-dotted-4.5
		{llm.ProviderClaude, "fixture-claude-mod"},             // a prefix of a listed id
		{llm.ProviderGemini, "fixture-other-vendor"},           // listed only under openai/
		{llm.ProviderGemini, "fixture-claude-model"},           // listed only under anthropic/
		{llm.ProviderGemini, "fixture-variable-price"},         // listed with a negative price
		{llm.ProviderClaude, "FIXTURE-CLAUDE-MODEL"},           // other case
		{llm.ProviderClaude, "anthropic/fixture-claude-model"}, // the prefix already on
	} {
		must(t, s.Record(ctx, llmusage.Call{Task: llm.TaskText, Provider: m.provider, Model: m.model}))
	}
	srv, _ := listing(t)
	must(t, llmusage.NewSyncer(s, srv.URL, quiet()).Sync(ctx))

	rows, err := s.Prices(ctx)
	must(t, err)
	if len(rows) != 7 {
		t.Fatalf("price list has %d models, want 7", len(rows))
	}
	for _, r := range rows {
		if r.Price != nil {
			t.Errorf("%s %s was priced: %+v", r.Provider, r.Model, *r.Price)
		}
	}
}

func TestAnExactIDWithADifferentSuffixIsAnotherModel(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	must(t, s.Record(ctx, okCall(claudeModel)))
	srv, _ := listing(t)
	must(t, llmusage.NewSyncer(s, srv.URL, quiet()).Sync(ctx))
	// The listing also has fixture-claude-model:extended at three times the
	// price; the exact id gets its own.
	if p := priceOf(t, s, llm.ProviderClaude, claudeModel); p.InputMicros != 3_000_000 {
		t.Errorf("price = %+v, want the exact entry's", p)
	}
}

func TestDueWhenNothingSucceededInADay(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	if due, err := s.Due(ctx); err != nil || !due {
		t.Fatalf("Due before any sync = %v, %v, want true", due, err)
	}
	srv, _ := listing(t)
	must(t, s.Record(ctx, okCall(claudeModel)))
	must(t, llmusage.NewSyncer(s, srv.URL, quiet()).Sync(ctx))
	if due, err := s.Due(ctx); err != nil || due {
		t.Errorf("Due just after a sync = %v, %v, want false", due, err)
	}
}

func TestWithNoModelUsedOrChosenTheSyncFetchesNothingAndRecordsNothing(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	srv, hits := listing(t)

	must(t, llmusage.NewSyncer(s, srv.URL, quiet()).Sync(ctx))

	if *hits != 0 {
		t.Errorf("the listing was fetched %d times, want none: there is nothing to price", *hits)
	}
	st, err := s.SyncStatus(ctx)
	must(t, err)
	if st != (llmusage.SyncStatus{}) {
		t.Errorf("status = %+v, want nothing recorded: no success, and no failure", st)
	}
	if due, err := s.Due(ctx); err != nil || !due {
		t.Errorf("Due = %v, %v, want true so the first model chosen is priced at the next check", due, err)
	}
	// Once a model is in use the same syncer fetches.
	must(t, s.Record(ctx, okCall(claudeModel)))
	must(t, llmusage.NewSyncer(s, srv.URL, quiet()).Sync(ctx))
	if *hits != 1 {
		t.Errorf("after a call, the listing was fetched %d times, want 1", *hits)
	}
}
