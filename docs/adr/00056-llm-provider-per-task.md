# ADR-00056: Text and vision each name their own LLM provider, set on the operator's page and stored in the database

**Status:** Accepted

**Date:** 2026-11-10

## Context

[ADR-00007](00007-llm-provider-interface.md) made the provider and the text
and vision models configuration, and in practice that configuration is the
environment: one provider for every task, with changes needing an edit to the
server's environment and a restart. Text and vision cannot come from different
providers, and nothing in the app shows what is in use. Photo ingest is
becoming a vision transcription (#606), which makes the vision
model's choice matter on its own.

Options considered for how many providers:

- **One provider for every task.** Rejected: it prevents choosing the better
  or cheaper model per task.
- **One provider per task.** Chosen. ADR-00007 already keeps provider
  implementations interchangeable, so mixing them needs no change to either.
- **An ordered list per task, tried in turn on failure.** Rejected. A failed
  LLM call already leaves the rule-based draft with its error shown
  (ADR-00007) and a manual retry with the LLM
  ([ADR-00020](00020-ingest-step-graph-gap-fill-review.md)). Failing over costs a full
  timeout before the second provider starts, makes a draft's style depend on
  vendor uptime, and needs rules for which failures count and a mark on the
  review screen naming who answered. Per-task settings can grow into a list
  later without undoing this.

Options considered for where the settings live:

- **The environment, as today.** Rejected by the owner: a change needs shell
  access and a restart, and the operator cannot see what is set.
- **The operator's page, stored in the database.** Chosen.

## Decision

### Providers

There are two LLM tasks, **text** and **vision**. Each has its own provider
(`claude` or `gemini`) and model identifier, chosen independently. A task with
no provider set has no LLM, as an unset provider means today.

### Where the settings live

The provider and model for each task, and the call timeout, are set by an
operator on the operator's Deployment page and stored in the database. A
change takes effect on the next LLM call, without a restart. Model
identifiers remain configuration, never code (ADR-00007): the database is
where that configuration now lives.

API keys, the encryption that protects them and the removal of the
`GOBBLER_LLM_*` environment variables are
[ADR-00057](00057-integration-settings-in-the-database.md).

### Choosing a model

The operator picks each task's model from a list the chosen provider's
model-listing API returns, called with the stored key. There is no free-text
entry. The list is narrowed to what the task can use, from what each provider
reports: for Claude, active models whose listing marks structured output as
supported, and for vision also image input. gobbler's Claude provider
constrains its answer with a tool call, and the listing reports nothing about
tool use, so structured output is the nearest capability it does report and
stands in for it; for Gemini, models that support content generation,
since Gemini reports no separate image-input flag. If the list cannot be
fetched, the page says why and the saved model stays as it is. A saved model
the provider no longer lists is shown as such, so it can be replaced.

Two options were rejected. Free text works for any model with nothing to
build, but a typo surfaces only on the first ingest that fails. Free text
checked by a test call at save catches the typo, but the owner prefers
choosing from what the provider offers.

### What the Deployment page shows

The Deployment page shows each task's provider and model and whether each
key is set. How the LLM is being used, what it costs and whether its calls
succeed are shown on a page of their own
([ADR-00058](00058-llm-usage-and-cost.md)), not here.

## Consequences

This amends ADR-00007's Decision: provider and model are chosen per task, and
the configuration that holds them is the database, set from the operator's
page.

**Obligations:**

1. ADR-00007 gains a note at its Decision pointing to this ADR.
2. Ingestion selects the text provider for text calls and the vision provider
   for vision calls; a test configures Claude for one and Gemini for the
   other and shows each call reaches the provider its task names.
3. A setting saved on the Deployment page is used by the next LLM call
   without a restart, shown by a test.
4. Tests show the model list for each provider is narrowed as above from a
   recorded listing, that a failed listing leaves the saved model unchanged,
   and that a saved model missing from the listing is marked.
5. `docs/design.md`'s configuration table says the LLM settings live on the
   Deployment page, per task.
