package ingest

import (
	"reflect"
	"testing"
)

func TestLLMEquipmentIsMatchedToTheCatalogAndMarked(t *testing.T) {
	e := newEnv(t)
	ans := llmAnswer()
	ans.Equipment = []string{"Whisk", "pastry blender", "Whisks"}
	ans.Components[0].Steps = []string{"Heat the oven.", "Simmer for 20 minutes."}
	fp := &fakeProvider{resp: ans}

	d, err := e.pipelineWith(t, testConfig(), fp).FromText(e.ctx, messyText)
	if err != nil {
		t.Fatal(err)
	}
	// The LLM's list first, then catalog names found in its steps; the
	// unknown name is a proposal, like an unmatched ingredient.
	want := []string{"whisk", "pastry blender?", "oven"}
	if got := equipmentOf(d); !reflect.DeepEqual(got, want) {
		t.Fatalf("equipment = %q, want %q", got, want)
	}
	if !d.Equipment[0].FromLLM || !d.Equipment[1].FromLLM {
		t.Errorf("the LLM's equipment must be marked FromLLM: %+v", d.Equipment)
	}
	if d.Equipment[2].FromLLM {
		t.Errorf("equipment the rules found in the text is not the LLM's: %+v", d.Equipment[2])
	}
	if got := d.ProposedEquipment(); !reflect.DeepEqual(got, []ProposedEquipment{{Name: "pastry blender"}}) {
		t.Errorf("proposed = %+v", got)
	}
	if unrev, err := e.catalog.ListUnreviewedEquipment(e.ctx); err != nil || len(unrev) != 0 {
		t.Errorf("the LLM path must not create equipment: %+v, %v", unrev, err)
	}
}

// The rule-based reading's equipment (a page's own tool list among it)
// survives an LLM answer that names none.
func TestLLMDraftKeepsTheRuleReadingsEquipment(t *testing.T) {
	e := newEnv(t)
	srv, _ := serve(t, []byte(toolsPage))
	fp := &fakeProvider{resp: llmAnswer()}
	p := e.pipelineWith(t, testConfig(), fp)
	p.deps.HTTP = srv.Client()

	rule, err := p.FromURL(e.ctx, srv.URL+"/braise")
	if err != nil {
		t.Fatal(err)
	}
	d, err := p.Fallback().Retry(e.ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"dutch oven", "mandoline slicer?"}
	if got := equipmentOf(d); !reflect.DeepEqual(got, want) {
		t.Errorf("equipment = %q, want %q", got, want)
	}
	for _, eq := range d.Equipment {
		if eq.FromLLM {
			t.Errorf("%q was read by the rules, not supplied by the LLM", eq.Name)
		}
	}
}
