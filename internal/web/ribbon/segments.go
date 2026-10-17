package ribbon

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// Family is a colour family. Each has --st-<family>-b/-i/-a tokens in app.css,
// except FamilySlate, which uses the --slate token.
type Family string

// The colour families.
const (
	FamilyActive  Family = "active"
	FamilySky     Family = "sky"
	FamilyMint    Family = "mint"
	FamilyIce     Family = "ice"
	FamilyApricot Family = "apricot"
	FamilyButter  Family = "butter"
	FamilySlate   Family = "slate"
)

// familyOf maps a step to its colour family (docs/ui/design.md, Time ribbon).
// Hands-on time is always coral, so a step that is not passive is FamilyActive
// whatever its kind. Prep is coral too, even if marked passive. An unkinded
// passive step is slate.
func familyOf(kind recipe.StageKind, passive bool) Family {
	if !passive {
		return FamilyActive
	}
	switch kind {
	case recipe.StagePrep:
		return FamilyActive
	case recipe.StageRise, recipe.StageRest:
		return FamilySky
	case recipe.StageSteep, recipe.StageMarinate:
		return FamilyMint
	case recipe.StageChill:
		return FamilyIce
	case recipe.StageBake, recipe.StageRoast:
		return FamilyApricot
	case recipe.StageSimmer, recipe.StageBraise, recipe.StageCook:
		return FamilyButter
	}
	return FamilySlate
}

// Part is one step's share of a segment.
type Part struct {
	Family  Family
	Kind    string // the stage kind, or "hands-on" / "waiting" when unset
	Minutes int
}

// Segment is one block of the ribbon.
type Segment struct {
	Family  Family
	Active  bool // coral family: shows a hand; otherwise a clock
	Minutes int
	Parts   []Part
	// Flex is the flex-grow value: the square root of the minutes.
	Flex string
	// Text is the short time shown in the block, such as "1h 30m".
	Text string
	// Title is the tooltip: each part's time and kind.
	Title string
	// Background is a CSS gradient when the parts span several families, and
	// empty when the family class already paints the whole block.
	Background string
}

// Ribbon is the computed ribbon for a recipe.
type Ribbon struct {
	// Full has one segment per timed step.
	Full []Segment
	// Merged joins neighbours that are both hands-on or both waiting.
	Merged []Segment
	// Total is the summed duration of the timed steps.
	Total time.Duration
	// Label is the screen-reader description of the whole ribbon.
	Label string
}

// Empty reports whether there is nothing to draw.
func (r Ribbon) Empty() bool { return len(r.Full) == 0 }

// Merges reports whether the merged level differs from the full one.
func (r Ribbon) Merges() bool { return len(r.Merged) < len(r.Full) }

// Build computes the ribbon from a recipe's steps in order. Steps without a
// duration are left out: a ribbon shows time, and an unknown time is not zero.
func Build(steps []recipe.Step) Ribbon {
	var r Ribbon
	var labels []string
	for _, s := range steps {
		if s.Duration <= 0 {
			continue
		}
		fam := familyOf(s.Kind, s.Passive)
		p := Part{Family: fam, Kind: kindName(s), Minutes: minutes(s.Duration)}
		r.Full = append(r.Full, newSegment(fam == FamilyActive, []Part{p}))
		r.Total += s.Duration
		labels = append(labels, long(p.Minutes)+" "+p.Kind)
	}
	r.Merged = merge(r.Full)
	r.Label = strings.Join(labels, ", then ")
	return r
}

func kindName(s recipe.Step) string {
	switch {
	case s.Kind != "":
		return string(s.Kind)
	case s.Passive:
		return "waiting"
	}
	return "hands-on"
}

// minutes rounds to whole minutes, never below one: a timed step shows a time.
func minutes(d time.Duration) int {
	return max(1, int(math.Round(d.Minutes())))
}

func merge(segs []Segment) []Segment {
	var out []Segment
	for _, s := range segs {
		if n := len(out); n > 0 && out[n-1].Active == s.Active {
			out[n-1] = newSegment(s.Active, slices.Concat(out[n-1].Parts, s.Parts))
			continue
		}
		out = append(out, s)
	}
	return out
}

func newSegment(active bool, parts []Part) Segment {
	s := Segment{Active: active, Parts: parts}
	longest := parts[0]
	titles := make([]string, len(parts))
	for i, p := range parts {
		s.Minutes += p.Minutes
		if p.Minutes > longest.Minutes {
			longest = p
		}
		titles[i] = short(p.Minutes) + " " + p.Kind
	}
	s.Family = longest.Family
	s.Flex = fmt.Sprintf("%.2f", math.Sqrt(float64(s.Minutes)))
	s.Text = short(s.Minutes)
	s.Title = strings.Join(titles, " · ")
	s.Background = gradient(parts, s.Minutes)
	return s
}

// gradient paints each part in its own family, proportional to its time. It is
// empty when every part has the same family.
func gradient(parts []Part, total int) string {
	mixed := false
	for _, p := range parts {
		mixed = mixed || p.Family != parts[0].Family
	}
	if !mixed {
		return ""
	}
	stops := make([]string, len(parts))
	at := 0.0
	for i, p := range parts {
		from := at
		at += float64(p.Minutes) / float64(total) * 100
		stops[i] = fmt.Sprintf("%s %.1f%% %.1f%%", fill(p.Family), from, at)
	}
	return "linear-gradient(90deg," + strings.Join(stops, ",") + ")"
}

// fill is the translucent fill of a family, from its tokens.
func fill(f Family) string {
	if f == FamilySlate {
		return "color-mix(in srgb,var(--slate) 22%,transparent)"
	}
	return fmt.Sprintf("color-mix(in srgb,var(--st-%[1]s-b) var(--st-%[1]s-a),transparent)", f)
}

// short is the compact time: "45m", "1h", "1h 30m".
func short(m int) string {
	switch {
	case m < 60:
		return fmt.Sprintf("%dm", m)
	case m%60 == 0:
		return fmt.Sprintf("%dh", m/60)
	}
	return fmt.Sprintf("%dh %dm", m/60, m%60)
}

// long is the spoken time: "1 hour 30 minutes".
func long(m int) string {
	unit := func(n int, one string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s", one)
		}
		return fmt.Sprintf("%d %ss", n, one)
	}
	switch {
	case m < 60:
		return unit(m, "minute")
	case m%60 == 0:
		return unit(m/60, "hour")
	}
	return unit(m/60, "hour") + " " + unit(m%60, "minute")
}
