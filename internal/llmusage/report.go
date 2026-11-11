package llmusage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/llm"
	"github.com/kaecyra/gobbler/internal/llmusage/llmusagedb"
)

// Window is a span of time ending now that the usage page reports over.
type Window struct {
	// Key is the window's name in a query string.
	Key   string
	Label string
	// Span is how far back it reaches; zero is all time.
	Span time.Duration
}

// Windows lists the windows the page offers, the default first.
var Windows = []Window{
	{Key: "30d", Label: "Last 30 days", Span: 30 * 24 * time.Hour},
	{Key: "7d", Label: "Last 7 days", Span: 7 * 24 * time.Hour},
	{Key: "24h", Label: "Last 24 hours", Span: 24 * time.Hour},
	{Key: "all", Label: "All time"},
}

// WindowByKey returns the window named key, and false when there is none.
func WindowByKey(key string) (Window, bool) {
	for _, w := range Windows {
		if w.Key == key {
			return w, true
		}
	}
	return Window{}, false
}

// Totals are the sums over a set of calls. CostMicros adds only the calls that
// have a price; Unpriced counts the rest, so an unpriced call is never folded
// into the cost as free.
type Totals struct {
	Calls        int64
	InputTokens  int64
	OutputTokens int64
	Failed       int64
	CostMicros   int64
	Unpriced     int64
}

// FailedShare is the share of calls that failed, 0 to 1; 0 with no calls.
func (t Totals) FailedShare() float64 {
	if t.Calls == 0 {
		return 0
	}
	return float64(t.Failed) / float64(t.Calls)
}

// ModelTotals are the totals of one task, provider and model.
type ModelTotals struct {
	Task            llm.Task
	Provider, Model string
	Totals
}

// HouseholdTotals are the totals of one household. HouseholdID is 0 for the
// calls that ran for none, whose Name is empty.
type HouseholdTotals struct {
	HouseholdID int64
	Name        string
	Totals
}

// Failure is one failed call.
type Failure struct {
	At              time.Time
	Task            llm.Task
	Provider, Model string
	// Household is the household's name, empty for none.
	Household string
	Latency   time.Duration
	Error     string
}

// MaxFailures is how many recent failures a report lists.
const MaxFailures = 20

// Report is the usage over a window.
type Report struct {
	Window      Window
	Totals      Totals
	ByModel     []ModelTotals
	ByHousehold []HouseholdTotals
	Failures    []Failure
}

func totalsOf(calls, in, out, failed, cost, unpriced int64) Totals {
	return Totals{Calls: calls, InputTokens: in, OutputTokens: out, Failed: failed, CostMicros: cost, Unpriced: unpriced}
}

// Report reads the usage over w ending now, from one snapshot.
func (s *Store) Report(ctx context.Context, w Window) (Report, error) {
	since := ""
	if w.Span > 0 {
		since = stamp(s.now().Add(-w.Span))
	}
	rep := Report{Window: w}
	err := db.WithReadTx(ctx, s.db, s.q, func(q *llmusagedb.Queries, _ *sql.Tx) error {
		t, err := q.Summary(ctx, since)
		if err != nil {
			return fmt.Errorf("sum calls: %w", err)
		}
		rep.Totals = totalsOf(t.Calls, t.InputTokens, t.OutputTokens, t.Failed, t.CostMicros, t.Unpriced)
		models, err := q.SummaryByModel(ctx, since)
		if err != nil {
			return fmt.Errorf("sum calls by model: %w", err)
		}
		for _, m := range models {
			rep.ByModel = append(rep.ByModel, ModelTotals{
				Task: llm.Task(m.Task), Provider: m.Provider, Model: m.Model,
				Totals: totalsOf(m.Calls, m.InputTokens, m.OutputTokens, m.Failed, m.CostMicros, m.Unpriced),
			})
		}
		households, err := q.SummaryByHousehold(ctx, since)
		if err != nil {
			return fmt.Errorf("sum calls by household: %w", err)
		}
		for _, h := range households {
			rep.ByHousehold = append(rep.ByHousehold, HouseholdTotals{
				HouseholdID: h.HouseholdID.Int64, Name: h.HouseholdName,
				Totals: totalsOf(h.Calls, h.InputTokens, h.OutputTokens, h.Failed, h.CostMicros, h.Unpriced),
			})
		}
		failures, err := q.RecentFailures(ctx, llmusagedb.RecentFailuresParams{CalledAt: since, Limit: MaxFailures})
		if err != nil {
			return fmt.Errorf("list failures: %w", err)
		}
		for _, f := range failures {
			at, err := parseStamp(f.CalledAt)
			if err != nil {
				return err
			}
			rep.Failures = append(rep.Failures, Failure{
				At: at, Task: llm.Task(f.Task), Provider: f.Provider, Model: f.Model,
				Household: f.HouseholdName, Latency: time.Duration(f.LatencyMs) * time.Millisecond, Error: f.Error,
			})
		}
		return nil
	})
	if err != nil {
		return Report{}, err
	}
	return rep, nil
}
