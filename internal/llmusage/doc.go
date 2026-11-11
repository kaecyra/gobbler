// Package llmusage records every LLM call with its cost, keeps the price of
// each model, syncs those prices daily from OpenRouter's public model listing,
// and reports usage over a window for the operator's usage page (ADR-00058).
//
// A call's cost is computed when it is recorded, from the price then in force,
// and stored in integer millionths of a US dollar; a call whose model has no
// price is recorded with no cost, never a cost of zero. A price the operator
// set is never overwritten by the sync. The SQL is in queries/, generated into
// llmusagedb (ADR-00014).
package llmusage
