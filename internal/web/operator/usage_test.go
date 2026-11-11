package operator

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/llm"
	"github.com/kaecyra/gobbler/internal/llmusage"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// A placeholder identifier: no real model name is written here.
const usageModel = "fixture-usage-model"

type fakeSync struct {
	calls int
	err   error
}

func (f *fakeSync) Sync(context.Context) error { f.calls++; return f.err }

type usageEnv struct {
	db    *sql.DB
	store *llmusage.Store
	sync  *fakeSync
	deps  Deps
}

func newUsageEnv(t *testing.T) *usageEnv {
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
	e := &usageEnv{db: d, store: llmusage.New(d, nil), sync: &fakeSync{}}
	e.deps = Deps{Usage: e.store, PriceSync: e.sync}
	return e
}

func (e *usageEnv) do(t *testing.T, asOperator bool, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	ctx := verified(t, req)
	if asOperator {
		ctx = layout.WithOperator(ctx)
	}
	rec := httptest.NewRecorder()
	Routes(e.deps).ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func (e *usageEnv) record(t *testing.T, c llmusage.Call) {
	t.Helper()
	if err := e.store.Record(context.Background(), c); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

// ADR-00058 obligation 5: the page is an operator's alone.
func TestUsagePagesRefuseANonOperator(t *testing.T) {
	e := newUsageEnv(t)
	form := url.Values{"provider": {"claude"}, "model": {usageModel}, "input": {"1"}, "output": {"2"}}
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/usage"},
		{http.MethodPost, "/usage/sync"},
		{http.MethodPost, "/usage/prices"},
		{http.MethodPost, "/usage/prices/clear"},
	} {
		rec := e.do(t, false, c.method, c.path, form)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as a non-operator = %d, want 403", c.method, c.path, rec.Code)
		}
	}
	if e.sync.calls != 0 {
		t.Error("a non-operator started a sync")
	}
	if _, err := e.store.Prices(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ := e.store.Prices(context.Background())
	if len(rows) != 0 {
		t.Errorf("a non-operator set a price: %+v", rows)
	}
}

func TestUsagePageShowsTotalsAndNeverCountsUnpricedAsFree(t *testing.T) {
	e := newUsageEnv(t)
	ctx := context.Background()
	if err := e.store.SetPrice(ctx, llm.ProviderClaude, usageModel, "3", "15"); err != nil {
		t.Fatal(err)
	}
	e.record(t, llmusage.Call{Task: llm.TaskText, Provider: llm.ProviderClaude, Model: usageModel, Input: 1000, Output: 2000})
	e.record(t, llmusage.Call{Task: llm.TaskVision, Provider: llm.ProviderGemini, Model: "fixture-unpriced", Input: 5, Output: 5})
	e.record(t, llmusage.Call{Task: llm.TaskVision, Provider: llm.ProviderGemini, Model: "fixture-unpriced", Err: "provider said no"})

	rec := e.do(t, true, http.MethodGet, "/usage", nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /usage = %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		"$0.033",                        // 1000 in at $3 and 2000 out at $15 per million
		"2 unpriced, not in this total", // the two Gemini calls
		"33.3% of calls",                // one of three failed
		"provider said no",              // the failure
		"Unpriced",                      // the unpriced model's cost cell and price
		`href="/operator/usage"`,        // the rail
		`aria-current="page"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestUsagePageWindowDefaultsAndRefusesAnUnknownOne(t *testing.T) {
	e := newUsageEnv(t)
	if rec := e.do(t, true, http.MethodGet, "/usage?window=all", nil); rec.Code != http.StatusOK {
		t.Errorf("window=all = %d", rec.Code)
	}
	if rec := e.do(t, true, http.MethodGet, "/usage?window=forever", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("window=forever = %d, want 400", rec.Code)
	}
}

func TestSettingAPriceStoresItAsManualAndClearingItRemovesIt(t *testing.T) {
	e := newUsageEnv(t)
	form := url.Values{"window": {"7d"}, "provider": {"claude"}, "model": {usageModel}, "input": {"1.5"}, "output": {"6"}}
	rec := e.do(t, true, http.MethodPost, "/usage/prices", form)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/operator/usage?window=7d" {
		t.Fatalf("POST prices = %d to %q, want 303 back to the 7d window", rec.Code, rec.Header().Get("Location"))
	}
	rows, err := e.store.Prices(context.Background())
	if err != nil || len(rows) != 1 || rows[0].Price == nil || rows[0].Price.Source != llmusage.SourceManual || rows[0].Price.InputMicros != 1_500_000 {
		t.Fatalf("prices = %+v, %v", rows, err)
	}
	rec = e.do(t, true, http.MethodPost, "/usage/prices/clear", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST clear = %d", rec.Code)
	}
	if rows, _ := e.store.Prices(context.Background()); len(rows) != 0 {
		t.Errorf("prices after clear = %+v", rows)
	}
}

func TestABadPriceShowsTheProblemOnThePageAndSetsNothing(t *testing.T) {
	e := newUsageEnv(t)
	form := url.Values{"provider": {"claude"}, "model": {usageModel}, "input": {"cheap"}, "output": {"6"}}
	rec := e.do(t, true, http.MethodPost, "/usage/prices", form)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST prices = %d, want 422", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="usage-problem"`) || !strings.Contains(body, "input price") || strings.Contains(body, "invalid price: ") {
		t.Errorf("page does not state the problem plainly:\n%s", body)
	}
	if rows, _ := e.store.Prices(context.Background()); len(rows) != 0 {
		t.Errorf("a bad price was stored: %+v", rows)
	}
}

func TestSyncNowRunsTheSyncAndLandsOnThePage(t *testing.T) {
	e := newUsageEnv(t)
	rec := e.do(t, true, http.MethodPost, "/usage/sync", url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/operator/usage" || e.sync.calls != 1 {
		t.Fatalf("sync = %d to %q after %d calls", rec.Code, rec.Header().Get("Location"), e.sync.calls)
	}
	// A failed sync was recorded by the syncer; the page shows it, so this is a redirect too.
	e.sync.err = errors.Join(llmusage.ErrSyncFailed, errors.New("offline"))
	if rec := e.do(t, true, http.MethodPost, "/usage/sync", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Errorf("recorded failed sync = %d, want 303", rec.Code)
	}
	// A failure that could not even be recorded is an error.
	e.sync.err = errors.New("database is locked")
	if rec := e.do(t, true, http.MethodPost, "/usage/sync", url.Values{}); rec.Code < 500 {
		t.Errorf("unrecorded sync failure = %d, want a 5xx", rec.Code)
	}
}

func TestUsageRoutesAnswer500WithoutTheirDependencies(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/usage", nil)
	Routes(Deps{}).ServeHTTP(rec, req.WithContext(layout.WithOperator(verified(t, req))))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("= %d, want 500", rec.Code)
	}
}

