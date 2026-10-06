# Architecture Decision Records

Numbered, immutable records of architectural rulings. One decision per file.
The convention itself is [ADR-00001](00001-record-architecture-decisions.md) —
read that first.

Numbers are zero-padded to five digits, never reused for a second decision, and
never changed once assigned. A gap in the sequence is normal — a deleted
record's number stays spent. Accepted records are immutable: supersede or amend them, never rewrite them. Nothing has reached production yet, so until the first production deploy a record found wrong may be corrected in place — same number, same file.

Start a new one by copying [TEMPLATE.md](TEMPLATE.md), and add its row to the
index below in the same commit.

## Index

| ADR | Decision | Status |
|---|---|---|
| [00001](00001-record-architecture-decisions.md) | Record architecture decisions | Accepted |
| [00002](00002-single-binary-go-stack.md) | Single Go binary: chi, templ, HTMX, SQLite | Accepted |
| [00003](00003-cloudflare-access-identity.md) | Identity from verified Cloudflare Access JWT; single user | Accepted |
| [00004](00004-recipe-data-model.md) | Recipe data model: components, ingredient lines, step graph, revisions | Accepted |
| [00005](00005-quantities-and-units.md) | Exact quantities, unit dimensions, density conversion, scaling rules | Accepted |
| [00006](00006-rule-first-ingestion.md) | Rule-first ingestion with confidence-gated LLM fallback and mandatory review | Accepted |
| [00007](00007-llm-provider-interface.md) | LLM provider interface with Claude and Gemini | Accepted |
| [00008](00008-ingredient-catalog.md) | Canonical ingredient catalog: curated seed plus USDA FoodData Central | Accepted |
| [00009](00009-shopping-list.md) | Shopping list aggregation, pantry, live view, Todoist push | Accepted |
| [00010](00010-backups.md) | Nightly SQLite snapshot to S3-compatible storage | Accepted |
| [00011](00011-v1-scope-and-deferrals.md) | Scope and deferrals | Accepted |
| [00012](00012-web-subpackage-per-feature.md) | Web layer as a server package plus one subpackage per feature area | Accepted |
| [00013](00013-playwright-e2e-in-node.md) | End-to-end tests with Node Playwright under e2e/ | Accepted |

## What needs one

Anything expensive to reverse, or that a later reader would otherwise have to
reconstruct: schema shape, data ownership, isolation boundaries, authorization
models, and what is deliberately deferred. A recorded deferral is a decision,
not an omission. Implementation detail inside an agreed design does not need
one.

An ADR is also what moves work from uncovered to covered — see
[collaboration](../../.claude/rules/collaboration.md). Until the decision is
written down, the work it authorises is still a conversation.

## Making a decision

One decision per exchange. Verify the claims against the code first, explain
the situation in plain English, argue the options honestly including the
rejected ones, and record the outcome immediately.

Verifying first is not ceremony. Open questions are usually written from
memory, and memory drifts — a decision made on a "fact" nobody checked is a
decision made about a system that does not exist.

Recording immediately is not either. Commit each ADR as it is made, not at the
end of a pass: a session ending with a dozen rulings in an uncommitted file has
produced nothing durable. Where the consequences are large, the ADR lands first
and the consequential edits follow in their own commit. The record is the part
that must not be lost.
