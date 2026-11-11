package llmusage_test

import (
	"context"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/llm"
	"github.com/kaecyra/gobbler/internal/llmusage"
	"github.com/kaecyra/gobbler/internal/settings"
)

func TestReportTotalsAndBreakdowns(t *testing.T) {
	ctx := context.Background()
	s, d := newStore(t)
	for _, h := range []struct {
		id   int
		name string
	}{{1, "Alpha House"}, {2, "Beta House"}} {
		if _, err := d.Exec(`INSERT INTO households (id, name, created_at) VALUES (?, ?, '2026-01-01T00:00:00Z')`, h.id, h.name); err != nil {
			t.Fatalf("seed household: %v", err)
		}
	}
	must(t, s.SetPrice(ctx, llm.ProviderClaude, claudeModel, "3", "15"))
	rec := func(c llmusage.Call) { t.Helper(); must(t, s.Record(ctx, c)) }
	text := okCall(claudeModel)
	text.HouseholdID = 1
	rec(text)
	rec(text)
	rec(llmusage.Call{Task: llm.TaskVision, Provider: llm.ProviderGemini, Model: geminiModel, HouseholdID: 2, Input: 500, Output: 100})
	rec(llmusage.Call{Task: llm.TaskVision, Provider: llm.ProviderGemini, Model: geminiModel, HouseholdID: 2, Err: "boom"})
	rec(llmusage.Call{Task: llm.TaskText, Provider: llm.ProviderClaude, Model: claudeModel, Err: "no household"})

	rep, err := s.Report(ctx, mustWindow(t, "all"))
	must(t, err)

	// Two priced successes (33000 each) and a priced failure (cost 0); the
	// three Gemini-model calls have no price.
	want := llmusage.Totals{Calls: 5, InputTokens: 2500, OutputTokens: 4100, Failed: 2, CostMicros: 66_000, Unpriced: 2}
	if rep.Totals != want {
		t.Errorf("totals = %+v, want %+v", rep.Totals, want)
	}
	if got := rep.Totals.FailedShare(); got != 0.4 {
		t.Errorf("failed share = %v, want 0.4", got)
	}
	if len(rep.ByModel) != 2 {
		t.Fatalf("by model = %+v, want 2 rows", rep.ByModel)
	}
	if m := rep.ByModel[0]; m.Task != llm.TaskText || m.Model != claudeModel || m.Calls != 3 || m.CostMicros != 66_000 || m.Unpriced != 0 {
		t.Errorf("first model row = %+v", m)
	}
	if m := rep.ByModel[1]; m.Task != llm.TaskVision || m.Provider != llm.ProviderGemini || m.Calls != 2 || m.Unpriced != 2 || m.CostMicros != 0 {
		t.Errorf("second model row = %+v", m)
	}
	names := []string{}
	for _, h := range rep.ByHousehold {
		names = append(names, h.Name)
	}
	if len(names) != 3 || names[0] != "" || names[1] != "Alpha House" || names[2] != "Beta House" {
		t.Errorf("households = %q, want the household-less row first, then Alpha, Beta", names)
	}
	if h := rep.ByHousehold[1]; h.Calls != 2 || h.CostMicros != 66_000 {
		t.Errorf("Alpha row = %+v", h)
	}
	if len(rep.Failures) != 2 || rep.Failures[0].Error != "no household" || rep.Failures[1].Household != "Beta House" {
		t.Errorf("failures = %+v, want the newest first", rep.Failures)
	}
}

func TestReportOverAWindowLeavesOutOlderCalls(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	old := okCall(claudeModel)
	old.At = time.Now().Add(-10 * 24 * time.Hour)
	must(t, s.Record(ctx, old))
	must(t, s.Record(ctx, okCall(claudeModel)))
	for key, want := range map[string]int64{"24h": 1, "7d": 1, "30d": 2, "all": 2} {
		rep, err := s.Report(ctx, mustWindow(t, key))
		must(t, err)
		if rep.Totals.Calls != want {
			t.Errorf("window %s: %d calls, want %d", key, rep.Totals.Calls, want)
		}
	}
	if _, ok := llmusage.WindowByKey("1y"); ok {
		t.Error("an unknown window was found")
	}
}

