package layout

import (
	"strconv"
	"time"
)

// CalendarDays counts the calendar days from t to now. t is read as a plain
// calendar date, its own date in its own zone; today is now's date in now's
// zone. Both are reduced to a date before subtracting, so the hour and the
// distance of now's zone from UTC never change the answer. A timestamp, rather
// than a date, is put in now's zone first (t.In(now.Location())).
func CalendarDays(t, now time.Time) int {
	date := func(t time.Time) time.Time {
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	return int(date(now).Sub(date(t)).Hours() / 24)
}

// Ago says how long before now a date was, in the largest whole unit, counted
// in calendar days (CalendarDays): the library's made-it dates and the import
// page's waiting drafts say it the same way.
func Ago(t, now time.Time) string {
	days := CalendarDays(t, now)
	switch {
	case days < 1:
		return "today"
	case days == 1:
		return "yesterday"
	case days < 14:
		return strconv.Itoa(days) + " days ago"
	case days < 60:
		return strconv.Itoa(days/7) + " weeks ago"
	case days < 365:
		return strconv.Itoa(days/30) + " months ago"
	case days < 2*365:
		return "a year ago"
	}
	return strconv.Itoa(days/365) + " years ago"
}
