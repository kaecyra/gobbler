# ADR-00059: A household's LLM call records are deleted with the household

**Status:** Accepted

**Date:** 2026-11-11

## Context

[ADR-00058](00058-llm-usage-and-cost.md) records every LLM call with the
household it ran for and keeps the records indefinitely, so the operator's
usage page can show what the LLM has cost.
[ADR-00018](00018-households-as-tenants.md) says that deleting a household
removes all of the household's data. Read literally, the two disagree about
a deleted household's call records.

Options considered:

- **Keep the records and drop the household link.** The cost total keeps
  matching what the operator paid. A record without its household still
  holds the model, tokens, cost and error of a call made for that household.
  Rejected by the owner: the call was made on the household's behalf, so it
  is the household's data and goes with it.
- **Delete the records with the household.** Chosen.

## Decision

Deleting a household deletes every LLM call record made for it. "Kept
indefinitely" in ADR-00058 means no record is removed for its age. It does
not exempt records from household deletion.

After a household is deleted, the usage page's totals, all-time included, no
longer count that household's calls. They then understate what the operator
was billed. This cost is accepted. The provider's own billing remains the
record of what was paid.

## Consequences

This amends ADR-00058's Decision, "The call record": records are kept
indefinitely except that a household's are deleted with it.

**Obligations:**

1. ADR-00058 gains a note under "The call record" pointing to this ADR.
2. A test deletes a household that has call records and shows that its
   records are gone and that records with no household remain.
