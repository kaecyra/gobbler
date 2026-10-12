package recipe

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func passive(s Step) Step { s.Passive = true; return s }

// guidanceIDs flattens guidance to "passive: offered,offered" strings, which
// are easy to compare and to read in a failure.
func guidanceIDs(g []Meanwhile) []string {
	out := make([]string, 0, len(g))
	for _, m := range g {
		s := string(m.Passive.ID) + ":"
		for i, o := range m.Offered {
			if i > 0 {
				s += ","
			}
			s += string(o.ID)
		}
		out = append(out, s)
	}
	return out
}

func TestMeanwhile(t *testing.T) {
	tests := []struct {
		name string
		r    Recipe
		want []string
	}{
		{"no steps", rec(Component{}), []string{}},
		{"no passive steps means no guidance", rec(Component{Steps: []Step{st("a"), st("b", "a"), st("c")}}), []string{}},
		{
			"dough and sauce across components",
			rec(
				Component{Name: "Dough", Steps: []Step{
					st("mix"), passive(st("rise", "mix")), st("shape", "rise"), st("bake", "shape"),
				}},
				Component{Name: "Sauce", Steps: []Step{
					st("chop"), st("simmer", "chop"), st("assemble", "bake", "simmer"),
				}},
			),
			// chop is ready and feeds simmer; simmer waits for chop; shape
			// waits for the rise itself.
			[]string{"rise:chop"},
		},
		{
			"passive step with nothing to offer",
			rec(Component{Steps: []Step{st("a"), passive(st("wait", "a")), st("b", "wait")}}),
			[]string{"wait:"},
		},
		{
			"a step whose dependency is not yet satisfied is not offered",
			rec(Component{Steps: []Step{
				passive(st("proof")), st("prep"), st("fill", "prep"), st("assemble", "proof", "fill"),
			}}),
			[]string{"proof:prep"},
		},
		{
			"a step whose dependency was finished before the passive step is offered",
			rec(Component{Steps: []Step{
				st("a"), passive(st("wait", "a")), st("b", "a"), st("c", "b", "wait"),
			}}),
			[]string{"wait:b"},
		},
		{
			"a step that feeds nothing is not offered",
			rec(Component{Steps: []Step{
				passive(st("wait")), st("wash up"), st("feeds"), st("final", "wait", "feeds"),
			}}),
			[]string{"wait:feeds"},
		},
		{
			"a passive step is not offered as work during another",
			rec(Component{Steps: []Step{
				passive(st("rise1")), passive(st("rise2")), st("final", "rise1", "rise2"),
			}}),
			[]string{"rise1:", "rise2:"},
		},
		{
			"long chain offers only its ready head",
			rec(Component{Steps: []Step{
				passive(st("wait")), st("c1"), st("c2", "c1"), st("c3", "c2"), st("c4", "c3"), st("final", "wait", "c4"),
			}}),
			[]string{"wait:c1"},
		},
		{
			"several passive steps and several offers in linear order",
			rec(
				Component{Name: "A", Steps: []Step{passive(st("p1")), st("x"), st("y")}},
				Component{Name: "B", Steps: []Step{passive(st("p2", "p1")), st("z"), st("last", "x", "y", "z", "p2")}},
			),
			[]string{"p1:x,y,z", "p2:x,y,z"},
		},
		{
			"no step graph at all gives no offers",
			rec(Component{Steps: []Step{passive(st("a")), st("b"), st("c")}}),
			[]string{"a:"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, err := tc.r.Meanwhile()
			if err != nil {
				t.Fatal(err)
			}
			if got := guidanceIDs(g); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("guidance = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMeanwhileLocatesSteps(t *testing.T) {
	r := rec(
		Component{Name: "Dough", Steps: []Step{st("mix"), passive(st("rise", "mix"))}},
		Component{Name: "Sauce", Steps: []Step{st("x"), st("chop"), st("simmer", "chop")}},
		Component{Name: "Finish", Steps: []Step{st("serve", "rise", "simmer")}},
	)
	g, err := r.Meanwhile()
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 1 || len(g[0].Offered) != 1 {
		t.Fatalf("guidance = %+v", g)
	}
	want := StepRef{ID: "chop", Component: "Sauce", ComponentIndex: 1, Position: 2}
	if g[0].Offered[0] != want {
		t.Errorf("offered = %+v, want %+v", g[0].Offered[0], want)
	}
	if g[0].Passive.ID != "rise" || g[0].Passive.Component != "Dough" || g[0].Passive.Position != 2 {
		t.Errorf("passive = %+v", g[0].Passive)
	}
}

func TestMeanwhileInvalidGraph(t *testing.T) {
	tests := []struct {
		name string
		r    Recipe
		want error
	}{
		{"cycle", rec(Component{Steps: []Step{passive(st("a", "b")), st("b", "a")}}), ErrCycle},
		{"self cycle", rec(Component{Steps: []Step{passive(st("a", "a"))}}), ErrCycle},
		{"unknown dependency", rec(Component{Steps: []Step{passive(st("a", "zzz"))}}), ErrUnknownDependency},
		{"duplicate id", rec(Component{Steps: []Step{passive(st("a")), st("a")}}), ErrBadStepID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, err := tc.r.Meanwhile()
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if g != nil {
				t.Errorf("guidance = %+v alongside an error", g)
			}
		})
	}
}

func TestMeanwhileDeterministic(t *testing.T) {
	build := func(deps func(...StepID) []StepID) Recipe {
		return rec(
			Component{Name: "Dough", Steps: []Step{
				st("mix"), passive(Step{ID: "rise", Text: "rise", DependsOn: deps("mix")}),
			}},
			Component{Name: "Sauce", Steps: []Step{
				st("a"), st("b"), st("c"),
				{ID: "sauce", Text: "sauce", DependsOn: deps("a", "b", "c")},
				{ID: "serve", Text: "serve", DependsOn: deps("rise", "sauce")},
			}},
		)
	}
	ident := func(d ...StepID) []StepID { return d }
	reversed := func(d ...StepID) []StepID {
		out := make([]StepID, len(d))
		for i, s := range d {
			out[len(d)-1-i] = s
		}
		return out
	}
	want, err := build(ident).Meanwhile()
	if err != nil {
		t.Fatal(err)
	}
	if got := guidanceIDs(want); !reflect.DeepEqual(got, []string{"rise:a,b,c"}) {
		t.Fatalf("guidance = %q", got)
	}
	for i := 0; i < 20; i++ {
		for name, f := range map[string]func(...StepID) []StepID{"ident": ident, "reversed": reversed} {
			got, err := build(f).Meanwhile()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("run %d (%s): %s, want %s", i, name, fmt.Sprint(got), fmt.Sprint(want))
			}
		}
	}
}
