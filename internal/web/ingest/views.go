package ingest

import (
	"fmt"
	"strings"

	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
)

func percent(f float64) string { return fmt.Sprintf("%.0f%%", f*100) }

// issueText turns an issue code ("no_steps") into words.
func issueText(code string) string {
	s := strings.ReplaceAll(code, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func sourceText(d ingestcore.Draft) string {
	if d.Source.URL != "" {
		return d.Source.URL
	}
	return "pasted text"
}

// How the source toggle is set when the form is drawn.
const (
	modeLink = "link"
	modeText = "text"
	// modeBoth leaves the toggle unset so both fields show, as when a
	// submission gave a URL and pasted text.
	modeBoth = "both"
)

// mode is the toggle's setting for this form: from what was submitted, else
// from the requested source (?source=text), else the link field.
func (v formView) mode() string {
	switch {
	case v.URL != "" && strings.TrimSpace(v.Text) != "":
		return modeBoth
	case strings.TrimSpace(v.Text) != "", v.Source == modeText:
		return modeText
	}
	return modeLink
}

// srcRow is one row of the review screen's source box.
type srcRow struct {
	Text    string
	Heading bool
	Low     bool
}

// sourceRows lists the ingredient lines as the source wrote them, under their
// component's name. A line the rules were unsure of is Low. Lines with no raw
// text (the LLM supplied them) have nothing to show and are left out.
func sourceRows(d ingestcore.Draft) []srcRow {
	var rows []srcRow
	for _, c := range d.Components {
		start := len(rows)
		if c.Name != "" {
			rows = append(rows, srcRow{Text: c.Name, Heading: true})
		}
		shown := false
		for _, l := range c.Lines {
			if strings.TrimSpace(l.Raw) == "" {
				continue
			}
			shown = true
			rows = append(rows, srcRow{Text: l.Raw, Low: l.Confidence < d.Threshold})
		}
		if !shown {
			rows = rows[:start]
		}
	}
	return rows
}

// lowLineCount is how many ingredient lines the rules were unsure of.
func lowLineCount(d ingestcore.Draft) int {
	n := 0
	for _, c := range d.Components {
		for _, l := range c.Lines {
			if l.Confidence < d.Threshold {
				n++
			}
		}
	}
	return n
}

func lowLinesText(n int) string {
	if n == 1 {
		return "1 line needs a look"
	}
	return fmt.Sprintf("%d lines need a look", n)
}
