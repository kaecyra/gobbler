package parse

import (
	"testing"
	"time"
)

const (
	durTestHourSpan   = time.Hour
	durTestMinuteSpan = time.Minute
)

func TestDuration(t *testing.T) {
	tests := []struct {
		in         string
		lo, hi     time.Duration
		rng        bool
		confidence float64
	}{
		{"1 hr 30 min", 90 * durTestMinuteSpan, 90 * durTestMinuteSpan, false, 1},
		{"1 hour 30 minutes", 90 * durTestMinuteSpan, 90 * durTestMinuteSpan, false, 1},
		{"1 hour and 15 minutes", 75 * durTestMinuteSpan, 75 * durTestMinuteSpan, false, 1},
		{"90 minutes", 90 * durTestMinuteSpan, 90 * durTestMinuteSpan, false, 1},
		{"90 mins", 90 * durTestMinuteSpan, 90 * durTestMinuteSpan, false, 1},
		{"1-2 hours", durTestHourSpan, 2 * durTestHourSpan, true, 1},
		{"1 to 2 hours", durTestHourSpan, 2 * durTestHourSpan, true, 1},
		{"25–30 minutes", 25 * durTestMinuteSpan, 30 * durTestMinuteSpan, true, 1},
		{"1h30m", 90 * durTestMinuteSpan, 90 * durTestMinuteSpan, false, 1},
		{"45m", 45 * durTestMinuteSpan, 45 * durTestMinuteSpan, false, 1},
		{"1.5 hours", 90 * durTestMinuteSpan, 90 * durTestMinuteSpan, false, 1},
		{"1 1/2 hours", 90 * durTestMinuteSpan, 90 * durTestMinuteSpan, false, 1},
		{"1½ hours", 90 * durTestMinuteSpan, 90 * durTestMinuteSpan, false, 1},
		{"half an hour", 30 * durTestMinuteSpan, 30 * durTestMinuteSpan, false, 1},
		{"an hour", durTestHourSpan, durTestHourSpan, false, 1},
		{"45 seconds", 45 * time.Second, 45 * time.Second, false, 1},
		{"2 days", 48 * durTestHourSpan, 48 * durTestHourSpan, false, 1},
		{"  20 MIN  ", 20 * durTestMinuteSpan, 20 * durTestMinuteSpan, false, 1},
		{"about 10 minutes", 10 * durTestMinuteSpan, 10 * durTestMinuteSpan, false, 1},
		{"PT1H30M", 90 * durTestMinuteSpan, 90 * durTestMinuteSpan, false, 1},
		{"PT45M", 45 * durTestMinuteSpan, 45 * durTestMinuteSpan, false, 1},
		{"P1DT2H", 26 * durTestHourSpan, 26 * durTestHourSpan, false, 1},
		{"PT30S", 30 * time.Second, 30 * time.Second, false, 1},
		{"10 minutes plus chilling", 10 * durTestMinuteSpan, 10 * durTestMinuteSpan, false, durationPartialConfidence},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := Duration(tt.in)
			if !got.Found {
				t.Fatalf("Duration(%q) not found", tt.in)
			}
			if got.Min != tt.lo || got.Max != tt.hi || got.Range != tt.rng || got.Confidence != tt.confidence {
				t.Errorf("Duration(%q) = %v-%v range=%v conf=%v, want %v-%v range=%v conf=%v",
					tt.in, got.Min, got.Max, got.Range, got.Confidence, tt.lo, tt.hi, tt.rng, tt.confidence)
			}
		})
	}
}

func TestDurationAbsent(t *testing.T) {
	for _, in := range []string{"", "   ", "overnight", "soon", "P", "PT", "350 degrees", "2-1 hours", "1/0 hours", "hour"} {
		t.Run(in, func(t *testing.T) {
			got := Duration(in)
			if got.Found || got.Confidence != 0 || got.Min != 0 {
				t.Errorf("Duration(%q) = %+v, want absent", in, got)
			}
		})
	}
}

func TestFindDurations(t *testing.T) {
	text := "Bake for 25 to 30 minutes, then rest 5 min. Chill 1 hour 30 minutes or overnight. Serve with 2 eggs and 30m of patience."
	got := FindDurations(text)
	want := []struct {
		text   string
		lo, hi time.Duration
		rng    bool
	}{
		{"25 to 30 minutes", 25 * durTestMinuteSpan, 30 * durTestMinuteSpan, true},
		{"5 min", 5 * durTestMinuteSpan, 5 * durTestMinuteSpan, false},
		{"1 hour 30 minutes", 90 * durTestMinuteSpan, 90 * durTestMinuteSpan, false},
	}
	if len(got) != len(want) {
		t.Fatalf("FindDurations found %+v, want %d results", got, len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Text != w.text || g.Min != w.lo || g.Max != w.hi || g.Range != w.rng {
			t.Errorf("result %d = %+v, want %+v", i, g, w)
		}
		if text[g.Start:g.End] != g.Text {
			t.Errorf("result %d offsets %d:%d give %q, not %q", i, g.Start, g.End, text[g.Start:g.End], g.Text)
		}
	}
}

func TestFindDurationsSeparateTerms(t *testing.T) {
	got := FindDurations("Bake 10 minutes, 5 minutes more covered.")
	if len(got) != 2 || got[0].Min != 10*durTestMinuteSpan || got[1].Min != 5*durTestMinuteSpan {
		t.Errorf("FindDurations = %+v, want separate 10m and 5m", got)
	}
}

func TestFindDurationsNone(t *testing.T) {
	if got := FindDurations("Stir until smooth. Season with 2 tsp salt."); len(got) != 0 {
		t.Errorf("FindDurations = %+v, want none", got)
	}
}

func TestDeterministicDurations(t *testing.T) {
	a := FindDurations("Simmer 1 hr 15 min, then 10 minutes.")
	b := FindDurations("Simmer 1 hr 15 min, then 10 minutes.")
	if len(a) != len(b) {
		t.Fatal("results differ between runs")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("result %d differs between runs", i)
		}
	}
}
