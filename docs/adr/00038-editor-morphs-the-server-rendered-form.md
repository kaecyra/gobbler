# ADR-00038: The recipe editor morphs the server-rendered form in place

**Status:** Accepted

**Date:** 2026-11-05

## Context

The editor (ADR-00020) is rendered by the server. Every structural action (add,
remove or move a part, line, qualifier, step or equipment) posts the whole form,
and the server answers with the whole form again. That keeps one renderer:
templ draws every row, validation and catalog matching stay in Go, and the rows
that depend on other rows (each step's "Starts after" list, the part counts, the
count of items that need a look) are always drawn correctly. The form works
without scripts.

The answer replaced the form's DOM. Replacing it threw away everything the
browser was holding: the scroll anchor, so the page jumped by about a screen;
which rows were open; focus and caret. Each loss was being restored by hand in
script, and each restoration left a visible flicker or a lost state.

Options considered:

- **Replace the form, and restore scroll, open rows and focus by script.**
  This is what shipped first. Rejected: it fights the browser, and any state not
  restored by hand is lost.
- **Do local edits in the browser** by cloning rows rendered as templates, and
  post only on save. Instant, with no swap at all. Rejected for now: the rows
  that depend on other rows would need a second renderer in script, or would be
  wrong until save.
- **Morph the returned form into the page** with idiomorph's htmx extension.
  Nodes that persist keep their state. Chosen.

## Decision

- A structural action still posts the whole form, and the server still renders
  the whole form. The client morphs the answer into the page instead of
  replacing it.
- The morph is idiomorph's htmx extension, vendored unmodified under the
  embedded static assets with a note of its version, source and checksum, like
  every other frontend asset (AGENTS.md, Boundaries).
- Which rows are open is the browser's state. A morph never closes a row the
  person opened; the server may still open one, such as a row just added.
- Without scripts the form posts and the page reloads, as before.

## Consequences

The editor's script no longer saves and restores the scroll position or the
open rows. Doing local edits in the browser stays open as a later speed-up if
the round trip is ever too slow on a tablet; it would need its own record.

**Obligations:**

1. The editor form declares the morph swap, and the editor page loads the
   vendored extension before the editor's script.
2. An end-to-end test samples the scroll position on every frame across a
   structural action and fails on any movement over 4px, including one that
   is scrolled back afterwards.
