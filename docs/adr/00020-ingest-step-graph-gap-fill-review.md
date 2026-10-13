# ADR-00020: Ingest infers the step graph by rules then LLM, the LLM fills only gaps, and review reuses the editor

**Status:** Accepted

**Date:** 2026-10-13

## Context

A read of the ingest pipeline against [ADR-00006](00006-rule-first-ingestion.md)
found three behaviours no record decides:

- **Step graph.** [ADR-00004](00004-recipe-data-model.md) gives steps
  dependencies and an `active` / `passive` flag, and derives total time and
  "meanwhile" guidance from them. Ingest produces neither: steps arrive in
  reading order, independent of one another, so an ingested recipe has no
  critical path until someone builds the graph by hand. Options considered:
  rules only (deterministic, misses non-obvious parallelism); the LLM only
  (best on messy text, but every ingest that wants a graph pays for a call,
  against ADR-00006's rule-first stance); the editor only (no new code, no
  graph until a person draws one); rules first with the LLM supplying
  dependencies when the fallback runs.
- **What the fallback replaces.** ADR-00006 step 5 asks the LLM "for the same
  structure", and the whole rule-based draft is replaced by its answer. That
  discards rule-based lines and fields that scored well. The alternative sends
  only the weak parts and merges the answer, keeping the deterministic result
  wherever it was good enough, at the cost of a more involved prompt and merge.
- **What review can edit.** The review screen corrects only a few fields;
  amounts, units, qualifiers, durations, components, equipment and whole lines
  can be changed only after saving, in the recipe editor. Options considered: a
  separate full editor on the review screen (rejected: a second implementation
  of the editor), a "save, then open in the editor" path (rejected: a recipe
  with wrong amounts or units is saved and enters search and shopping lists
  before it is fixed), or only the commonest fixes (rejected: each further fix
  would be built twice, once here and once in the editor). The owner's ruling
  is that review reuses the editor itself.

## Decision

1. **Step graph: rules, then LLM.** The rule-based pass builds the graph: a
   linear chain in reading order by default; steps marked `passive` by a fixed
   vocabulary (bake, rise, prove, rest, chill, marinate, simmer and similar);
   the chain broken where a step's text says it runs alongside another
   ("meanwhile", "while the …"). A step's graph is *cue-backed* when a rule
   matched for it (a passive-vocabulary word or a parallel cue) and *default*
   otherwise. When the LLM fallback runs, it is asked for the dependencies and
   passive flags of every default step, and its answer replaces those only;
   cue-backed steps keep the rule result. Both are shown on review.
2. **The fallback fills gaps.** The LLM is sent the source and only the
   low-confidence lines and missing or low-confidence fields, and its answer
   replaces exactly those. Every rule-based value that met the threshold is
   kept. This amends ADR-00006 step 5. The manual "retry with LLM" keeps its
   meaning: it runs the same gap-filling call regardless of the overall score.
3. **Review reuses the editor.** The review screen is the recipe editor
   operating on the unsaved draft: the same components, the same form handling
   and the same validation, with confirm in place of save. Nothing on the review
   screen is a second implementation of an editor control.

## Consequences

ADR-00006 step 5 now reads as decision 2 above; steps 2–4 stay deterministic,
and the graph inference in decision 1 is part of them, so it is covered by the
same versioned corpus.

**Obligations:**

1. Ingest sets step dependencies and passive flags by rule, covered by corpus
   cases for a linear recipe, a "meanwhile" recipe and a passive step (#190).
2. The LLM structured schema carries step dependencies and passive flags; a
   test shows a fallback answer changes the graph of a default step and leaves
   a cue-backed step's graph unchanged (#190).
3. The fallback request contains only the below-threshold lines and fields; a
   test shows a high-confidence rule-based line is unchanged after a fallback
   whose answer differs for it (#191).
4. The review screen renders and submits through the editor's components and
   form handling; a test edits an amount, a unit and a step dependency on a
   draft before confirm (#192).
5. ADR-00006 step 5 carries an amendment note pointing here.
