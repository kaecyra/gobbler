// Package recipepath builds and reads the URL of a recipe's view page
// (ADR-00047): /recipes/{id}-{slug}. The id is the only lookup key; the slug is
// decoration derived from the title when the link is made, never stored and
// never looked up. The sub-pages of a recipe (edit, journal, cook, history,
// clone, picture) keep a plain numeric id and use Base.
package recipepath

import (
	"errors"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Prefix is the path every recipe URL starts with.
const Prefix = "/recipes/"

// maxSlug is the longest slug, in bytes; a longer title is cut at a '-'.
const maxSlug = 60

// ErrBadSegment is returned for a path segment that cannot name a recipe.
var ErrBadSegment = errors.New("not a recipe path segment")

// Base is the plain-id path of a recipe, the base of its sub-pages:
// /recipes/42/edit is Base(42) + "/edit".
func Base(id int64) string { return Prefix + strconv.FormatInt(id, 10) }

// View is the canonical path of the recipe's view page for a recipe with the
// given title. A title with no ASCII letters or digits gives the plain
// Base(id).
func View(id int64, title string) string { return Prefix + Segment(id, title) }

// Segment is the canonical last path segment of the recipe's view page.
func Segment(id int64, title string) string {
	s := strconv.FormatInt(id, 10)
	if slug := Slug(title); slug != "" {
		return s + "-" + slug
	}
	return s
}

// ParseSegment reads the recipe id from the view page's path segment: leading
// decimal digits that make a positive int64, then either the end of the
// segment or '-' followed by anything. Everything else is ErrBadSegment.
func ParseSegment(seg string) (int64, error) {
	n := 0
	for n < len(seg) && seg[n] >= '0' && seg[n] <= '9' {
		n++
	}
	if n == 0 || (n < len(seg) && seg[n] != '-') {
		return 0, ErrBadSegment
	}
	id, err := strconv.ParseInt(seg[:n], 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrBadSegment
	}
	return id, nil
}

// Slug turns a title into lowercase ASCII words joined by single '-'. Letters
// with diacritics lose the mark ("Crème" gives "creme"); other non-ASCII
// characters count as separators. The result is at most 60 bytes, cut at a
// '-' boundary, and is empty when the title has no ASCII letters or digits.
func Slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFD.String(title) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue // a combining mark belongs to the letter before it
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r >= 'A' && r <= 'Z':
			r += 'a' - 'A'
		default:
			dash = b.Len() > 0
			continue
		}
		if dash {
			b.WriteByte('-')
			dash = false
		}
		b.WriteRune(r)
	}
	s := b.String() // ASCII only, so bytes are characters
	if len(s) > maxSlug {
		cut := s[:maxSlug]
		if s[maxSlug] != '-' {
			if i := strings.LastIndexByte(cut, '-'); i > 0 {
				cut = cut[:i]
			}
		}
		s = cut
	}
	return strings.Trim(s, "-")
}
