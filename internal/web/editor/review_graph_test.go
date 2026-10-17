package editor

import (
	"bytes"
	"html"
	"net/url"
	"regexp"
	"slices"
	"testing"

	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
)

var graphField = regexp.MustCompile(`<input type="hidden" name="(c\.\d+\.s\.\d+\.graph)" value="([^"]*)"`)

// graphBase is a draft whose second step is a rule-read passive step and whose
// third was read by the LLM.
func graphBase() ingestcore.Draft {
	d := ingestcore.Draft{
		Name:      ingestcore.Field[string]{Value: "Loaf", Confidence: 1},
		Threshold: 0.7,
		Components: []ingestcore.Component{{
			Lines: []ingestcore.Line{{Raw: "1 cup zest", Name: "zest", Confidence: 1}},
			Steps: []ingestcore.Step{
				{Text: "Mix.", Confidence: 1},
				{Text: "Bake.", Confidence: 1, Passive: true, DependsOn: []int{1}, GraphCue: true},
				{Text: "Cool.", Confidence: 1, DependsOn: []int{2}, GraphFromLLM: true},
			},
		}},
	}
	return d
}

// postedGraphForm is the review form as the browser would post it back.
func postedGraphForm(t *testing.T, e *env, u Unsaved, base ingestcore.Draft) url.Values {
	t.Helper()
	form, err := u.Open(e.ctx, base, ReviewOptions{Action: "/x"})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := form.Component().Render(e.ctx, &buf); err != nil {
		t.Fatal(err)
	}
	v := url.Values{
		"name": {"Loaf"}, "c.0.name": {""},
		"c.0.l.0.qty": {"1"}, "c.0.l.0.unit": {"cup"}, "c.0.l.0.ing": {"zest"}, "c.0.l.0.propose": {"1"},
		"c.0.l.0.rule": {"linear"}, "c.0.l.0.raw": {"1 cup zest"},
	}
	for i, s := range []string{"Mix.", "Bake.", "Cool."} {
		p := "c.0.s." + string(rune('0'+i)) + "."
		v.Set(p+"text", s)
		v.Set(p+"to", "0")
		v.Set(p+"id", "s"+string(rune('1'+i)))
	}
	v.Set("c.0.s.1.passive", "1")
	v.Set("c.0.s.1.dep", "s1")
	v.Set("c.0.s.2.dep", "s2")
	for _, m := range graphField.FindAllStringSubmatch(buf.String(), -1) {
		v.Set(m[1], html.UnescapeString(m[2]))
	}
	return v
}

func submitGraph(t *testing.T, e *env, u Unsaved, v url.Values) []ingestcore.Step {
	t.Helper()
	out, err := u.Submit(e.ctx, v, graphBase(), ReviewOptions{Action: "/x"})
	if err != nil || !out.Ready {
		t.Fatalf("Submit: ready=%v err=%v", out.Ready, err)
	}
	return out.Draft.Components[0].Steps
}

// What GraphCue and GraphFromLLM say is the rules' or the LLM's reading of the
// graph: they stay while the user leaves that step's graph alone, and clear
// when the user changes it, because the graph is then the user's.
func TestGraphProvenanceSurvivesUntilTheUserEditsThatStep(t *testing.T) {
	e := newEnv(t)
	u := NewUnsaved(e.cat)

	steps := submitGraph(t, e, u, postedGraphForm(t, e, u, graphBase()))
	if !steps[1].GraphCue || !steps[2].GraphFromLLM || !steps[1].Passive || !slices.Equal(steps[2].DependsOn, []int{2}) {
		t.Errorf("unedited steps = %+v, want the inferred graph and its provenance", steps)
	}

	v := postedGraphForm(t, e, u, graphBase())
	v.Del("c.0.s.1.passive")
	v.Set("c.0.s.2.dep", "s1")
	steps = submitGraph(t, e, u, v)
	if steps[1].GraphCue || steps[2].GraphFromLLM || steps[1].Passive || !slices.Equal(steps[2].DependsOn, []int{1}) {
		t.Errorf("edited steps = %+v, want the user's graph with no rule or LLM provenance", steps)
	}

	v = postedGraphForm(t, e, u, graphBase())
	v.Set("c.0.s.3.text", "Serve.")
	v.Set("c.0.s.3.to", "0")
	v.Set("c.0.s.3.dep", "s3")
	steps = submitGraph(t, e, u, v)
	if len(steps) != 4 || steps[3].GraphCue || steps[3].GraphFromLLM || !slices.Equal(steps[3].DependsOn, []int{3}) {
		t.Errorf("added step = %+v, want its own graph, not marked as inferred", steps)
	}
}
