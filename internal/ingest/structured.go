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
// non-blank name, no blank equipment name, at least one component, and an ingredients key on every
// component. Anything else is an error wrapping ErrInvalidStructured, never a
// partial Structured. Every provider calls it; none keeps its own copy.
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
	for i, c := range s.Components {
		// The schema requires the key; absent or null is a violation.
		if c.Ingredients == nil {
			return Structured{}, fmt.Errorf("%w: component %d has no ingredients", ErrInvalidStructured, i)
		}
	}
	return s, nil
}
