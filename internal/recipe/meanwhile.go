package recipe

import "fmt"

// Meanwhile is the guidance for one passive step: the steps offered as work to
// do while it runs ("while the dough rises: make the sauce").
type Meanwhile struct {
	Passive StepRef
	// Offered is in linear order and may be empty.
	Offered []StepRef
}

// Meanwhile derives meanwhile guidance from the step graph (ADR-00004). It
// returns one entry per passive step, in linear order.
//
// A step is offered during passive step P when all of these hold:
//   - it is active: another passive step needs no attention, so it is not work;
//   - it is not already finished: it is not a prerequisite of P;
//   - every dependency of it is finished before P starts, meaning each is a
//     (transitive) dependency of P. This also excludes everything
//     that waits on P;
//   - it feeds a later step: some step depends on it.
//
// The result is a pure function of the graph. An invalid graph (a cycle, an
// unknown dependency, a bad step ID) yields the error of ValidateSteps and no
// guidance.
func (r Recipe) Meanwhile() ([]Meanwhile, error) {
	order, err := r.topoOrder()
	if err != nil {
		return nil, fmt.Errorf("meanwhile: %w", err)
	}

	// before[id] is every step that must finish before id can start. order
	// lists dependencies first, so each dependency's set is already built.
	before := make(map[StepID]map[StepID]bool, len(order))
	feeds := make(map[StepID]bool, len(order))
	for _, n := range order {
		set := make(map[StepID]bool)
		for _, d := range n.step.DependsOn {
			set[d] = true
			feeds[d] = true
			for a := range before[d] {
				set[a] = true
			}
		}
		before[n.ref.ID] = set
	}

	linear := r.LinearOrder()
	steps := make(map[StepID]Step, len(linear))
	for _, n := range order {
		steps[n.ref.ID] = n.step
	}

	out := []Meanwhile{}
	for _, p := range linear {
		if !steps[p.ID].Passive {
			continue
		}
		m := Meanwhile{Passive: p}
		for _, c := range linear {
			s := steps[c.ID]
			if s.Passive || before[p.ID][c.ID] || !feeds[c.ID] || !startsBefore(s, before[p.ID]) {
				continue
			}
			m.Offered = append(m.Offered, c)
		}
		out = append(out, m)
	}
	return out, nil
}

// startsBefore reports whether every dependency of s is in done.
func startsBefore(s Step, done map[StepID]bool) bool {
	for _, d := range s.DependsOn {
		if !done[d] {
			return false
		}
	}
	return true
}
