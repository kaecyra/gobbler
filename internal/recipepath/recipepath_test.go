package recipepath_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/recipepath"
)

func TestSlug(t *testing.T) {
	long := strings.Repeat("abcdefghi ", 10)
	for _, c := range []struct{ name, title, want string }{
		{"plain", "Chicken Curry", "chicken-curry"},
		{"runs collapse", "  Mac  &  Cheese!!  ", "mac-cheese"},
		{"apostrophe splits", "Grandma's Pie", "grandma-s-pie"},
		{"digits", "5-Minute Salsa", "5-minute-salsa"},
		{"diacritics stripped", "Crème Brûlée à l'orange", "creme-brulee-a-l-orange"},
		{"decomposed input", "Café au lait", "cafe-au-lait"},
		{"non-latin dropped", "炒飯 Fried Rice", "fried-rice"},
		{"only non-ascii", "炒飯", ""},
		{"only punctuation", "!!!", ""},
		{"empty", "", ""},
		{"cut at word boundary", long, strings.TrimSuffix(strings.Repeat("abcdefghi-", 6), "-")},
		{"cut falls on a separator", strings.Repeat("a", 60) + " bbb", strings.Repeat("a", 60)},
		{"one long word", strings.Repeat("x", 80), strings.Repeat("x", 60)},
		{"fits", strings.Repeat("a", 30) + " " + strings.Repeat("b", 29), strings.Repeat("a", 30) + "-" + strings.Repeat("b", 29)},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := recipepath.Slug(c.title)
			if got != c.want {
				t.Errorf("Slug(%q) = %q, want %q", c.title, got, c.want)
			}
			if len(got) > 60 || strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") || strings.Contains(got, "--") {
				t.Errorf("Slug(%q) = %q breaks the shape rules", c.title, got)
			}
		})
	}
}

func TestViewAndBase(t *testing.T) {
	for _, c := range []struct {
		id    int64
		title string
		want  string
	}{
		{42, "Chicken Curry", "/recipes/42-chicken-curry"},
		{7, "", "/recipes/7"},
		{7, "炒飯", "/recipes/7"},
	} {
		if got := recipepath.View(c.id, c.title); got != c.want {
			t.Errorf("View(%d, %q) = %q, want %q", c.id, c.title, got, c.want)
		}
	}
	if got := recipepath.Base(42); got != "/recipes/42" {
		t.Errorf("Base(42) = %q", got)
	}
}

func TestParseSegment(t *testing.T) {
	for _, c := range []struct {
		seg  string
		want int64
		ok   bool
	}{
		{"42", 42, true},
		{"42-chicken-curry", 42, true},
		{"42-", 42, true},
		{"42--x", 42, true},
		{"42-炒飯", 42, true},
		{"007-x", 7, true},
		{"9223372036854775807", 9223372036854775807, true},
		{"", 0, false},
		{"abc", 0, false},
		{"-42", 0, false},
		{"42abc", 0, false},
		{"42_x", 0, false},
		{"0", 0, false},
		{"0-x", 0, false},
		{"9223372036854775808", 0, false},
		{"99999999999999999999-x", 0, false},
		{"４２", 0, false}, // full-width digits are not decimal digits here
	} {
		got, err := recipepath.ParseSegment(c.seg)
		if c.ok != (err == nil) || got != c.want {
			t.Errorf("ParseSegment(%q) = %d, %v; want %d ok=%v", c.seg, got, err, c.want, c.ok)
		}
		if !c.ok && !errors.Is(err, recipepath.ErrBadSegment) {
			t.Errorf("ParseSegment(%q) error = %v, want ErrBadSegment", c.seg, err)
		}
	}
}
