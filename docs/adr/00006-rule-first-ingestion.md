# ADR-00006: Rule-first ingestion with confidence-gated LLM fallback and mandatory review

**Status:** Accepted

**Date:** 2026-10-06

## Context

Getting a recipe into gobbler must be easy and repeatable. Inputs are a URL, a
block of pasted text, or a photo of a cookbook page. The owner wants ingestion
to be more or less deterministic: the same input produces the same structure,
and a model is used only when rules are not good enough.

Most recipe sites embed a schema.org `Recipe` object as JSON-LD, which gives
name, author, times, yield, ingredient strings and instructions without
scraping. The ingredient strings still need parsing into the model of
ADR-00004 and ADR-00005.

Options considered:

- **LLM-first: send every input to a model.** Fast to build, but not
  deterministic, costs money per recipe, and makes a remote service necessary
  for a basic feature. Rejected.
- **Rules only.** Deterministic, but handwritten or unusual recipes and photos
  would fail outright. Rejected.
- **Rules first, scored, with an LLM fallback.** Chosen.
- **For photos, LLM vision first.** Rejected in favour of local OCR first, for
  the same determinism and cost reasons; vision is the fallback.

## Decision

### Pipeline

1. **Acquire.** URL: fetch the page. Text: as given. Photo: run `tesseract`
   OCR locally to get text.
2. **Extract** (rule-based):
   - URL: JSON-LD `Recipe`, then microdata, then site-specific extractors for
     sites that publish neither. If none match, treat the page's main text as
     pasted text.
   - Text: a sectioning parser finds title, ingredient blocks (including
     component headings such as "For the sauce:"), and steps.
3. **Parse** each ingredient line with the rule-based line parser into
   quantity, unit, ingredient, qualifiers and optional flag. Parse durations
   from time fields and step text.
4. **Score.** Every field and every line gets a confidence; the recipe gets an
   overall score. Unrecognised units, unmatched ingredients, leftover tokens
   and missing required fields lower it.
5. **Fallback.** Below a configured threshold, the pipeline calls the
   configured LLM provider (ADR-00007) with the source and the rule-based
   draft, asking for the same structure. For photos, the fallback may send the
   image itself to a vision-capable model. The review screen also offers a
   manual "retry with LLM" for any draft.
6. **Review.** The draft, rule-based or LLM-assisted, is shown on a review
   screen with low-confidence items highlighted and LLM-supplied values marked.
   Nothing is saved until the user confirms.
7. **Save.** The recipe is written with its source snapshot (ADR-00004).

Steps 2–4 are deterministic: the same input and the same parser version produce
the same draft. Parser changes are covered by a corpus of real inputs with
expected outputs.

## Consequences

The ingredient-line parser and its test corpus are the core of the project and
need sustained attention. Sites without structured data will depend on the
text parser or the fallback.

**Obligations:**

1. No code path saves an ingested recipe without the review step.
2. The LLM is called only when the score is below the threshold or the user
   asks; a test proves a high-confidence input makes no provider call.
3. The ingredient-line parser has a golden-file corpus of real lines with
   expected structured output, run in the test suite.
4. LLM-supplied values are distinguishable from rule-parsed values on the
   review screen.
5. The threshold is configuration, not a constant.
