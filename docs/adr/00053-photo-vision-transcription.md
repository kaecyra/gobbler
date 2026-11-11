# ADR-00053: A photo is transcribed by a vision model, structured by the rules, and checked for numbers against tesseract

**Status:** Accepted

**Date:** 2026-11-11

## Context

[ADR-00006](00006-rule-first-ingestion.md) reads a photo with `tesseract` and
sends the text through the same parser, score and fallback as pasted text. The
score measures whether a line parses, not whether OCR read it right.
A misread that still parses cleanly, such as `½ cup` read as `4 cup`, `1 tsp`
read as `l tsp`, or a two-column page whose ingredients and steps interleave,
scores high, so the fallback never runs. When it does run,
[ADR-00020](00020-ingest-step-graph-gap-fill-review.md) gap-fill asks only about
lines below the threshold, so the vision model never sees the confidently wrong
ones, and the review screen shows a wrong draft with nothing flagged.

Options considered:

- **Keep `tesseract` first.** Rejected by the owner: the misreads are where the
  errors come from.
- **A vision "layout" pass, then a text model.** The first call lists the
  page's columns and sections and the second structures the text. Rejected: the
  second call cannot see the photo and works from `tesseract` text, which is
  the source of the misreads, and it hands structuring to the LLM, which
  ADR-00006 keeps deterministic.
- **Vision structures the photo directly.** Rejected for the same reason:
  structuring belongs to the rules, and the structured result would have no
  source text to store, replay or compare.
- **Vision transcribes, the rules structure, `tesseract` checks the numbers.**
  Chosen by the owner.

Options considered for what is stored:

- **The transcript alone.** Rejected by the owner: a replay of the snapshot
  (preview, re-import, reparse, [ADR-00048](00048-agent-driven-import.md))
  would reproduce the parse but not the number-check flags, so the same input
  and parser version would give a different draft.
- **The transcript and the `tesseract` text.** Chosen by the owner.

## Decision

When an LLM provider is configured for vision ([ADR-00007](00007-llm-provider-interface.md),
[ADR-00056](00056-llm-provider-per-task.md)), a photo is ingested as follows.
With none configured, ingestion is as [ADR-00006](00006-rule-first-ingestion.md)
states: `tesseract` alone, and no provider call.

1. **Vision transcribes.** The provider is sent the photo and asked for a
   word-for-word transcription in reading order: columns untangled, headings
   kept, fractions and temperatures exactly as printed. It must not interpret,
   convert, reword or structure anything. The request carries no schema.
2. **The rule parser structures the transcript, unchanged.** The transcript is
   the source snapshot of kind `ocr`, so a replay of the snapshot is
   deterministic.
3. **`tesseract` checks the numbers.** `tesseract` still runs on the photo. A
   number in a transcribed ingredient line that appears nowhere in the
   `tesseract` text lowers that line's confidence below the threshold. Numbers
   are compared by value, so `½`, `1/2` and `0.5` agree, a range is its two
   ends, and a temperature is its digits. Gap-fill then treats the line as a
   gap and sends it with the photo to the vision model, as ADR-00020 describes.
   Gap-fill is unchanged.
4. **The `tesseract` text is stored as a snapshot kind of its own,
   `ocr_check`,** beside the transcript, on the same terms as every other
   snapshot: added with the recipe, never modified. Draft, preview, re-import
   and reparse replay the pair, and the same pair gives the same flags.
5. **A transcription that fails or times out is logged, and the draft is built
   from the `tesseract` text**, with no `ocr_check` snapshot. The draft is still
   reviewable, so the degraded path is useful to the person. A transcript with
   no text is a failure.
6. **A photo over the size the provider accepts for gap-fill is not
   transcribed:** `tesseract` alone, logged, as the gap-fill already does.

## Consequences

The LLM now runs on a photo before the rules do. This narrows two rulings.
ADR-00006 obligation 2 ("the LLM is called only when the score is below the
threshold or the user asks") does not cover the transcription, which is called
on every photo when a provider is configured. The AGENTS.md boundary that the
LLM never runs before the rule-based path has been tried is reworded to allow
it. What stays: the transcript is stored text, the rules structure it, the
LLM's output never changes a recipe without the review screen, and a draft is
reproducible from its stored snapshots.

A photo costs one vision call more than before, whatever it scores. A page
`tesseract` cannot read at all still fails the ingest as before, because the
number check needs its text.

A draft holds one snapshot slot, so a held photo draft carries the `tesseract`
text inside its draft document, and confirming it writes the `ocr_check`
snapshot with the recipe. A share of a recipe copies one snapshot and does not
carry the `ocr_check`.

**Obligations:**

1. `source_snapshots.kind` admits `ocr_check` by a migration, and the store's
   `SnapshotKind` and its `Valid` accept it. A migration test shows the kind
   accepted after Up, an unknown kind refused, and rows surviving Up, Down and
   Up.
2. `ingest.Provider` has a transcription call, implemented by `internal/llm/claude`
   and `internal/llm/gemini` and covered by the `internal/llm/llmtest`
   contract; the model is read from configuration only (ADR-00007).
3. A test with a stub provider proves a photo draft's snapshot is the provider's
   transcript and that the rule parser built the draft from it.
4. A table test proves a transcribed line whose number `tesseract` did not read
   is below the threshold, for `½` against `1/2`, unicode fractions, ranges and
   temperatures.
5. Tests prove that with no provider the photo path makes no provider call, that
   a transcription failure is logged and the draft is built from the `tesseract`
   text, and that a photo over `maxVisionImageBytes` is not transcribed.
6. A test proves replaying the stored transcript and `tesseract` text gives the
   same line confidences, and that saving a photo draft, held or direct, stores
   both snapshots.
7. ADR-00006's status line gains "Amended by" this ADR, and its Pipeline steps
   1 and 5, its rejection of vision-first for photos and its obligation 2 carry
   an amendment note pointing here.
8. AGENTS.md "Boundaries" rewords the LLM boundary to allow the transcription.
9. `docs/design.md`'s runtime diagram and its W5 row show vision transcription
   with `tesseract` as the check.
10. The e2e suite covers the transcribed path with a stub provider alongside the
    stub `tesseract`.
