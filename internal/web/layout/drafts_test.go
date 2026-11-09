package layout

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/household"
)

// renderNav renders the page shell with ctx and returns it.
func renderNav(ctx context.Context, t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	ctx = household.WithContext(ctx, household.Household{ID: 1, Name: "Probe"})
	if err := Page("Probe").Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// ADR-00049: the Add recipe entry carries the count of drafts waiting for
// review as a badge, in its accessible name too, and no badge at zero. Only
// that entry asks for the count.
func TestNavShowsTheWaitingDraftsBadge(t *testing.T) {
	calls := 0
	count := func(n int) WaitingDrafts {
		return func(context.Context) int { calls++; return n }
	}

	got := renderNav(WithWaitingDrafts(context.Background(), count(3)), t)
	if calls != 1 {
		t.Errorf("counted %d times, want once", calls)
	}
	for _, want := range []string{
		`<span class="tab-count" aria-hidden="true" data-testid="waiting-drafts">3</span>`,
		`aria-label="Add recipe, 3 drafts waiting for review"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("nav missing %q", want)
		}
	}
	if !strings.Contains(renderNav(WithWaitingDrafts(context.Background(), count(1)), t), `aria-label="Add recipe, 1 draft waiting for review"`) {
		t.Error("one draft is not named in the singular")
	}

	for name, ctx := range map[string]context.Context{
		"zero":     WithWaitingDrafts(context.Background(), count(0)),
		"no count": context.Background(),
	} {
		got := renderNav(ctx, t)
		if strings.Contains(got, "tab-count") || !strings.Contains(got, `aria-label="Add recipe"`) {
			t.Errorf("%s: the badge is shown, or the label changed", name)
		}
	}
}
