# ADR-00058: Every LLM call is recorded with its cost, priced from a daily OpenRouter sync with operator overrides, and shown on an operator page

**Status:** Accepted

**Date:** 2026-11-10

## Context

The operator pays for every LLM call, and with text and vision on separate
providers ([ADR-00056](00056-llm-provider-per-task.md)) there are two bills to
watch. Each call is logged with provider, model, latency and token counts
([ADR-00007](00007-llm-provider-interface.md)), but only someone reading the
logs sees it, and nothing says what it cost or whether calls are failing.

Options considered for showing whether calls succeed:

- **The last call per task, in memory, on the Deployment page.** No
  migration. Rejected by the owner: usage, cost and health belong together on
  a page of their own, and memory holds no history.
- **Every call recorded in the database.** Chosen. One small insert per call
  is nothing at a household's volume.

Prices cannot be hard-coded: they change, and a remembered price is the
plausible wrong value the code-style rule forbids. Options considered for
where they come from:

- **Typed by the operator per model.** No third party, but prices go stale
  without anyone noticing, and an unpriced model shows no cost. Rejected.
- **Tokens only, no cost.** Honest, but drops half of what was asked for.
  Rejected.
- **A daily sync from OpenRouter's public model listing.** Current with no
  effort. Its price for a Claude or Gemini model is OpenRouter's own figure,
  usually the vendor's. A model it does not match has no price. Not enough on
  its own.
- **The sync, with the operator able to set or override a price.** Chosen.
  The override covers what the sync misses or gets wrong.

## Decision

### The call record

Every LLM call, successful or not, is recorded with: when it ran, the task
(text or vision), the provider and model, the household it ran for, input and
output tokens as the provider reports them, latency, the outcome and the
error if it failed, and its cost. Cost is computed when the call is recorded
from the price in force then, and stored in integer millionths of a US
dollar; a later price change does not rewrite past calls. A call with no
price for its model is recorded with no cost, not a cost of zero. Records are
kept indefinitely.
*Amended by [ADR-00059](00059-llm-call-records-deleted-with-household.md): a
household's call records are deleted with the household.*

### Prices

A price is held per provider and model: input and output per million tokens,
with its source (`openrouter` or `manual`) and when it was last synced.

Once a day, and on demand from the page, gobbler fetches OpenRouter's public
model listing (`https://openrouter.ai/api/v1/models`) without credentials and
updates the price of every model a task has used or is set to use. A model
matches only the OpenRouter entry whose id is exactly the provider's prefix
(`anthropic/` for Claude, `google/` for Gemini) followed by the model id
gobbler uses; there is no other normalisation, so a price is never taken from
a different model. A model with no exact match is left unpriced until the
operator sets its price. A price
the operator has set is never overwritten by the sync. A failed sync is
logged and leaves every price as it was; the page shows when the last sync
succeeded and the last failure.

### The page

An operator-only usage page, separate from the Deployment page, shows over a
chosen window: total cost, number of calls, tokens in and out, and the share
of calls that failed; the same broken down by task and model and by
household; and the most recent failures with their errors. Calls with no
price are counted and shown as unpriced, never folded into the total as
free. Prices are listed and set on the same page.

## Consequences

OpenRouter becomes an outbound destination. It is fetched, not loaded by a
browser, and sends nothing about gobbler or its users, so the AGENTS.md
boundary on trackers and remotely loaded scripts is not engaged.

ADR-00007's per-call log line stays; the record is in addition to it.

**Obligations:**

1. The call record and the price table are added by migration.
2. A test shows a successful and a failed call each produce a record, the
   failed one with its error, and that ingestion still behaves as ADR-00007
   says when recording fails (the failure is logged; the ingest is not
   failed by it).
3. Tests show cost is computed from the price in force, that an unpriced
   model's call has no cost rather than zero, and that changing a price later
   leaves recorded costs unchanged.
4. Tests against a recorded OpenRouter listing show the sync updates a synced
   price, leaves a manual one alone, on a failed fetch changes nothing and
   records the failure, and leaves unpriced a model whose id matches an entry
   only after a change beyond adding the provider prefix.
5. An e2e test reaches the usage page as an operator, and a test shows a
   non-operator is refused.
6. `docs/operations.md` names OpenRouter as an outbound destination and says
   what is fetched and how often. `docs/design.md`'s package table gains the
   package that owns the record and prices.
