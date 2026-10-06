// Package parse turns recipe text into structured values with rules alone
// (ADR-00006): the ingredient-line parser (IngredientLine) and the duration parser
// (Duration, FindDurations).
//
// Everything here is pure and deterministic: the same input and the same
// parser version produce the same output. A line or duration the rules cannot
// fully read comes back as a low-confidence result, never as an error, so one
// hard line cannot abort an ingest. Ingredient matching goes through the
// IngredientMatcher interface, which the catalog satisfies at the call site;
// this package imports neither the catalog nor the database.
package parse
