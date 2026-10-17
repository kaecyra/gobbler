package ribbon

import (
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
)

func step(mins int, passive bool, kind recipe.StageKind) recipe.Step {
	return recipe.Step{Duration: time.Duration(mins) * time.Minute, Passive: passive, Kind: kind}
}

func TestFamilyOf(t *testing.T) {
	tests := map[string]struct {
		kind    recipe.StageKind
		passive bool
		want    Family
	}{
		"prep":                   {recipe.StagePrep, false, FamilyActive},
		"unkinded active":        {"", false, FamilyActive},
		"kinded active is coral": {recipe.StageSimmer, false, FamilyActive},
		"rise":                   {recipe.StageRise, true, FamilySky},
		"rest":                   {recipe.StageRest, true, FamilySky},
		"steep":                  {recipe.StageSteep, true, FamilyMint},
		"marinate":               {recipe.StageMarinate, true, FamilyMint},
		"chill":                  {recipe.StageChill, true, FamilyIce},
		"bake":                   {recipe.StageBake, true, FamilyApricot},
		"roast":                  {recipe.StageRoast, true, FamilyApricot},
		"simmer":                 {recipe.StageSimmer, true, FamilyButter},
		"braise":                 {recipe.StageBraise, true, FamilyButter},
		"cook":                   {recipe.StageCook, true, FamilyButter},
		"unkinded passive":       {"", true, FamilySlate},
		"prep marked passive":    {recipe.StagePrep, true, FamilyActive},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := familyOf(tc.kind, tc.passive); got != tc.want {
				t.Fatalf("familyOf(%q, %v) = %q, want %q", tc.kind, tc.passive, got, tc.want)
			}
		})
	}
}

func TestBuildSkipsStepsWithoutDuration(t *testing.T) {
	r := Build([]recipe.Step{step(0, false, ""), step(10, false, recipe.StagePrep), {Passive: true}})
	if len(r.Full) != 1 || r.Full[0].Minutes != 10 {
		t.Fatalf("Full = %+v, want only the 10 minute step", r.Full)
	}
}

func TestBuildEmpty(t *testing.T) {
	if r := Build(nil); !r.Empty() || r.Total != 0 {
		t.Fatalf("Build(nil) = %+v, want empty", r)
	}
	if r := Build([]recipe.Step{step(0, false, "")}); !r.Empty() {
		t.Fatalf("untimed steps made a ribbon: %+v", r)
	}
}

func TestBuildFullOneSegmentPerStep(t *testing.T) {
	r := Build([]recipe.Step{
		step(20, false, recipe.StagePrep),
		step(30, false, ""),
		step(180, true, recipe.StageBraise),
		step(15, false, ""),
	})
	if len(r.Full) != 4 {
		t.Fatalf("Full has %d segments, want one per step: %+v", len(r.Full), r.Full)
	}
	wantFam := []Family{FamilyActive, FamilyActive, FamilyButter, FamilyActive}
	for i, s := range r.Full {
		if s.Family != wantFam[i] {
			t.Errorf("Full[%d].Family = %q, want %q", i, s.Family, wantFam[i])
		}
	}
	if r.Total != 245*time.Minute {
		t.Errorf("Total = %v, want 245m", r.Total)
	}
}

func TestBuildMergesNeighboursOfTheSameKind(t *testing.T) {
	r := Build([]recipe.Step{
		step(20, false, recipe.StagePrep),
		step(30, false, ""),
		step(180, true, recipe.StageBraise),
		step(60, true, recipe.StageRest),
		step(15, false, ""),
	})
	if len(r.Merged) != 3 {
		t.Fatalf("Merged = %+v, want active, waiting, active", r.Merged)
	}
	m := r.Merged[0]
	if !m.Active || m.Minutes != 50 || m.Family != FamilyActive || len(m.Parts) != 2 {
		t.Errorf("Merged[0] = %+v, want 50 active minutes in two parts", m)
	}
	w := r.Merged[1]
	if w.Active || w.Minutes != 240 {
		t.Errorf("Merged[1] = %+v, want 240 waiting minutes", w)
	}
	if w.Family != FamilyButter {
		t.Errorf("merged waiting segment takes the family of its longest part: got %q, want butter", w.Family)
	}
	if w.Background == "" {
		t.Error("a segment of two families needs a gradient background")
	}
	if r.Merged[0].Background != "" {
		t.Errorf("one-family segment must use its class fill, got %q", r.Merged[0].Background)
	}
}

func TestBuildFlexGrowsWithSquareRootOfDuration(t *testing.T) {
	r := Build([]recipe.Step{step(4, false, ""), step(16, true, recipe.StageRise)})
	if r.Full[0].Flex != "2.00" || r.Full[1].Flex != "4.00" {
		t.Fatalf("Flex = %q, %q; want 2.00 and 4.00", r.Full[0].Flex, r.Full[1].Flex)
	}
}

func TestBuildDeduplicatesWhenNothingMerges(t *testing.T) {
	r := Build([]recipe.Step{step(10, false, ""), step(20, true, recipe.StageRise), step(5, false, "")})
	if r.Merges() {
		t.Fatalf("alternating kinds do not merge, but Merges() is true")
	}
}

func TestBuildMergesReportsMerging(t *testing.T) {
	r := Build([]recipe.Step{step(10, false, ""), step(5, false, "")})
	if !r.Merges() {
		t.Fatal("two active neighbours merge, but Merges() is false")
	}
}

func TestMinutesRoundedAndAtLeastOne(t *testing.T) {
	r := Build([]recipe.Step{{Duration: 20 * time.Second}, {Duration: 89 * time.Second, Passive: true}})
	if r.Full[0].Minutes != 1 || r.Full[1].Minutes != 1 {
		t.Fatalf("Minutes = %d, %d; want 1, 1", r.Full[0].Minutes, r.Full[1].Minutes)
	}
}

func TestShort(t *testing.T) {
	tests := map[int]string{1: "1m", 45: "45m", 60: "1h", 90: "1h 30m", 245: "4h 5m"}
	for m, want := range tests {
		if got := short(m); got != want {
			t.Errorf("short(%d) = %q, want %q", m, got, want)
		}
	}
}

func TestLong(t *testing.T) {
	tests := map[int]string{1: "1 minute", 45: "45 minutes", 60: "1 hour", 90: "1 hour 30 minutes", 120: "2 hours", 121: "2 hours 1 minute"}
	for m, want := range tests {
		if got := long(m); got != want {
			t.Errorf("long(%d) = %q, want %q", m, got, want)
		}
	}
}

func TestLabelAndTitle(t *testing.T) {
	r := Build([]recipe.Step{
		step(20, false, recipe.StagePrep),
		step(30, false, ""),
		step(180, true, recipe.StageBraise),
		step(10, true, ""),
	})
	want := "20 minutes prep, then 30 minutes hands-on, then 3 hours braise, then 10 minutes waiting"
	if r.Label != want {
		t.Errorf("Label = %q, want %q", r.Label, want)
	}
	if got := r.Merged[0].Title; got != "20m prep · 30m hands-on" {
		t.Errorf("Merged[0].Title = %q", got)
	}
	if got := r.Full[2].Title; got != "3h braise" {
		t.Errorf("Full[2].Title = %q", got)
	}
}
