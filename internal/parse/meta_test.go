package parse

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

const metaBody = "\n\nIngredients\n2 cups flour\n\nDirections\n1. Mix.\n"

func TestSplitSectionsMeta(t *testing.T) {
	minute := time.Minute
	tests := []struct {
		name      string
		head      string
		want      Meta
		issues    []string // meta issues expected among Issues
		notes     []string
		wantTitle string
	}{
		{
			name:      "labelled lines under a title",
			head:      "Pancakes\nPrep time: 20 min\nCook time: 1 hr 10 min\nTotal time: 1 hr 30 min\nServes 4\nCalories: 350 kcal",
			want:      Meta{Prep: 20 * minute, Cook: 70 * minute, Total: 90 * minute, Yield: "4", Calories: 350},
			wantTitle: "Pancakes",
			notes:     []string{},
		},
		{
			name:      "a meta line first does not become the title",
			head:      "Prep: 10 minutes\nPancakes",
			want:      Meta{Prep: 10 * minute},
			wantTitle: "Pancakes",
			notes:     []string{},
		},
		{
			name:      "several fields on one line",
			head:      "Pancakes\nPrep: 10 min | Cook: 20 min | Serves: 4",
			want:      Meta{Prep: 10 * minute, Cook: 20 * minute, Yield: "4"},
			wantTitle: "Pancakes",
			notes:     []string{},
		},
		{
			name:      "markdown emphasis, bullets, ready in and makes",
			head:      "Pancakes\n- **Ready in:** 45 mins\n* Makes 12 pancakes\n**Calories per serving:** 1,200",
			want:      Meta{Total: 45 * minute, Yield: "12 pancakes", Calories: 1200},
			wantTitle: "Pancakes",
			notes:     []string{},
		},
		{
			name:      "an unreadable duration is flagged and its line kept in notes",
			head:      "Pancakes\nPrep time: a while\nCook time: 20 min",
			want:      Meta{Cook: 20 * minute},
			issues:    []string{IssueUnparsedDuration},
			wantTitle: "Pancakes",
			notes:     []string{"Prep time: a while"},
		},
		{
			name:      "a range is not guessed",
			head:      "Pancakes\nCook time: 20-25 min",
			issues:    []string{IssueUnparsedDuration},
			wantTitle: "Pancakes",
			notes:     []string{"Cook time: 20-25 min"},
		},
		{
			name:      "an unreadable yield and calories are flagged and kept",
			head:      "Pancakes\nYield: plenty\nCalories: lots",
			issues:    []string{IssueUnreadableYield, IssueUnreadableCalories},
			wantTitle: "Pancakes",
			notes:     []string{"Yield: plenty", "Calories: lots"},
		},
		{
			name:      "prose that starts like a label is not meta",
			head:      "Pancakes\nMakes a great breakfast for a crowd.\nCook 20 minutes per side if you like them dark",
			wantTitle: "Pancakes",
			notes:     []string{"Makes a great breakfast for a crowd.", "Cook 20 minutes per side if you like them dark"},
		},
		{
			name:      "a repeated label keeps the first value and the later line",
			head:      "Pancakes\nServes 4\nServes 6",
			want:      Meta{Yield: "4"},
			wantTitle: "Pancakes",
			notes:     []string{"Serves 6"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SplitSections(tc.head + metaBody)
			if err != nil {
				t.Fatal(err)
			}
			var gotMeta Meta
			if got.Meta != nil {
				gotMeta = *got.Meta
			}
			if gotMeta != tc.want {
				t.Errorf("Meta = %+v, want %+v", gotMeta, tc.want)
			}
			if got.Title != tc.wantTitle {
				t.Errorf("Title = %q, want %q", got.Title, tc.wantTitle)
			}
			if !reflect.DeepEqual(got.Notes, tc.notes) {
				t.Errorf("Notes = %q, want %q", got.Notes, tc.notes)
			}
			for _, is := range tc.issues {
				if !slices.Contains(got.Issues, is) {
					t.Errorf("Issues = %v, missing %q", got.Issues, is)
				}
			}
			if len(tc.issues) == 0 {
				for _, is := range []string{IssueUnparsedDuration, IssueUnreadableYield, IssueUnreadableCalories} {
					if slices.Contains(got.Issues, is) {
						t.Errorf("Issues = %v, unexpected %q", got.Issues, is)
					}
				}
			}
		})
	}
}

// Meta labels inside the steps are step text, never meta.
func TestSplitSectionsMetaIgnoredInSteps(t *testing.T) {
	got, err := SplitSections("Soup\n\nIngredients\n1 onion\n\nDirections\n1. Prep: dice the onion.\n2. Cook time: until soft.\n")
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta != nil {
		t.Errorf("Meta = %+v, want none", got.Meta)
	}
	if n := len(got.Components[0].Steps); n != 2 {
		t.Errorf("steps = %q, want both kept", got.Components[0].Steps)
	}
}

func TestSplitSectionsNoMetaLeavesNilMeta(t *testing.T) {
	got, err := SplitSections("Soup" + metaBody)
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta != nil || strings.Contains(strings.Join(got.Issues, ","), "unparsed") {
		t.Errorf("Meta = %+v issues %v, want none", got.Meta, got.Issues)
	}
}

// A footer after the steps is read when it is colon-labelled and clean; the
// same words as prose, or an unreadable value, stay steps with no issue.
func TestSplitSectionsMetaFooterAfterSteps(t *testing.T) {
	const head = "Soup\n\nIngredients\n1 onion\n\nDirections\n1. Dice the onion.\n2. Simmer.\n"
	got, err := SplitSections(head + "Calories: 280\nPrep time: 10 min\n")
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta == nil || got.Meta.Calories != 280 || got.Meta.Prep != 10*time.Minute {
		t.Errorf("Meta = %+v, want calories 280 and prep 10m", got.Meta)
	}
	if want := []string{"Dice the onion.", "Simmer."}; !reflect.DeepEqual(got.Components[0].Steps, want) {
		t.Errorf("steps = %q, want %q", got.Components[0].Steps, want)
	}

	got, err = SplitSections(head + "Cook 20 minutes\nServes 4\nCalories: lots\n")
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta != nil {
		t.Errorf("Meta = %+v, want none from prose or unreadable lines", got.Meta)
	}
	all := strings.Join(got.Components[0].Steps, " ")
	for _, s := range []string{"Cook 20 minutes", "Serves 4", "Calories: lots"} {
		if !strings.Contains(all, s) {
			t.Errorf("steps = %q, want %q kept", got.Components[0].Steps, s)
		}
	}
	for _, is := range []string{IssueUnparsedDuration, IssueUnreadableYield, IssueUnreadableCalories} {
		if slices.Contains(got.Issues, is) {
			t.Errorf("Issues = %v, unexpected %q inside steps", got.Issues, is)
		}
	}
}
