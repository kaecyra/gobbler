package recipes

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe/display"
	"github.com/kaecyra/gobbler/internal/store"
)

// view is everything the templates show, already formatted.
type view struct {
	Query    string
	Selected []string
	Chips    []chip
	Rows     []row
	// Filtered is true when a query or tag narrowed the list, which is what
	// tells an empty library from a search with no match.
	Filtered bool
}

// chip is a tag that can be toggled. Href is the page with the tag added to the
// current filter, or removed when it is already selected.
type chip struct {
	Tag    string
	Count  int
	Active bool
	Href   string
}

type row struct {
	Href        string
	Name        string
	Description string
	Rating      string // "4 of 5", or empty when unrated
	Stars       string // decorative, hidden from assistive technology
	Tags        []string
	Times       []string // "Prep 10 min", "Cook 20 min", "Total 45 min"
}

func buildView(q string, selected []string, found []store.Summary, inUse []store.TagCount) view {
	selected = slices.DeleteFunc(slices.Clone(selected), func(t string) bool { return strings.TrimSpace(t) == "" })
	v := view{Query: q, Selected: selected, Filtered: q != "" || len(selected) > 0}
	for _, t := range inUse {
		active := slices.Contains(selected, t.Tag)
		var next []string
		if active {
			next = slices.DeleteFunc(slices.Clone(selected), func(s string) bool { return s == t.Tag })
		} else {
			next = append(slices.Clone(selected), t.Tag)
		}
		v.Chips = append(v.Chips, chip{Tag: t.Tag, Count: t.Recipes, Active: active, Href: listHref(q, next)})
	}
	for _, s := range found {
		v.Rows = append(v.Rows, rowFor(s))
	}
	return v
}

func rowFor(s store.Summary) row {
	r := row{Href: recipeHref(s.ID), Name: s.Name, Description: s.Description, Tags: s.Tags}
	if s.Rating != nil {
		r.Rating = strconv.Itoa(*s.Rating) + " of 5"
		r.Stars = strings.Repeat("★", *s.Rating) + strings.Repeat("☆", max(0, 5-*s.Rating))
	}
	for _, t := range []struct {
		label string
		d     time.Duration
	}{{"Prep", s.Prep}, {"Cook", s.Cook}, {"Total", s.Total}} {
		if text := display.FormatDuration(t.d); text != "" {
			r.Times = append(r.Times, t.label+" "+text)
		}
	}
	return r
}

// listHref is the list page for a query and tag filter: the URL that reproduces
// the results, used by chips and by the address bar.
func listHref(q string, tags []string) string {
	vals := url.Values{}
	if q != "" {
		vals.Set(paramText, q)
	}
	for _, t := range tags {
		vals.Add(paramTag, t)
	}
	if len(vals) == 0 {
		return Pattern
	}
	return Pattern + "?" + vals.Encode()
}

func itoa(n int) string { return strconv.Itoa(n) }

// boolAttr is the value of an ARIA true/false attribute.
func boolAttr(b bool) string { return strconv.FormatBool(b) }
