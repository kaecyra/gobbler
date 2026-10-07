package parse

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var sectionsUpdate = flag.Bool("update-sections", false, "rewrite the sections golden files")

func TestSplitSectionsCorpus(t *testing.T) {
	inputs, err := filepath.Glob("testdata/sections/*.txt")
	if err != nil || len(inputs) == 0 {
		t.Fatalf("no corpus inputs found: %v", err)
	}
	for _, in := range inputs {
		name := strings.TrimSuffix(filepath.Base(in), ".txt")
		t.Run(name, func(t *testing.T) {
			text, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}
			got, err := SplitSections(string(text))
			if err != nil {
				t.Fatalf("SplitSections: %v", err)
			}
			gotJSON, err := json.MarshalIndent(got, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			gotJSON = append(gotJSON, '\n')
			golden := strings.TrimSuffix(in, ".txt") + ".golden.json"
			if *sectionsUpdate {
				if err := os.WriteFile(golden, gotJSON, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if string(want) != string(gotJSON) {
				t.Errorf("output differs from %s\n got: %s\nwant: %s", golden, gotJSON, want)
			}
			// Determinism: a second run must be identical.
			again, err := SplitSections(string(text))
			if err != nil {
				t.Fatalf("second SplitSections: %v", err)
			}
			if !reflect.DeepEqual(got, again) {
				t.Error("repeated run gave different output")
			}
		})
	}
}

func TestSplitSectionsEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\r\n\t\n"} {
		if _, err := SplitSections(in); !errors.Is(err, ErrEmptyText) {
			t.Errorf("SplitSections(%q) err = %v, want ErrEmptyText", in, err)
		}
	}
}

func TestSplitSectionsNonEmptyIsNeverAnError(t *testing.T) {
	for _, in := range []string{"x", "...", "Ingredients", "Directions:", "For the sauce:", "1."} {
		if _, err := SplitSections(in); err != nil {
			t.Errorf("SplitSections(%q) err = %v, want nil", in, err)
		}
	}
}

