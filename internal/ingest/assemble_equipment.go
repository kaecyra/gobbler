package ingest

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// maxEquipmentNameRunes bounds a name read from a source or an LLM answer. A
// longer string is a sentence, not a piece of equipment.
const maxEquipmentNameRunes = 60

// equipmentName is the form of a tool name the catalog stores: the text before
// any qualifier ("Mandoline slicer (optional)", "pan, 9 inch"), normalised. It
// is empty for a name that is blank or too long to be equipment.
func equipmentName(s string) string {
	if i := strings.IndexAny(s, "(,"); i >= 0 {
		s = s[:i]
	}
	name := normalizeName(s)
	if utf8.RuneCountInString(name) > maxEquipmentNameRunes {
		return ""
	}
	return name
}

// equipmentSet collects a draft's equipment once each, in the order added.
type equipmentSet struct {
	items []Equipment
	seen  map[string]bool
}

func newEquipmentSet() *equipmentSet { return &equipmentSet{seen: map[string]bool{}} }

// add records e unless the same catalog entry, or the same unmatched name, is
// already present.
func (s *equipmentSet) add(e Equipment) {
	key := "n:" + e.Name
	if e.Equipment != 0 {
		key = "i:" + strconv.FormatInt(int64(e.Equipment), 10)
	}
	if s.seen[key] {
		return
	}
	s.seen[key] = true
	s.items = append(s.items, e)
}

// named adds the equipment called name: the catalog's entry when it has one,
// else a proposal. A name that is not equipment-shaped is skipped.
func (a *assembler) named(set *equipmentSet, raw string, fromLLM bool) {
	name := equipmentName(raw)
	if name == "" {
		return
	}
	if eq, ok := a.rs.matchEquipment(name); ok {
		set.add(Equipment{Name: eq.Name, Equipment: recipe.EquipmentID(eq.ID), FromLLM: fromLLM})
		return
	}
	set.add(Equipment{Name: name, FromLLM: fromLLM})
}

// scanEquipment adds the catalog equipment named in the draft's ingredient
// lines and steps, which are read in reading order: each component's lines,
// then its steps. Only whole words match, a plural matches its singular, and
// where two names overlap the longer one wins ("stand mixer" over "mixer").
func (a *assembler) scanEquipment(set *equipmentSet) {
	all := a.rs.catalogEquipment()
	if len(all) == 0 {
		return
	}
	terms := equipmentTerms(all)
	for _, c := range a.d.Components {
		for _, l := range c.Lines {
			addMatches(set, findEquipment(l.Raw, terms), false)
		}
		for _, st := range c.Steps {
			addMatches(set, findEquipment(st.Text, terms), false)
		}
	}
}

func addMatches(set *equipmentSet, ms []equipmentMatch, fromLLM bool) {
	for _, m := range ms {
		set.add(Equipment{Name: m.eq.Name, Equipment: recipe.EquipmentID(m.eq.ID), FromLLM: fromLLM})
	}
}

type equipmentTerm struct {
	eq catalog.Equipment
	re *regexp.Regexp
}

type equipmentMatch struct {
	eq         catalog.Equipment
	start, end int
}

// equipmentTerms compiles every name and alias. A trailing "s" or "es" is
// optional, so "wire racks" matches "wire rack".
func equipmentTerms(all []catalog.Equipment) []equipmentTerm {
	var out []equipmentTerm
	for _, eq := range all {
		for _, t := range append([]string{eq.Name}, eq.Aliases...) {
			t = normalizeName(t)
			if t == "" {
				continue
			}
			out = append(out, equipmentTerm{eq: eq, re: regexp.MustCompile(`(?i)` + regexp.QuoteMeta(t) + `(?:e?s)?`)})
		}
	}
	return out
}

// findEquipment returns the equipment named in text, in order of appearance.
func findEquipment(text string, terms []equipmentTerm) []equipmentMatch {
	var found []equipmentMatch
	for _, t := range terms {
		for _, loc := range t.re.FindAllStringIndex(text, -1) {
			if wordEdge(text, loc[0], loc[1]) {
				found = append(found, equipmentMatch{eq: t.eq, start: loc[0], end: loc[1]})
			}
		}
	}
	// Earliest first; at one position the longest; then by id, so the result
	// does not depend on the order the catalog listed equipment.
	sort.Slice(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.start != b.start {
			return a.start < b.start
		}
		if a.end != b.end {
			return a.end > b.end
		}
		return a.eq.ID < b.eq.ID
	})
	var out []equipmentMatch
	last := 0
	for _, m := range found {
		if m.start < last {
			continue
		}
		out = append(out, m)
		last = m.end
	}
	return out
}

// wordEdge reports whether text[start:end] is a whole word or phrase.
func wordEdge(text string, start, end int) bool {
	if r, _ := utf8.DecodeLastRuneInString(text[:start]); start > 0 && isWordRune(r) {
		return false
	}
	if r, _ := utf8.DecodeRuneInString(text[end:]); end < len(text) && isWordRune(r) {
		return false
	}
	return true
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
