package ingest

import (
	"fmt"
	"strings"
	"time"

	"github.com/a-h/templ"

	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// A field or line is flagged when the rules were less sure of it than the
// threshold the draft was judged against.

func lowRequired(f ingestcore.Field[string], threshold float64) bool {
	return f.Confidence < threshold
}

// lowOptional does not flag a field the source never stated: absence is not
// a misreading.
func lowOptional(f ingestcore.Field[string], threshold float64) bool {
	return f.Value != "" && f.Confidence < threshold
}

func flagClass(low bool) string {
	if low {
		return "low"
	}
	return ""
}

// flagMark is the non-colour signal for a flagged item.
func flagMark(low bool) templ.Component {
	if !low {
		return templ.NopComponent
	}
	return templ.Raw(`<strong class="flag">Check:</strong>`)
}

// llmMark is the signal that the LLM, not the rules, supplied a value
// (ADR-00006 obligation 4). It is text, so it does not rely on colour.
func llmMark(fromLLM bool) templ.Component {
	if !fromLLM {
		return templ.NopComponent
	}
	return templ.Raw(`<strong class="llm-mark">LLM</strong>`)
}

// markClass joins the classes of a flagged and/or LLM-supplied item.
func markClass(low, fromLLM bool) string {
	switch {
	case low && fromLLM:
		return "low llm"
	case low:
		return "low"
	case fromLLM:
		return "llm"
	}
	return ""
}

func percent(f float64) string { return fmt.Sprintf("%.0f%%", f*100) }

func formatDuration(d time.Duration) string {
	mins := int(d.Round(time.Minute) / time.Minute)
	switch {
	case mins >= 60 && mins%60 == 0:
		return fmt.Sprintf("%d h", mins/60)
	case mins >= 60:
		return fmt.Sprintf("%d h %d min", mins/60, mins%60)
	case mins == 0:
		return d.Round(time.Second).String()
	}
	return fmt.Sprintf("%d min", mins)
}

func yieldText(y recipe.Yield) string {
	return strings.TrimSpace(y.Qty.String() + " " + y.Unit)
}

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

// lineSummary is the parsed reading of a line: amount, ingredient, qualifiers.
func lineSummary(l ingestcore.Line) string {
	var parts []string
	switch {
	case l.ToTaste:
		parts = append(parts, "to taste")
	default:
		if a := l.Amount.String(); a != "" {
			parts = append(parts, a)
		}
	}
	if l.Name != "" {
		parts = append(parts, l.Name)
	}
	for _, q := range l.Qualifiers {
		parts = append(parts, fmt.Sprintf("(%s: %s)", q.Kind, q.Text))
	}
	if l.Optional {
		parts = append(parts, "(optional)")
	}
	return strings.Join(parts, " ")
}