func TestSplitSectionsCases(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		title  string
		ing    [][]string // per component
		steps  [][]string
		names  []string
		issues []string
	}{
		{
			name:  "headings are case-insensitive and colon-optional",
			in:    "Soup\nINGREDIENTS:\n1 onion\nmethod\nChop the onion.\nCook it.",
			title: "Soup", names: []string{""},
			ing: [][]string{{"1 onion"}}, steps: [][]string{{"Chop the onion.", "Cook it."}},
		},
		{
			name:  "numbered step markers stripped with several styles",
			in:    "T\nIngredients\n1 egg\nDirections\n1. One\n2) Two\n3: Three\nStep 4: Four",
			title: "T", names: []string{""},
			ing: [][]string{{"1 egg"}}, steps: [][]string{{"One", "Two", "Three", "Four"}},
		},
		{
			name:  "wrapped numbered step continues previous step",
			in:    "T\nIngredients\n1 egg\nDirections\n1. Beat the\negg well.\n2. Fry.",
			title: "T", names: []string{""},
			ing: [][]string{{"1 egg"}}, steps: [][]string{{"Beat the egg well.", "Fry."}},
		},
		{
			name:  "bulleted steps",
			in:    "T\nIngredients\n1 egg\nDirections\n- Beat.\n- Fry.",
			title: "T", names: []string{""},
			ing: [][]string{{"1 egg"}}, steps: [][]string{{"Beat.", "Fry."}},
		},
		{
			name:  "step heading with marker alone on its line",
			in:    "T\nIngredients\n1 egg\nDirections\nStep 1\nBeat the egg.\nStep 2\nFry it.",
			title: "T", names: []string{""},
			ing: [][]string{{"1 egg"}}, steps: [][]string{{"Beat the egg.", "Fry it."}},
		},
		{
			name:  "unmarked steps on consecutive lines are one step each",
			in:    "T\nIngredients\n1 egg\nDirections\nBeat the egg.\nFry it.",
			title: "T", names: []string{""},
			ing: [][]string{{"1 egg"}}, steps: [][]string{{"Beat the egg.", "Fry it."}},
		},
		{
			name:  "step line ending in colon is not a component heading",
			in:    "T\nIngredients\n1 egg\nDirections\nMix the following:\nsalt and egg.",
			title: "T", names: []string{""},
			ing: [][]string{{"1 egg"}}, steps: [][]string{{"Mix the following:", "salt and egg."}},
		},
		{
			name:  "for-the heading names component without prefix",
			in:    "T\nIngredients\nFor the Sauce\n1 tomato\nFor the base:\n1 flour\nDirections\nFor the sauce:\nSimmer.",
			title: "T", names: []string{"Sauce", "base"},
			ing: [][]string{{"1 tomato"}, {"1 flour"}}, steps: [][]string{{"Simmer."}, {}},
		},
		{
			name:  "title that starts with a digit when headings follow",
			in:    "7-Layer Dip\nIngredients\n1 can beans\nDirections\nLayer.",
			title: "7-Layer Dip", names: []string{""},
			ing: [][]string{{"1 can beans"}}, steps: [][]string{{"Layer."}},
		},
		{
			name:  "crlf line endings",
			in:    "T\r\nIngredients\r\n1 egg\r\nDirections\r\nFry.\r\n",
			title: "T", names: []string{""},
			ing: [][]string{{"1 egg"}}, steps: [][]string{{"Fry."}},
		},
		{
			name: "no title is reported", in: "Ingredients\n1 egg\nDirections\nFry.",
			names: []string{""}, ing: [][]string{{"1 egg"}}, steps: [][]string{{"Fry."}},
			issues: []string{IssueNoTitle},
		},
		{
			name: "missing steps reported", in: "T\nIngredients\n1 egg",
			title: "T", names: []string{""}, ing: [][]string{{"1 egg"}}, steps: [][]string{{}},
			issues: []string{IssueNoSteps},
		},
		{
			name: "missing ingredients reported", in: "T\nDirections\nFry.",
			title: "T", names: []string{""}, ing: [][]string{{}}, steps: [][]string{{"Fry."}},
			issues: []string{IssueNoIngredients},
		},
		{
			name: "headless text is split by line shape", in: "Toast\n2 slices bread\n1 tbsp butter\nToast the bread. Butter it.",
			title: "Toast", names: []string{""},
			ing: [][]string{{"2 slices bread", "1 tbsp butter"}}, steps: [][]string{{"Toast the bread. Butter it."}},
			issues: []string{IssueNoHeadings},
		},
		{
			name: "missing steps heading is an implicit boundary", in: "Toast\nIngredients\n2 slices bread\nToast the bread.",
			title: "Toast", names: []string{""},
			ing: [][]string{{"2 slices bread"}}, steps: [][]string{{"Toast the bread."}},
			issues: []string{IssueImplicitBoundary},
		},
		{
			name:  "decimal quantity is not a step marker",
			in:    "T\nIngredients\n1 onion\nDirections\nChop the onion.\n\n2.5 cups of stock go in next.",
			title: "T", names: []string{""},
			ing: [][]string{{"1 onion"}}, steps: [][]string{{"Chop the onion.", "2.5 cups of stock go in next."}},
		},
		{
			name: "decimal quantity line is an ingredient when headless", in: "Bread\n1.5 cups flour\n2 eggs\nMix everything together in a large bowl.",
			title: "Bread", names: []string{""},
			ing: [][]string{{"1.5 cups flour", "2 eggs"}}, steps: [][]string{{"Mix everything together in a large bowl."}},
			issues: []string{IssueNoHeadings},
		},
		{
			name:  "period-ended ingredient does not leave ingredients when a steps heading follows",
			in:    "Soup\nIngredients\n1 onion\nSalt and pepper, to taste.\n2 carrots\nDirections\nChop.",
			title: "Soup", names: []string{""},
			ing: [][]string{{"1 onion", "Salt and pepper, to taste.", "2 carrots"}}, steps: [][]string{{"Chop."}},
		},
		{
			name:  "for in step prose is not a component heading",
			in:    "T\nIngredients\n1 onion\nDirections\nSimmer the soup\nfor 20 minutes\nServe.",
			title: "T", names: []string{""},
			ing: [][]string{{"1 onion"}}, steps: [][]string{{"Simmer the soup", "for 20 minutes", "Serve."}},
		},
		{
			name:  "for in notes is not a component heading",
			in:    "T\nIngredients\n1 onion\nDirections\nChop.\nNotes\nFor best results\nuse fresh onions.",
			title: "T", names: []string{""},
			ing: [][]string{{"1 onion"}}, steps: [][]string{{"Chop."}},
		},
		{
			name:  "for followed by a number is not a component",
			in:    "T\nIngredients\nFor 2 servings:\n1 onion\nDirections\nChop.",
			title: "T", names: []string{""},
			ing: [][]string{{"For 2 servings:", "1 onion"}}, steps: [][]string{{"Chop."}},
		},
		{
			name:  "for-heading with a colon is accepted in steps",
			in:    "T\nIngredients\n1 onion\nDirections\nChop.\nFor the glaze:\nBrush.",
			title: "T", names: []string{"", "glaze"},
			ing: [][]string{{"1 onion"}, {}}, steps: [][]string{{"Chop."}, {"Brush."}},
		},
		{
			name:  "markdown heading inside steps is a component",
			in:    "T\nIngredients\n1 onion\nDirections\n## Assembly\nStack.",
			title: "T", names: []string{"", "Assembly"},
			ing: [][]string{{"1 onion"}, {}}, steps: [][]string{{}, {"Stack."}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SplitSections(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got.Title != tt.title {
				t.Errorf("title = %q, want %q", got.Title, tt.title)
			}
			if len(got.Components) != len(tt.names) {
				t.Fatalf("components = %+v, want names %v", got.Components, tt.names)
			}
			for i, c := range got.Components {
				if c.Name != tt.names[i] {
					t.Errorf("component %d name = %q, want %q", i, c.Name, tt.names[i])
				}
				if !reflect.DeepEqual(c.Ingredients, tt.ing[i]) {
					t.Errorf("component %d ingredients = %q, want %q", i, c.Ingredients, tt.ing[i])
				}
				if !reflect.DeepEqual(c.Steps, tt.steps[i]) {
					t.Errorf("component %d steps = %q, want %q", i, c.Steps, tt.steps[i])
				}
			}
			if !reflect.DeepEqual(got.Issues, nonNilIssues(tt.issues)) {
				t.Errorf("issues = %v, want %v", got.Issues, tt.issues)
			}
			if len(tt.issues) == 0 && got.Confidence != 1 {
				t.Errorf("confidence = %v, want 1 with no issues", got.Confidence)
			}
			if len(tt.issues) > 0 && got.Confidence >= 1 {
				t.Errorf("confidence = %v, want below 1 with issues %v", got.Confidence, tt.issues)
			}
		})
	}
}

func nonNilIssues(i []string) []string {
	if i == nil {
		return []string{}
	}
	return i
}

func TestSplitSectionsConfidenceFloor(t *testing.T) {
	got, err := SplitSections("...")
	if err != nil {
		t.Fatal(err)
	}
	if got.Confidence < 0 || got.Confidence > 0.3 {
		t.Errorf("confidence = %v, want low and non-negative", got.Confidence)
	}
}
