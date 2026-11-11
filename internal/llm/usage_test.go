package llm_test

import (
	"context"
	"testing"

	"github.com/kaecyra/gobbler/internal/llm"
)

func TestUsageReportCollectsWhatProvidersReport(t *testing.T) {
	ctx, report := llm.WithUsageReport(context.Background())
	if got := report.Usage(); got != (llm.Usage{}) {
		t.Fatalf("a new report holds %+v, want nothing", got)
	}
	llm.ReportUsage(ctx, llm.Usage{Input: 10, Output: 4})
	llm.ReportUsage(ctx, llm.Usage{Input: 5, Output: 1})
	if got := report.Usage(); got != (llm.Usage{Input: 15, Output: 5}) {
		t.Errorf("report = %+v, want the two added", got)
	}
}

func TestReportingWithNoReportOnTheContextIsHarmless(_ *testing.T) {
	llm.ReportUsage(context.Background(), llm.Usage{Input: 1})
}
