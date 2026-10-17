package ingest

import (
	"fmt"
	"strings"

	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
)

func flagClass(low bool) string {
	if low {
		return "low"
	}
	return ""
}

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
