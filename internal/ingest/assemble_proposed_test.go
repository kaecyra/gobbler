package ingest

import (
	"reflect"
	"testing"
)

func TestProposedFor(t *testing.T) {
	comp := func(lines ...Line) Component { return Component{Lines: lines} }
	tests := []struct {
		name  string
		comps []Component
		prior []ProposedIngredient
		want  []ProposedIngredient
	}{
		{"none", nil, nil, nil},
		{
			"matched lines are not proposed",
			[]Component{comp(Line{Name: "flour", Ingredient: 3})},
			nil, nil,
		},
		{
			"blank names are skipped",
			[]Component{comp(Line{Name: "  "})},
			nil, nil,
		},
		{
			"normalised and de-duplicated in reading order across components",
			[]Component{
				comp(Line{Name: "  Dragonfruit   ZEST "}, Line{Name: "quux"}),
				comp(Line{Name: "dragonfruit zest"}, Line{Name: "Aardvark"}),
			},
			nil,
			[]ProposedIngredient{
				{Name: "dragonfruit zest", Aisle: "uncategorised"},
				{Name: "quux", Aisle: "uncategorised"},
				{Name: "aardvark", Aisle: "uncategorised"},
			},
		},
		{
			"a name already proposed keeps its aisle; a prior name no longer on a line drops",
			[]Component{comp(Line{Name: "olive oil"}, Line{Name: "quux"})},
			[]ProposedIngredient{{Name: "olive oil", Aisle: "oils"}, {Name: "gone", Aisle: "dairy"}},
			[]ProposedIngredient{
				{Name: "olive oil", Aisle: "oils"},
				{Name: "quux", Aisle: "uncategorised"},
			},
		},
		{
			"a prior entry for a line since matched drops",
			[]Component{comp(Line{Name: "olive oil", Ingredient: 9})},
			[]ProposedIngredient{{Name: "olive oil", Aisle: "oils"}},
			nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ProposedFor(tc.comps, tc.prior)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ProposedFor = %+v, want %+v", got, tc.want)
			}
			if again := ProposedFor(tc.comps, tc.prior); !reflect.DeepEqual(again, got) {
				t.Errorf("not deterministic: %+v then %+v", got, again)
			}
		})
	}
}
