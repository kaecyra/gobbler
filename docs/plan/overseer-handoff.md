# Overseer handoff — 2026-10-06

Volatile state the graph does not hold. `docs/plan/graph.json` is the durable record.

## Merged (squash SHA, PR)

| Node | Issue | PR | SHA |
|---|---|---|---|
| w0-scaffold | #1 | #40 | 8ff9646 |
| w0-config | #2 | #41 | 1bdb5be |
| w0-db | #3 | #44 | df5c56e |
| w1-quantity | #8 | #42 | 9cedb95 |
| w1-recipe-model | #9 | #45 | b9551f6 |
| w0-auth | #4 | #46 | 7883a7b |
| w2-line-parser | #12 | #47 | ce0de1e |
| w2-text-sectioning | #13 | #49 | 32204e6 |
| w1-catalog | #10 | #48 | 19686d9 |

Dispatcher PR #43 (shared append-only paths switched to `open` mode) merged. `VERSION` on main is 0.9.0.

## In flight, in review, worktrees

None. No worktrees on disk; no open worker PRs. Nothing dispatched is unaccounted for.

## Migration numbers handed out

`schema`: `00001` to w1-catalog (merged). Next is `00002`.

## Blocked

- l-meal-planning (#38), l-invitations (#39): deferred by ADR-00011, need their own ADR (recorded in the graph).

## Things a fresh dispatcher must know

- **`make` is not installed in the container.** Run gates on the host via exec-daemon: `make fmt-check`, `make test`.
- **Lint gate:** the container's golangci-lint misses revive findings CI catches (e.g. `time-naming` unit suffixes); the host's Homebrew golangci-lint is 2.13.2 and `make lint` refuses it by design. Use on the host, in the worktree: `GOTOOLCHAIN=go1.26.4 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run ./...`.
- **VERSION churn:** every source PR bumps `VERSION`, so each merge invalidates the other open branches; the worker rebases and re-bumps before the next merge.
- **Overseer plugin defect (outside this repo):** `partition_by_lane` in graph-core exempts only `open` shared files, while graph-node's docs say any shared file is exempt. Worked around in #43; switch the three entries back to `append_only` when the plugin honours it.
- Worker contract common part lived in the session scratchpad; a fresh session re-derives it from `conduct/worker_contract.md` plus the notes above.
- `node.ts status <id> in_review` can race GitHub's closing-issue link right after `gh pr create`; wait a few seconds and retry.

## Follow-ups to carry into later nodes

- **w1-store (#11):** must register `catalog.OnIngredientMerge` hooks for every table referencing `ingredients(id)` (plain FK, no `ON DELETE CASCADE`); see PR #48 body.
- **w2-ingest-pipeline (#15):** unit spellings currently live in `internal/parse/line_units.go`; the catalog also stores unit aliases. Give unit aliases one owner when wiring the matcher. Line and section confidence share one shape (float in [0,1], `Issues []string`).

## Next three to dispatch, and why

1. **w0-web-shell (#5)** — next on the serial foundation track; unblocks the e2e harness, container and every UI node.
2. **w1-store (#11)** — now dispatchable (catalog and recipe-model merged); critical path to ingestion, search and every recipe UI node. Needs a `schema` migration number (next is 00002).
3. **w2-structured-extract (#14)** — depends only on w1-recipe-model (merged); independent lane, keeps the ingestion track moving.
