package llmusage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/llm"
	"github.com/kaecyra/gobbler/internal/llmusage/llmusagedb"
)

// timeFormat is how times are stored: UTC, fixed width, so text comparison
// orders them.
const timeFormat = "2006-01-02T15:04:05Z"

func stamp(t time.Time) string { return t.UTC().Format(timeFormat) }

func parseStamp(s string) (time.Time, error) {
	t, err := time.Parse(timeFormat, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("stored time %q: %w", s, err)
	}
	return t, nil
}

// Call is one LLM call to record.
type Call struct {
	// At is when the call started.
	At time.Time
	// Task is llm.TaskText or llm.TaskVision.
	Task llm.Task
	// Provider is llm.ProviderClaude or llm.ProviderGemini.
	Provider string
	Model    string
	// HouseholdID is the household the call ran for, 0 for none.
	HouseholdID int64
	// Input and Output are the tokens the provider reported, zero on a failed
	// call.
	Input, Output int64
	Latency       time.Duration
	// Err is why the call failed, with no API key in it; empty for a call
	// that succeeded.
	Err string
}

// MaxErrorBytes bounds a recorded error, so one runaway message cannot fill
// the table.
const MaxErrorBytes = 1000

// ModelRef names one model of one provider.
type ModelRef struct{ Provider, Model string }

// ModelSource lists the models the tasks are set to use now. *Configured
// satisfies it.
type ModelSource interface {
	Configured(ctx context.Context) ([]ModelRef, error)
}

// Store keeps the call records and the prices. Create it with New.
type Store struct {
	db         *sql.DB
	q          *llmusagedb.Queries
	configured ModelSource
	now        func() time.Time
}

// New returns a store over d. configured says which models the tasks are set to
// use, so the price list shows them before any call has used them; nil lists
// only the models calls have used or that have a price.
func New(d *sql.DB, configured ModelSource) *Store {
	return &Store{db: d, q: llmusagedb.New(d), configured: configured, now: time.Now}
}

// Record stores c. Its cost is computed here, from the price in force now; a
// model with no price leaves it NULL. A later price change does not touch it.
func (s *Store) Record(ctx context.Context, c Call) error {
	if c.Task != llm.TaskText && c.Task != llm.TaskVision {
		return fmt.Errorf("record LLM call: task %q is not text or vision", c.Task)
	}
	if c.Provider == "" || c.Model == "" {
		return errors.New("record LLM call: a provider and a model are required")
	}
	if c.At.IsZero() {
		c.At = s.now()
	}
	arg := llmusagedb.InsertCallParams{
		CalledAt: stamp(c.At), Task: c.Task.String(), Provider: c.Provider, Model: c.Model,
		HouseholdID: sql.NullInt64{Int64: c.HouseholdID, Valid: c.HouseholdID != 0},
		InputTokens: max(c.Input, 0), OutputTokens: max(c.Output, 0), LatencyMs: max(c.Latency.Milliseconds(), 0),
		Outcome: "ok",
	}
	if c.Err != "" {
		arg.Outcome = "error"
		arg.Error = sql.NullString{String: truncate(c.Err, MaxErrorBytes), Valid: true}
	}
	err := db.WithTx(ctx, s.db, s.q, func(q *llmusagedb.Queries, _ *sql.Tx) error {
		p, err := q.GetPrice(ctx, llmusagedb.GetPriceParams{Provider: c.Provider, Model: c.Model})
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return fmt.Errorf("read price: %w", err)
		default:
			cost := Price{InputMicros: p.InputMicros, OutputMicros: p.OutputMicros}.Cost(arg.InputTokens, arg.OutputTokens)
			arg.CostMicros = sql.NullInt64{Int64: cost, Valid: true}
		}
		return q.InsertCall(ctx, arg)
	})
	if err != nil {
		return fmt.Errorf("record %s call to %s %s: %w", c.Task, c.Provider, c.Model, err)
	}
	return nil
}

// truncate cuts s to at most n bytes without splitting a character.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

func priceOf(r llmusagedb.LlmPrice) (Price, error) {
	p := Price{Provider: r.Provider, Model: r.Model, InputMicros: r.InputMicros, OutputMicros: r.OutputMicros, Source: r.Source}
	var err error
	if r.SyncedAt.Valid {
		if p.SyncedAt, err = parseStamp(r.SyncedAt.String); err != nil {
			return Price{}, err
		}
	}
	if p.UpdatedAt, err = parseStamp(r.UpdatedAt); err != nil {
		return Price{}, err
	}
	return p, nil
}

