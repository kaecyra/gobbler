package recipe

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors returned by ValidateSteps. Callers branch with errors.Is.
var (
	// ErrCycle is returned when step dependencies form a cycle. The concrete
	// error is a *CycleError naming the steps.
	ErrCycle = errors.New("step dependency cycle")
	// ErrUnknownDependency is returned when a step depends on an ID that is
	// not a step of the same recipe.
	ErrUnknownDependency = errors.New("dependency on a step outside the recipe")
	// ErrBadStepID is returned for an empty or duplicated step ID.
	ErrBadStepID = errors.New("step ID empty or duplicated")
)

// StepRef locates a step: its ID, component and 1-based position within it.
type StepRef struct {
	ID             StepID
	Component      string
	ComponentIndex int
	Position       int
}

// String renders the reference for people: `"Dough" step 2 (id)`; an unnamed
// component reads `step 2 (id)`.
func (s StepRef) String() string {
	if s.Component == "" {
		return fmt.Sprintf("step %d (%s)", s.Position, s.ID)
	}
	return fmt.Sprintf("%q step %d (%s)", s.Component, s.Position, s.ID)
}

// CycleError names the steps of a dependency cycle in order, each depending on
// the next and the last on the first.
type CycleError struct {
	Steps []StepRef
}

func (e *CycleError) Error() string {
	parts := make([]string, 0, len(e.Steps)+1)
	for _, s := range e.Steps {
		parts = append(parts, s.String())
	}
	parts = append(parts, e.Steps[0].String())
	return ErrCycle.Error() + ": " + strings.Join(parts, " depends on ")
}

// Is makes errors.Is(err, ErrCycle) true.
func (e *CycleError) Is(target error) bool { return target == ErrCycle }

// LinearOrder returns every step in the default order: component order, then
// step order within the component.
func (r Recipe) LinearOrder() []StepRef {
	var out []StepRef
	for ci, c := range r.Components {
		for si, s := range c.Steps {
			out = append(out, StepRef{ID: s.ID, Component: c.Name, ComponentIndex: ci, Position: si + 1})
		}
	}
	return out
}

// ValidateSteps checks that step IDs are non-empty and unique, that every
// dependency (and every equipment step link) names a step of this recipe, and
// that the dependencies form a DAG. A cycle yields a *CycleError.
func (r Recipe) ValidateSteps() error {
	_, err := r.topoOrder()
	if err != nil {
		return err
	}
	return r.validateEquipmentSteps()
}

func (r Recipe) validateEquipmentSteps() error {
	known := make(map[StepID]bool)
	for _, ref := range r.LinearOrder() {
		known[ref.ID] = true
	}
	for i, e := range r.Equipment {
		for _, id := range e.Steps {
			if !known[id] {
				return fmt.Errorf("%w: equipment %d uses step %q", ErrUnknownDependency, i+1, id)
			}
		}
	}
	return nil
}

// stepNode is a step with its location, indexed by ID.
type stepNode struct {
	ref  StepRef
	step Step
}

// topoOrder validates the graph and returns steps with dependencies before
// dependents. The order is deterministic: ties follow linear order.
func (r Recipe) topoOrder() ([]stepNode, error) {
	var nodes []stepNode
	byID := make(map[StepID]int)
	for _, ref := range r.LinearOrder() {
		if ref.ID == "" {
			return nil, fmt.Errorf("%w: %s", ErrBadStepID, ref)
		}
		if _, dup := byID[ref.ID]; dup {
			return nil, fmt.Errorf("%w: %s", ErrBadStepID, ref)
		}
		byID[ref.ID] = len(nodes)
		nodes = append(nodes, stepNode{ref: ref, step: r.Components[ref.ComponentIndex].Steps[ref.Position-1]})
	}
	for _, n := range nodes {
		for _, d := range n.step.DependsOn {
			if _, ok := byID[d]; !ok {
				return nil, fmt.Errorf("%w: %s depends on %q", ErrUnknownDependency, n.ref, d)
			}
		}
	}

	const (
		white = iota
		grey
		black
	)
	color := make([]int, len(nodes))
	var order []stepNode
	var stack []int
	var visit func(i int) error
	visit = func(i int) error {
		color[i] = grey
		stack = append(stack, i)
		for _, d := range nodes[i].step.DependsOn {
			j := byID[d]
			switch color[j] {
			case grey:
				start := len(stack) - 1
				for stack[start] != j {
					start--
				}
				cyc := make([]StepRef, 0, len(stack)-start)
				for _, k := range stack[start:] {
					cyc = append(cyc, nodes[k].ref)
				}
				return &CycleError{Steps: cyc}
			case white:
				if err := visit(j); err != nil {
					return err
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[i] = black
		order = append(order, nodes[i])
		return nil
	}
	for i := range nodes {
		if color[i] == white {
			if err := visit(i); err != nil {
				return nil, err
			}
		}
	}
	return order, nil
}
