# Overseer handoff — 2026-10-12, round 4

Volatile state the graph does not hold. `docs/plan/graph.json` is the durable record. `VERSION` on main is 0.27.0.

## Merged this session (squash SHA, PR)

| Node / change | Issue | PR | SHA |
|---|---|---|---|
| w2-review-screen | #16 | #80 | 136c1aa |
| `.gitignore` version-gate exemption, `.env` ignored | — | #77 | e0628d8 |
| w3-recipe-view | #18 | #81 | 5410c86 |
| ADR-00016 cross-origin protection, w3-csrf node | #83 | #87 | d08290c |
| w7-ops-doc | #37 | #84 | a39008d |
| w2-sqlc (closes #55, #59) | #60 | #79 | 66c3415 |
| lanes widened: w5-llm-fallback, w6-meanwhile | — | #89 | 06057ee |
| w6-meanwhile | #32 | #90 | 5117f8a |
| w5-llm-fallback | #29 | #91 | 74c279e |
| w3-csrf | #86 | #92 | 682dc99 |

#82 (round-3 handoff) was closed, superseded by this one.

## Open PRs

None. The merge queue is empty.

## Owner decisions this session

- CSRF: option B. ADR-00016 records it: stdlib `http.CrossOriginProtection` plus Access cookie SameSite=Lax.
- The owner merged the lane PRs #87 and #89 (escalation trigger 2).

## First thing next round: sqlc lanes (uncovered, needs a graph decision)

Dispatchable now: w3-search-list (#17), w4-shopping-store (#24), w3-unit-owner (#65), w3-store-atomic-ingest (#66). Their lanes predate sqlc (ADR-00014):

- They need `internal/store/queries/**`, plus `internal/catalog/queries/**` for unit-owner. No live node owns these.
- Any schema or query change regenerates `internal/store/storedb/**` or `internal/catalog/catalogdb/**`. That is generated code shared by every store node.

Options:
- Make `*/queries/**` an `append_only` shared file (one new `.sql` file per node), and `*db/**` `single_writer_per_round`. Then two nodes touching the same generated package never run in one round.
- Or serialise these four nodes.

Either way it goes through a dispatcher PR and the owner (it is a lane change). search, shopping-store and unit-owner each also need a `schema` migration: next is `00003`, and the sequence is contiguous, so assign at dispatch, one per round, or in merge order.

## Issues filed this session (no node unless stated)

- #85: equipment has no size, so ADR-00005's pan hint cannot apply. #81 removed the hint. Needs a decision and a migration.
- #88: the proposed-ingredient recompute is duplicated in `internal/web/ingest/edit.go`. Export it from `internal/ingest`.
- #93: **flaky `TestConcurrentMergesAndEditsDoNotFailBusy`**. `MergeIngredients` still hits SQLITE_BUSY after #79. It is a real data-integrity defect and a flaky gate that blocks random PRs. **Top priority**: needs a node (owner of `internal/catalog/**` / `internal/db/tx*.go` work).
- #78 (from round 3): a scripted check that the image has no Node and no `e2e/`. Still needs a node.

## Known out-of-lane leftovers

- `internal/web/ingest/edit.go` leaves `FromLLM` set after the user edits an LLM value (#91 worker). It belongs with #88's work.

## Worktrees on disk

None.

## Migration numbers

`schema`: `00002` was the last used. Next is `00003`. None were handed out this session.

## ADR numbers

The sequence is at `00016` (corrected this session: 00014/00015 had been written without drawing from it).

## Things a fresh dispatcher must know

- **Rebase dispatcher branches with cherry-pick, not `git rebase`.** A `git rebase` of a dispatcher branch left the sequencer in a broken state (the same commit listed as done and pending). Cherry-picking onto `origin/main` worked cleanly. Bookkeeping commits on main touch `graph.json`, so every open graph PR needs this before merge.
- **VERSION churn.** Every merge moves VERSION. Merge one PR at a time and re-bump the next through its worker. Do not hand out numbers ahead (lessons.md).
- **Host plugin path:** `$ODETTE_SCRIPTS_HOST` is not set in the host shell. Use `/Users/timothygunter/.odette/base/.marketplace-cache/market/plugins/overseer/...`.
- **Gates on the host:** `make fmt-check test test-scripts e2e-routes e2e version-check generate-check`, then pinned lint `GOTOOLCHAIN=go1.26.4 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run ./...` and `go vet ./...`.
- **Worktree setup** (still not in config): `go mod download && cd e2e && npm ci && npx playwright install chromium`, on the host. Give each concurrent worker distinct `GOBBLER_E2E_PORT` / `GOBBLER_E2E_ACCESS_PORT`.
- **Lanes:** read each issue's acceptance criteria against its node's `owns` before dispatch. Three nodes this session needed widening first.
- **Narrow fixes:** verify the diff yourself and post a `<!-- self-review-verdict: APPROVED -->` COMMENT review instead of another full round.

## Next to dispatch, and why

1. **A node for #93.** A flaky gate taxes every PR, and a BUSY merge silently keeps both ingredients.
2. **The sqlc-lane graph PR** (above), then w3-store-atomic-ingest (#66, no migration) and w3-search-list (#17, `00003`).
3. **Unblocked now that w3-csrf has merged:** w3-editor (#19), w3-history (#20), w3-journal (#21), w3-catalog-page (#22), w7-settings (#35). Check each lane against its issue first.
