package recipe

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func st(id string, deps ...StepID) Step { return Step{ID: StepID(id), Text: id, DependsOn: deps} }

func rec(comps ...Component) Recipe { return Recipe{Name: "r", Components: comps} }

func TestValidateSteps(t *testing.T) {
	tests := []struct {
		name     string
		r        Recipe
		wantErr  error
		wantText []string
	}{
		{"valid diamond", rec(Component{Name: "A", Steps: []Step{st("a"), st("b", "a"), st("c", "a"), st("d", "b", "c")}}), nil, nil},
		{"no steps", rec(Component{}), nil, nil},
		{"self dependency", rec(Component{Name: "Dough", Steps: []Step{st("a", "a")}}), ErrCycle, []string{`"Dough" step 1 (a)`}},
		{"two step cycle", rec(Component{Steps: []Step{st("a", "b"), st("b", "a")}}), ErrCycle, []string{"step 1 (a)", "step 2 (b)"}},
		{"cross component cycle", rec(
			Component{Name: "Dough", Steps: []Step{st("mix"), st("bake", "sauce")}},
			Component{Name: "Sauce", Steps: []Step{st("sauce", "bake")}},
		), ErrCycle, []string{`"Dough" step 2 (bake)`, `"Sauce" step 1 (sauce)`}},
		{"outside recipe", rec(Component{Steps: []Step{st("a", "zzz")}}), ErrUnknownDependency, []string{"zzz"}},
		{"duplicate id", rec(Component{Steps: []Step{st("a"), st("a")}}), ErrBadStepID, nil},
		{"empty id", rec(Component{Steps: []Step{st("")}}), ErrBadStepID, nil},
		{"equipment unknown step", Recipe{Components: []Component{{Steps: []Step{st("a")}}}, Equipment: []EquipmentLink{{Steps: []StepID{"nope"}}}}, ErrUnknownDependency, []string{"nope"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.r.ValidateSteps()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			for _, s := range tc.wantText {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("error %q lacks %q", err, s)
				}
			}
		})
	}
}

func TestCycleErrorNamesOnlyCycleMembers(t *testing.T) {
	r := rec(Component{Steps: []Step{st("pre"), st("a", "pre", "b"), st("b", "a")}})
	var ce *CycleError
	if err := r.ValidateSteps(); !errors.As(err, &ce) {
		t.Fatalf("want CycleError, got %v", err)
	}
	if len(ce.Steps) != 2 || ce.Steps[0].ID != "a" || ce.Steps[1].ID != "b" {
		t.Errorf("cycle steps = %+v", ce.Steps)
	}
}

func TestLinearOrder(t *testing.T) {
	r := rec(
		Component{Name: "A", Steps: []Step{st("a1"), st("a2")}},
		Component{Name: "B", Steps: []Step{st("b1", "a2")}},
	)
	var got []string
	for _, s := range r.LinearOrder() {
		got = append(got, string(s.ID))
	}
	if strings.Join(got, ",") != "a1,a2,b1" {
		t.Errorf("order = %v", got)
	}
}

func TestTotalTime(t *testing.T) {
	m := time.Minute
	dur := func(id string, d time.Duration, deps ...StepID) Step {
		s := st(id, deps...)
		s.Duration = d
		return s
	}
	tests := []struct {
		name string
		r    Recipe
		want Total
	}{
		{"stored wins", Recipe{Times: Times{Total: 90 * m, Prep: m}, Components: []Component{{Steps: []Step{dur("a", 5*m)}}}}, Total{90 * m, TotalStored}},
		{"critical path", Recipe{Times: Times{Prep: 100 * m}, Components: []Component{
			{Steps: []Step{dur("a", 10*m), dur("b", 30*m, "a"), dur("c", 5*m, "a"), dur("d", 20*m, "b", "c")}},
			{Steps: []Step{dur("e", 40*m)}},
		}}, Total{60 * m, TotalCriticalPath}},
		{"missing duration falls to sum", Recipe{Times: Times{Prep: 10 * m, Cook: 20 * m, Special: []SpecialTime{{LabelRise, 60 * m}}},
			Components: []Component{{Steps: []Step{dur("a", 5*m), st("b")}}}}, Total{90 * m, TotalSum}},
		{"no steps falls to sum", Recipe{Times: Times{Prep: 10 * m, Cook: 5 * m}, Components: []Component{{}}}, Total{15 * m, TotalSum}},
		{"nothing", Recipe{Components: []Component{{}}}, Total{0, TotalUnknown}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.r.TotalTime()
			if err != nil || got != tc.want {
				t.Errorf("got %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

func TestTotalTimeInvalidGraph(t *testing.T) {
	a := st("a", "a")
	a.Duration = time.Minute
	_, err := rec(Component{Steps: []Step{a}}).TotalTime()
	if !errors.Is(err, ErrCycle) {
		t.Errorf("err = %v, want cycle", err)
	}
}