// PriceRow is one model in the price list: its price, or none.
type PriceRow struct {
	ModelRef
	// Price is nil for a model with no price: its calls are unpriced.
	Price *Price
}

// Prices lists every model a call has used, a task is set to use, or that has a
// price, with its price.
func (s *Store) Prices(ctx context.Context) ([]PriceRow, error) {
	var configured []ModelRef
	if s.configured != nil {
		var err error
		if configured, err = s.configured.Configured(ctx); err != nil {
			return nil, fmt.Errorf("list configured models: %w", err)
		}
	}
	var rows []PriceRow
	err := db.WithReadTx(ctx, s.db, s.q, func(q *llmusagedb.Queries, _ *sql.Tx) error {
		priced, err := q.ListPrices(ctx)
		if err != nil {
			return fmt.Errorf("list prices: %w", err)
		}
		byModel := map[ModelRef]*Price{}
		for _, r := range priced {
			p, err := priceOf(r)
			if err != nil {
				return err
			}
			byModel[ModelRef{r.Provider, r.Model}] = &p
		}
		used, err := q.UsedModels(ctx)
		if err != nil {
			return fmt.Errorf("list used models: %w", err)
		}
		seen := map[ModelRef]bool{}
		add := func(m ModelRef) {
			if !seen[m] {
				seen[m] = true
				rows = append(rows, PriceRow{ModelRef: m, Price: byModel[m]})
			}
		}
		for m := range byModel {
			add(m)
		}
		for _, u := range used {
			add(ModelRef{u.Provider, u.Model})
		}
		for _, m := range configured {
			add(m)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(rows, func(a, b PriceRow) int {
		return strings.Compare(a.Provider+"\x00"+a.Model, b.Provider+"\x00"+b.Model)
	})
	return rows, nil
}

// SetPrice sets the price of a model as the operator typed it, in US dollars
// per million tokens, and marks it manual: the sync never overwrites it.
func (s *Store) SetPrice(ctx context.Context, provider, model, input, output string) error {
	if err := checkModel(provider, model); err != nil {
		return err
	}
	in, err := ParseUSDPerMillion(input)
	if err != nil {
		return fmt.Errorf("input price: %w", err)
	}
	out, err := ParseUSDPerMillion(output)
	if err != nil {
		return fmt.Errorf("output price: %w", err)
	}
	err = db.WithTx(ctx, s.db, s.q, func(q *llmusagedb.Queries, _ *sql.Tx) error {
		return q.SetManualPrice(ctx, llmusagedb.SetManualPriceParams{
			Provider: provider, Model: model, InputMicros: in, OutputMicros: out, UpdatedAt: stamp(s.now()),
		})
	})
	if err != nil {
		return fmt.Errorf("set price of %s %s: %w", provider, model, err)
	}
	return nil
}

// ClearPrice removes a model's price, manual or synced. The next sync sets it
// again if OpenRouter lists the model.
func (s *Store) ClearPrice(ctx context.Context, provider, model string) error {
	if err := checkModel(provider, model); err != nil {
		return err
	}
	err := db.WithTx(ctx, s.db, s.q, func(q *llmusagedb.Queries, _ *sql.Tx) error {
		return q.DeletePrice(ctx, llmusagedb.DeletePriceParams{Provider: provider, Model: model})
	})
	if err != nil {
		return fmt.Errorf("clear price of %s %s: %w", provider, model, err)
	}
	return nil
}

func checkModel(provider, model string) error {
	if llm.KeyField(provider) == "" {
		return refuse("%q is not a provider", provider)
	}
	if strings.TrimSpace(model) == "" {
		return refuse("a model is required")
	}
	return nil
}

// SyncStatus is what is known about the price sync.
type SyncStatus struct {
	// LastSuccess and LastFailure are zero when there has been none.
	LastSuccess time.Time
	LastFailure time.Time
	// LastError is why the last failure failed.
	LastError string
}

// SyncStatus reads the sync's status.
func (s *Store) SyncStatus(ctx context.Context) (SyncStatus, error) {
	r, err := s.q.GetSyncStatus(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return SyncStatus{}, nil
	}
	if err != nil {
		return SyncStatus{}, fmt.Errorf("read price sync status: %w", err)
	}
	var st SyncStatus
	if r.LastSuccessAt.Valid {
		if st.LastSuccess, err = parseStamp(r.LastSuccessAt.String); err != nil {
			return SyncStatus{}, err
		}
	}
	if r.LastFailureAt.Valid {
		if st.LastFailure, err = parseStamp(r.LastFailureAt.String); err != nil {
			return SyncStatus{}, err
		}
	}
	st.LastError = r.LastError.String
	return st, nil
}
