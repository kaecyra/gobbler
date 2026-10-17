package editor

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// mark is a flag the review screen puts on something it asks the user to
// check: the rules were less sure of it than the draft's threshold (Low), or
// the LLM supplied it (LLM). Both are shown as words, never colour alone
// (ADR-00006). A stored recipe's editor has none.
type mark struct{ Low, LLM bool }

const (
	markLow = "low"
	markLLM = "llm"
)

func parseMark(s string) mark {
	var m mark
	for _, w := range strings.Fields(s) {
		switch w {
		case markLow:
			m.Low = true
		case markLLM:
			m.LLM = true
		}
	}
	return m
}

func (m mark) zero() bool { return !m.Low && !m.LLM }

// String is the hidden field's value, and the CSS class of the flagged item.
func (m mark) String() string {
	var w []string
	if m.Low {
		w = append(w, markLow)
	}
	if m.LLM {
		w = append(w, markLLM)
	}
	return strings.Join(w, " ")
}

// markedFields are the metadata fields that can carry a mark.
var markedFields = []string{"name", "author", "description", "yield", "prep", "cook", "total"}

// markName is the name of the hidden field that carries a metadata mark.
func markName(field string) string { return "mark." + field }

func parseMarks(f url.Values) map[string]mark {
	out := map[string]mark{}
	for _, field := range markedFields {
		if m := parseMark(f.Get(markName(field))); !m.zero() {
			out[field] = m
		}
	}
	return out
}

func (d draft) mark(field string) mark { return d.Marks[field] }

// graphMark says where a step's passive flag and dependencies came from on the
// review screen: a rule matched the step's text (Cue), or the LLM supplied the
// reading (LLM). Read is the graph as it was read, so toIngest can tell a step
// whose graph the user left alone, which keeps its provenance, from one they
// changed, which is then theirs. It rides in a hidden field so it follows the
// step when it moves. A stored recipe's editor has none.
type graphMark struct {
	Cue, LLM bool
	Read     string
}

func (g graphMark) zero() bool { return !g.Cue && !g.LLM }

// graphReading is the comparable form of a step's passive flag and dependency
// ids, independent of the order the dependencies were ticked in.
func graphReading(passive bool, deps []string) string {
	ids := slices.Clone(deps)
	slices.Sort(ids)
	return fmt.Sprintf("%t:%s", passive, strings.Join(ids, ","))
}

// String is the hidden field's value: the flags, a semicolon, the reading.
func (g graphMark) String() string {
	var w []string
	if g.Cue {
		w = append(w, "cue")
	}
	if g.LLM {
		w = append(w, markLLM)
	}
	return strings.Join(w, " ") + ";" + g.Read
}

func parseGraphMark(s string) graphMark {
	flags, read, _ := strings.Cut(s, ";")
	g := graphMark{Read: read}
	for _, w := range strings.Fields(flags) {
		switch w {
		case "cue":
			g.Cue = true
		case markLLM:
			g.LLM = true
		}
	}
	return g
}

// current is g if the user left the step's graph as it was read, else nothing:
// a changed graph is the user's reading, not the rules' or the LLM's.
func (g graphMark) current(passive bool, deps []string) graphMark {
	if g.Read != graphReading(passive, deps) {
		return graphMark{}
	}
	return g
}
