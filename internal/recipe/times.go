package recipe

import (
	"fmt"
	"time"
)

// Special time labels. Any other label is free text.
const (
	LabelRise     = "rise"
	LabelMarinate = "marinate"
	LabelChill    = "chill"
	LabelRest     = "rest"
)

// SpecialTime is a labelled wait outside prep and cook.
type SpecialTime struct {
	Label    string
	Duration time.Duration
}

// Times are the stated times of a recipe. Total is zero unless the source
// stated one.
type Times struct {
	Prep    time.Duration
	Cook    time.Duration
	Special []SpecialTime
	Total   time.Duration
}

// TotalSource says which rule produced a derived total, so the UI can show it.
type TotalSource string

// Total sources, in the order they are tried (ADR-00004).
const (
	// TotalStored is the total the source stated.
	TotalStored TotalSource = "stored"
	// TotalCriticalPath is the longest chain of dependent steps.
	TotalCriticalPath TotalSource = "critical_path"
	// TotalSum is prep + cook + special times.
	TotalSum TotalSource = "sum"
	// TotalUnknown means no time information exists; the duration is zero.
	TotalUnknown TotalSource = "unknown"
)

// Total is a derived total time and where it came from.
type Total struct {
	Duration time.Duration
	Source   TotalSource
}

// TotalTime derives the recipe's total time: the stored total if present;
// else the critical path of the step graph when every step has a duration;
// else the sum of prep, cook and special times. A recipe with none of these
// reports TotalUnknown. It returns the graph error whenever the step graph is
// invalid and no stored total exists.
func (r Recipe) TotalTime() (Total, error) {
	if r.Times.Total > 0 {
		return Total{r.Times.Total, TotalStored}, nil
	}
	if d, ok, err := r.criticalPath(); err != nil {
		return Total{}, fmt.Errorf("critical path: %w", err)
	} else if ok {
		return Total{d, TotalCriticalPath}, nil
	}
	sum := r.Times.Prep + r.Times.Cook
	for _, s := range r.Times.Special {
		sum += s.Duration
	}
	if sum > 0 {
		return Total{sum, TotalSum}, nil
	}
	return Total{Source: TotalUnknown}, nil
}

// criticalPath returns the longest dependency chain by duration. ok is false
// when there are no steps or any step lacks a duration.
func (r Recipe) criticalPath() (d time.Duration, ok bool, err error) {
	order, err := r.topoOrder()
	if err != nil {
		return 0, false, err
	}
	if len(order) == 0 {
		return 0, false, nil
	}
	finish := make(map[StepID]time.Duration, len(order))
	for _, n := range order {
		if n.step.Duration <= 0 {
			return 0, false, nil
		}
		var start time.Duration
		for _, dep := range n.step.DependsOn {
			start = max(start, finish[dep])
		}
		finish[n.ref.ID] = start + n.step.Duration
		d = max(d, finish[n.ref.ID])
	}
	return d, true, nil
}
