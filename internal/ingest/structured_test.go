package ingest_test

import (
	"errors"
	"testing"

	"github.com/kaecyra/gobbler/internal/ingest"
)

func TestDecodeStructured(t *testing.T) {
	const comp = `{"ingredients":["2 slices bread"],"steps":["Toast."]}`
	good := `{"name":"Toast","components":[` + comp + `]}`
	s, err := ingest.DecodeStructured([]byte(good))
	if err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	if s.Name != "Toast" || len(s.Components) != 1 || s.Components[0].Ingredients[0] != "2 slices bread" {
		t.Fatalf("decoded %+v", s)
	}
	if _, err := ingest.DecodeStructured([]byte(`{"name":"Toast","components":[{"ingredients":[]}]}`)); err != nil {
		t.Fatalf("empty ingredients list must be accepted: %v", err)
	}
	withEquip, err := ingest.DecodeStructured([]byte(`{"name":"Toast","equipment":["toaster","Butter knife"],"components":[` + comp + `]}`))
	if err != nil || len(withEquip.Equipment) != 2 || withEquip.Equipment[1] != "Butter knife" {
		t.Fatalf("equipment list: %+v, %v", withEquip, err)
	}
	if _, err := ingest.DecodeStructured([]byte(good + " \n")); err != nil {
		t.Fatalf("trailing whitespace must be accepted: %v", err)
	}

	for name, in := range map[string]string{
		"empty":                   ``,
		"not json":                `toast`,
		"array":                   `[]`,
		"unknown top-level field": `{"name":"Toast","rating":5,"components":[` + comp + `]}`,
		"unknown component field": `{"name":"Toast","components":[{"ingredients":[],"notes":"x"}]}`,
		"blank name":              `{"name":"  ","components":[` + comp + `]}`,
		"no name":                 `{"components":[` + comp + `]}`,
		"no components":           `{"name":"Toast","components":[]}`,
		"null components":         `{"name":"Toast","components":null}`,
		"missing ingredients":     `{"name":"Toast","components":[{"steps":["x"]}]}`,
		"null ingredients":        `{"name":"Toast","components":[{"ingredients":null}]}`,
		"second ingredientless":   `{"name":"Toast","components":[` + comp + `,{"name":"x"}]}`,
		"blank equipment":         `{"name":"Toast","equipment":["toaster"," "],"components":[` + comp + `]}`,
		"null equipment entry":    `{"name":"Toast","equipment":[null],"components":[` + comp + `]}`,
		"equipment not a list":    `{"name":"Toast","equipment":"toaster","components":[` + comp + `]}`,
		"trailing text":           good + ` and more`,
		"second object":           good + good,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ingest.DecodeStructured([]byte(in))
			if err == nil {
				t.Fatalf("accepted as %+v", got)
			}
			if !errors.Is(err, ingest.ErrInvalidStructured) {
				t.Errorf("error does not wrap ErrInvalidStructured: %v", err)
			}
		})
	}
}

func TestDecodeStructuredReadsAndChecksTheStepGraph(t *testing.T) {
	const base = `{"name":"Bread","components":[{"ingredients":[]}],"step_graph":`
	if s, err := ingest.DecodeStructured([]byte(base + `[{"step":2,"passive":true,"depends_on":[1]}]}`)); err != nil ||
		len(s.StepGraph) != 1 || !s.StepGraph[0].Passive || s.StepGraph[0].DependsOn[0] != 1 {
		t.Fatalf("valid graph: %+v, %v", s, err)
	}
	for name, entries := range map[string]string{
		"step zero":        `[{"step":0,"passive":false,"depends_on":[]}]`,
		"no depends_on":    `[{"step":2,"passive":false}]`,
		"dep not earlier":  `[{"step":2,"passive":false,"depends_on":[2]}]`,
		"dep below one":    `[{"step":2,"passive":false,"depends_on":[0]}]`,
		"unknown property": `[{"step":2,"passive":false,"depends_on":[],"x":1}]`,
	} {
		if _, err := ingest.DecodeStructured([]byte(base + entries + `}`)); !errors.Is(err, ingest.ErrInvalidStructured) {
			t.Errorf("%s: err = %v, want ingest.ErrInvalidStructured", name, err)
		}
	}
}
