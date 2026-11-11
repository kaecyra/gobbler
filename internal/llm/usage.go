package llm

import (
	"context"
	"sync"
)

// Usage is the tokens one call used, as the provider reports them.
type Usage struct {
	Input, Output int64
}

// UsageReport collects the Usage a provider reports while a call runs. The
// caller of a provider makes one with WithUsageReport, passes the returned
// context to the call, and reads the report afterwards (ADR-00058): the
// Provider interface returns no token counts (ADR-00007), and the providers
// already know them for their log line.
type UsageReport struct {
	mu sync.Mutex
	u  Usage
}

// Usage returns what the call reported: zero when it reported nothing, as a
// failed call does.
func (r *UsageReport) Usage() Usage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.u
}

type usageKey struct{}

// WithUsageReport returns ctx carrying a new report, and the report.
func WithUsageReport(ctx context.Context) (context.Context, *UsageReport) {
	r := &UsageReport{}
	return context.WithValue(ctx, usageKey{}, r), r
}

// ReportUsage adds u to the report ctx carries. A provider calls it once a
// call has succeeded; with no report on ctx it does nothing.
func ReportUsage(ctx context.Context, u Usage) {
	r, ok := ctx.Value(usageKey{}).(*UsageReport)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.u.Input += u.Input
	r.u.Output += u.Output
}
