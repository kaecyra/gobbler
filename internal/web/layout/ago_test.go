package layout

import (
	"testing"
	"time"
)

func TestAgoInWholeUnits(t *testing.T) {
	today := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }
	for _, tc := range []struct{ got, want string }{
		{Ago(today, today), "today"},
		{Ago(d(2026, 10, 16), today), "yesterday"},
		{Ago(d(2026, 10, 10), today), "7 days ago"},
		{Ago(d(2026, 9, 1), today), "6 weeks ago"},
		{Ago(d(2026, 3, 2), today), "7 months ago"},
		{Ago(d(2025, 8, 1), today), "a year ago"},
		{Ago(d(2023, 1, 1), today), "3 years ago"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

// A made-it date is a plain calendar date (stored and parsed as UTC midnight);
// "today" is the calendar date in now's own zone. The difference must not
// depend on the hour or on how far now's zone sits from UTC.
func TestAgoCountsCalendarDays(t *testing.T) {
	toronto := time.FixedZone("EDT", -4*3600)
	tokyo := time.FixedZone("JST", 9*3600)
	made := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		now  time.Time
	}{
		{"toronto 00:30", time.Date(2026, 10, 18, 0, 30, 0, 0, toronto)},
		{"toronto 12:00", time.Date(2026, 10, 18, 12, 0, 0, 0, toronto)},
		{"toronto 23:30 (already the 19th in UTC)", time.Date(2026, 10, 18, 23, 30, 0, 0, toronto)},
		{"utc 00:30", time.Date(2026, 10, 18, 0, 30, 0, 0, time.UTC)},
		{"utc 23:30", time.Date(2026, 10, 18, 23, 30, 0, 0, time.UTC)},
		{"tokyo 00:30 (still the 17th in UTC)", time.Date(2026, 10, 18, 0, 30, 0, 0, tokyo)},
		{"tokyo 23:30", time.Date(2026, 10, 18, 23, 30, 0, 0, tokyo)},
	} {
		if got := Ago(made, tc.now); got != "2 days ago" {
			t.Errorf("%s: ago = %q, want %q", tc.name, got, "2 days ago")
		}
	}
	if got := Ago(made, time.Date(2026, 10, 16, 23, 59, 0, 0, toronto)); got != "today" {
		t.Errorf("same calendar day late evening: %q, want today", got)
	}
	if got := Ago(made, time.Date(2026, 10, 17, 0, 1, 0, 0, tokyo)); got != "yesterday" {
		t.Errorf("just past midnight: %q, want yesterday", got)
	}
}