func TestAFailureIsRaisedOnlyWhenItIsNewerThanTheLastSuccess(t *testing.T) {
	ok := time.Date(2026, 3, 9, 2, 30, 0, 0, time.UTC)
	rep := llmusage.Report{Window: llmusage.Windows[0]}
	for name, c := range map[string]struct {
		st     llmusage.SyncStatus
		raised bool
	}{
		"never ran":            {llmusage.SyncStatus{}, false},
		"failed after success": {llmusage.SyncStatus{LastSuccess: ok, LastFailure: ok.Add(time.Hour), LastError: "offline"}, true},
		"failed, then fixed":   {llmusage.SyncStatus{LastSuccess: ok.Add(time.Hour), LastFailure: ok, LastError: "offline"}, false},
		"failed, never fixed":  {llmusage.SyncStatus{LastFailure: ok, LastError: "offline"}, true},
	} {
		v := newUsageView(rep, nil, c.st, "")
		if v.Sync.FailureIsLatest != c.raised {
			t.Errorf("%s: raised = %v, want %v", name, v.Sync.FailureIsLatest, c.raised)
		}
		if (v.Sync.Failure != "") != !c.st.LastFailure.IsZero() || (v.Sync.Success != "") != !c.st.LastSuccess.IsZero() {
			t.Errorf("%s: sync view = %+v", name, v.Sync)
		}
	}
}

func TestAWindowOfOnlyUnpricedCallsReadsUnpricedNotZeroDollars(t *testing.T) {
	e := newUsageEnv(t)
	e.record(t, llmusage.Call{Task: llm.TaskText, Provider: llm.ProviderClaude, Model: "fixture-unpriced", Input: 10, Output: 10})
	body := e.do(t, true, http.MethodGet, "/usage", nil).Body.String()
	if strings.Contains(body, "$0.00") {
		t.Errorf("page shows a zero cost for calls that have no price")
	}
	v := newUsageView(llmusage.Report{Window: llmusage.Windows[0], Totals: llmusage.Totals{Calls: 2, Unpriced: 2}}, nil, llmusage.SyncStatus{}, "")
	if v.Stats[0].Value != "Unpriced" || v.Stats[0].Note != "" {
		t.Errorf("cost stat = %+v, want Unpriced with no note", v.Stats[0])
	}
}

func TestUseSyncedPriceSyncsAtOnceOnlyForAManualPrice(t *testing.T) {
	e := newUsageEnv(t)
	ctx := context.Background()
	form := url.Values{"provider": {"claude"}, "model": {usageModel}}
	if err := e.store.SetPrice(ctx, llm.ProviderClaude, usageModel, "1", "2"); err != nil {
		t.Fatal(err)
	}
	if rec := e.do(t, true, http.MethodPost, "/usage/prices/clear", form); rec.Code != http.StatusSeeOther || e.sync.calls != 1 {
		t.Fatalf("clearing a manual price = %d with %d syncs, want 303 and 1", rec.Code, e.sync.calls)
	}
	// A failed sync is shown on the page, not a 500.
	if err := e.store.SetPrice(ctx, llm.ProviderClaude, usageModel, "1", "2"); err != nil {
		t.Fatal(err)
	}
	e.sync.err = errors.Join(llmusage.ErrSyncFailed, errors.New("offline"))
	if rec := e.do(t, true, http.MethodPost, "/usage/prices/clear", form); rec.Code != http.StatusSeeOther {
		t.Errorf("clearing with a failed sync = %d, want 303", rec.Code)
	}
	// Removing a price the sync set needs no sync: the next one sets it again.
	e.sync.err, e.sync.calls = nil, 0
	e.record(t, llmusage.Call{Task: llm.TaskText, Provider: llm.ProviderClaude, Model: usageModel})
	if rec := e.do(t, true, http.MethodPost, "/usage/prices/clear", form); rec.Code != http.StatusSeeOther || e.sync.calls != 0 {
		t.Errorf("clearing an unpriced model = %d with %d syncs, want 303 and none", rec.Code, e.sync.calls)
	}
}
