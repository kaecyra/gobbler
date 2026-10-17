package editor

import (
	"net/url"
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
