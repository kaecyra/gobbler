package ingest

import (
	"reflect"
	"testing"

	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
)

func TestSourceRowsMarkOnlyLinesBelowTheThreshold(t *testing.T) {
	d := ingestcore.Draft{Threshold: 0.6, Components: []ingestcore.Component{
		{Name: "Sauce", Lines: []ingestcore.Line{
			{Raw: "2 tbsp butter", Confidence: 1},
			{Raw: "a glug of oil", Confidence: 0.3},
			{Raw: "", Confidence: 0.1, FromLLM: true},
		}},
		{Name: "Empty", Lines: []ingestcore.Line{{Raw: "", Confidence: 1}}},
	}}
	want := []srcRow{
		{Text: "Sauce", Heading: true},
		{Text: "2 tbsp butter"},
		{Text: "a glug of oil", Low: true},
	}
	if got := sourceRows(d); !reflect.DeepEqual(got, want) {
		t.Errorf("sourceRows = %+v, want %+v", got, want)
	}
	if got := lowLineCount(d); got != 2 {
		t.Errorf("lowLineCount = %d, want the unsure line and the blank LLM line", got)
	}
}

func TestFormModeFollowsWhatWasSubmitted(t *testing.T) {
	for _, tc := range []struct {
		v    formView
		want string
	}{
		{formView{}, modeLink},
		{formView{Source: modeText}, modeText},
		{formView{Text: "x"}, modeText},
		{formView{URL: "https://a.test", Text: "x"}, modeBoth},
		{formView{URL: "https://a.test"}, modeLink},
	} {
		if got := tc.v.mode(); got != tc.want {
			t.Errorf("%+v mode = %q, want %q", tc.v, got, tc.want)
		}
	}
}
