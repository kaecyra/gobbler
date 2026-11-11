package llmusage_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/llm"
	"github.com/kaecyra/gobbler/internal/llmusage"
)

type rawCall struct {
	outcome string
	err     sql.NullString
	cost    sql.NullInt64
	in, out int64
	hh      sql.NullInt64
}

func rawCalls(t *testing.T, d *sql.DB) []rawCall {
	t.Helper()
	rows, err := d.Query(`SELECT outcome, error, cost_micros, input_tokens, output_tokens, household_id FROM llm_calls ORDER BY id`)
	if err != nil {
		t.Fatalf("query calls: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []rawCall
	for rows.Next() {
		var c rawCall
		if err := rows.Scan(&c.outcome, &c.err, &c.cost, &c.in, &c.out, &c.hh); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

func TestASuccessfulAndAFailedCallEachMakeARecord(t *testing.T) {
	ctx := context.Background()
	s, d := newStore(t)
	if err := s.Record(ctx, okCall(claudeModel)); err != nil {
		t.Fatalf("Record ok: %v", err)
	}
	failed := llmusage.Call{Task: llm.TaskVision, Provider: llm.ProviderGemini, Model: geminiModel, Latency: 1500 * time.Millisecond, Err: "gemini call to fixture-gemini-model: 503 overloaded"}
	if err := s.Record(ctx, failed); err != nil {
		t.Fatalf("Record failed: %v", err)
	}
	got := rawCalls(t, d)
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
	if got[0].outcome != "ok" || got[0].err.Valid || got[0].in != 1000 || got[0].out != 2000 {
		t.Errorf("successful call recorded as %+v", got[0])
	}
	if got[1].outcome != "error" || got[1].err.String != failed.Err {
		t.Errorf("failed call recorded as %+v, want its error %q", got[1], failed.Err)
	}
}

func TestACostIsComputedFromThePriceInForceAndKeptInMicros(t *testing.T) {
	ctx := context.Background()
	s, d := newStore(t)
	if err := s.SetPrice(ctx, llm.ProviderClaude, claudeModel, "3", "15.00"); err != nil {
		t.Fatalf("SetPrice: %v", err)
	}
	if err := s.Record(ctx, okCall(claudeModel)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	// 1000 input tokens at $3 per million plus 2000 output at $15 per million.
	const want = 1000*3_000_000/1_000_000 + 2000*15_000_000/1_000_000
	got := rawCalls(t, d)
	if len(got) != 1 || !got[0].cost.Valid || got[0].cost.Int64 != want {
		t.Fatalf("cost = %+v, want %d millionths of a dollar", got, want)
	}
}

func TestAnUnpricedModelsCallHasNoCostNotZero(t *testing.T) {
	ctx := context.Background()
	s, d := newStore(t)
	if err := s.Record(ctx, okCall("fixture-never-priced")); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got := rawCalls(t, d)
	if len(got) != 1 || got[0].cost.Valid {
		t.Fatalf("cost = %+v, want NULL", got)
	}
	rep, err := s.Report(ctx, mustWindow(t, "all"))
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if rep.Totals.Unpriced != 1 || rep.Totals.CostMicros != 0 || rep.Totals.Calls != 1 {
		t.Errorf("totals = %+v, want 1 unpriced call and no cost", rep.Totals)
	}
}

func TestChangingAPriceLaterLeavesRecordedCostsUnchanged(t *testing.T) {
	ctx := context.Background()
	s, d := newStore(t)
	must(t, s.SetPrice(ctx, llm.ProviderClaude, claudeModel, "3", "15"))
	must(t, s.Record(ctx, okCall(claudeModel)))
	before := rawCalls(t, d)[0].cost

	must(t, s.SetPrice(ctx, llm.ProviderClaude, claudeModel, "30", "150"))
	must(t, s.Record(ctx, okCall(claudeModel)))
	must(t, s.ClearPrice(ctx, llm.ProviderClaude, claudeModel))
	must(t, s.Record(ctx, okCall(claudeModel)))

	got := rawCalls(t, d)
	if got[0].cost != before {
		t.Errorf("first call's cost changed from %v to %v", before, got[0].cost)
	}
	if got[1].cost.Int64 != 10*before.Int64 {
		t.Errorf("second call cost = %v, want ten times the first (%v)", got[1].cost, before)
	}
	if got[2].cost.Valid {
		t.Errorf("a call after the price was cleared has cost %v, want NULL", got[2].cost)
	}
}

// Deleting a household deletes its calls with the rest of its data (ADR-00018,
// Members; ADR-00059 amends ADR-00058's "kept indefinitely").
func TestAHouseholdIsRecordedAndDeletingItDeletesItsCalls(t *testing.T) {
	ctx := context.Background()
	s, d := newStore(t)
	if _, err := d.Exec(`INSERT INTO households (id, name, created_at) VALUES (7, 'Fixture Household', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed household: %v", err)
	}
	c := okCall(claudeModel)
	c.HouseholdID = 7
	must(t, s.Record(ctx, c))
	must(t, s.Record(ctx, okCall(claudeModel)))
	got := rawCalls(t, d)
	if !got[0].hh.Valid || got[0].hh.Int64 != 7 || got[1].hh.Valid {
		t.Errorf("households = %v and %v, want 7 and NULL", got[0].hh, got[1].hh)
	}
	// Deleting the household deletes its calls (ADR-00018, Members) and leaves
	// the ones that ran for none.
	if _, err := d.Exec(`DELETE FROM households WHERE id = 7`); err != nil {
		t.Fatalf("delete household: %v", err)
	}
	if got := rawCalls(t, d); len(got) != 1 || got[0].hh.Valid {
		t.Errorf("after the household was deleted: %+v, want only the call that ran for none", got)
	}
}

func TestRecordRefusesWhatItCannotStore(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	for name, c := range map[string]llmusage.Call{
		"unknown task": {Task: "audio", Provider: llm.ProviderClaude, Model: "m"},
		"no model":     {Task: llm.TaskText, Provider: llm.ProviderClaude},
		"no provider":  {Task: llm.TaskText, Model: "m"},
	} {
		if err := s.Record(ctx, c); err == nil {
			t.Errorf("%s: Record succeeded", name)
		}
	}
}

func TestALongErrorIsCutAtACharacterBoundary(t *testing.T) {
	ctx := context.Background()
	s, d := newStore(t)
	c := okCall(claudeModel)
	c.Err = strings.Repeat("é", llmusage.MaxErrorBytes)
	must(t, s.Record(ctx, c))
	e := rawCalls(t, d)[0].err.String
	if len(e) > llmusage.MaxErrorBytes+len("…") || !strings.HasSuffix(e, "…") || !isValidUTF8(e) {
		t.Errorf("stored error is %d bytes, valid=%v", len(e), isValidUTF8(e))
	}
}