func TestAnEmptyReportHasZeroTotalsAndNoShare(t *testing.T) {
	s, _ := newStore(t)
	rep, err := s.Report(context.Background(), mustWindow(t, "30d"))
	must(t, err)
	if rep.Totals != (llmusage.Totals{}) || rep.Totals.FailedShare() != 0 || len(rep.ByModel)+len(rep.ByHousehold)+len(rep.Failures) != 0 {
		t.Errorf("report = %+v", rep)
	}
}

func TestThePriceListHasConfiguredUsedAndPricedModels(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t, llmusage.ModelRef{Provider: llm.ProviderGemini, Model: geminiModel})
	must(t, s.Record(ctx, okCall(claudeModel)))
	must(t, s.SetPrice(ctx, llm.ProviderClaude, "fixture-set-only", "1", "2"))
	rows, err := s.Prices(ctx)
	must(t, err)
	var got []string
	for _, r := range rows {
		got = append(got, r.Provider+"/"+r.Model)
	}
	want := []string{"claude/" + claudeModel, "claude/fixture-set-only", "gemini/" + geminiModel}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("price list = %v, want %v", got, want)
	}
	if rows[0].Price != nil || rows[1].Price == nil {
		t.Errorf("prices = %+v", rows)
	}
}

func TestSetPriceRefusesWhatItCannotUse(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	for name, args := range map[string][4]string{
		"unknown provider": {"openai", "m", "1", "2"},
		"no model":         {"claude", " ", "1", "2"},
		"bad input":        {"claude", "m", "cheap", "2"},
		"bad output":       {"claude", "m", "1", "-2"},
	} {
		if err := s.SetPrice(ctx, args[0], args[1], args[2], args[3]); err == nil || !isInvalid(err) {
			t.Errorf("%s: error = %v, want ErrInvalid", name, err)
		}
	}
	if err := s.ClearPrice(ctx, "openai", "m"); !isInvalid(err) {
		t.Errorf("ClearPrice of an unknown provider: %v", err)
	}
}

type values map[string]string

func (v values) Value(_ context.Context, f settings.Field) (string, error) {
	if x, ok := v[f.Name]; ok {
		return x, nil
	}
	return "", settings.ErrUnset
}
func (values) Secret(context.Context, settings.Field) (string, error) { return "", settings.ErrUnset }

func TestConfiguredReadsEachTasksProviderAndModel(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		saved values
		want  []llmusage.ModelRef
	}{
		"both tasks": {
			values{"text_provider": "claude", "text_model": "a", "vision_provider": "gemini", "vision_model": "b"},
			[]llmusage.ModelRef{{Provider: "claude", Model: "a"}, {Provider: "gemini", Model: "b"}},
		},
		"the same model twice": {
			values{"text_provider": "claude", "text_model": "a", "vision_provider": "claude", "vision_model": "a"},
			[]llmusage.ModelRef{{Provider: "claude", Model: "a"}},
		},
		"a task with no model": {values{"text_provider": "claude", "vision_provider": "gemini", "vision_model": "b"}, []llmusage.ModelRef{{Provider: "gemini", Model: "b"}}},
		"nothing saved":        {values{}, nil},
		"an unknown provider":  {values{"text_provider": "other", "text_model": "a"}, nil},
	} {
		got, err := llmusage.Configured{Values: c.saved}.Configured(ctx)
		if err != nil || len(got) != len(c.want) {
			t.Errorf("%s: %v, %v, want %v", name, got, err, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: %v, want %v", name, got, c.want)
			}
		}
	}
}
