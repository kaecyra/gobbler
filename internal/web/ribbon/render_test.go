package ribbon

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/kaecyra/gobbler/internal/recipe"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var b bytes.Buffer
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func TestCardRendersNothingWithoutTimedSteps(t *testing.T) {
	if got := render(t, Card(Build(nil))); got != "" {
		t.Fatalf("empty ribbon rendered %q", got)
	}
	if got := render(t, Large(Build([]recipe.Step{{}}))); got != "" {
		t.Fatalf("untimed ribbon rendered %q", got)
	}
}

func TestRibbonMarkup(t *testing.T) {
	r := Build([]recipe.Step{
		step(20, false, recipe.StagePrep),
		step(30, false, ""),
		step(180, true, recipe.StageBraise),
	})
	got := render(t, Card(r))
	for _, want := range []string{
		`role="img"`,
		`aria-label="20 minutes prep, then 30 minutes hands-on, then 3 hours braise"`,
		`data-level="0"`,
		`rib-full`, `rib-merged`,
		`rib-seg rib-butter`,
		`flex-grow:`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("markup missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(render(t, Large(r)), "rib-big") {
		t.Error("Large lacks rib-big")
	}
}

func TestRibbonOmitsMergedLevelWhenNothingMerges(t *testing.T) {
	got := render(t, Card(Build([]recipe.Step{step(10, false, ""), step(20, true, recipe.StageRise)})))
	if strings.Contains(got, "rib-merged") {
		t.Errorf("duplicate merged level printed:\n%s", got)
	}
}
