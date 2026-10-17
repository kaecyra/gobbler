package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrInvalidStructured marks provider output that is not a valid Structured.
// DecodeStructured wraps it, so a caller branches with errors.Is.
var ErrInvalidStructured = errors.New("does not match the ingestion schema")

// DecodeStructured is the one strict reading of a provider's answer
// (ADR-00006, ADR-00007): exactly one JSON object, no unknown fields, a
// non-blank name, no blank equipment name, at least one component, an
// ingredients key on every component, and a step_graph that names each step
// once with dependencies on earlier steps only. Anything else is an error wrapping
// ErrInvalidStructured, never a partial Structured. Every provider calls it; none keeps its own copy.
func DecodeStructured(raw []byte) (Structured, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var s Structured
	if err := dec.Decode(&s); err != nil {
		return Structured{}, fmt.Errorf("%w: %w", ErrInvalidStructured, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Structured{}, fmt.Errorf("%w: text after the JSON object", ErrInvalidStructured)
	}
	if strings.TrimSpace(s.Name) == "" {
		return Structured{}, fmt.Errorf("%w: recipe has no name", ErrInvalidStructured)
	}
	if len(s.Components) == 0 {
		return Structured{}, fmt.Errorf("%w: recipe has no components", ErrInvalidStructured)
	}
	for i, name := range s.Equipment {
		if strings.TrimSpace(name) == "" {
			return Structured{}, fmt.Errorf("%w: equipment %d is blank", ErrInvalidStructured, i)
		}
	}
	seen := map[int]bool{}
	for i, a := range s.StepGraph {
		switch {
		case a.Step < 1:
			return Structured{}, fmt.Errorf("%w: step_graph entry %d names step %d", ErrInvalidStructured, i, a.Step)
		case a.DependsOn == nil:
			return Structured{}, fmt.Errorf("%w: step_graph entry %d has no depends_on", ErrInvalidStructured, i)
		case seen[a.Step]:
			return Structured{}, fmt.Errorf("%w: step_graph answers step %d twice", ErrInvalidStructured, a.Step)
		}
		seen[a.Step] = true
		if err := checkDependsOn(a.Step, a.DependsOn); err != nil {
			return Structured{}, fmt.Errorf("%w: step_graph %w", ErrInvalidStructured, err)
		}
	}
	for i, c := range s.Components {
		// The schema requires the key; absent or null is a violation.
		if c.Ingredients == nil {
			return Structured{}, fmt.Errorf("%w: component %d has no ingredients", ErrInvalidStructured, i)
		}
	}
	return s, nil
}
