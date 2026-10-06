// Package recipe is the pure domain model for a stored recipe: components,
// ingredient lines with typed qualifiers, equipment, steps and times
// (ADR-00004), built on exact quantities (ADR-00005).
//
// It also validates the step dependency graph, derives the default linear
// order, and derives total time. Everything is values in, values out: no
// database, HTTP or templ. Persistence belongs to the store, parsing to the
// ingestion packages, and the canonical ingredient catalog is referenced here
// by ID only.
package recipe
