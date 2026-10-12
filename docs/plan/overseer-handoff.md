# Overseer handoff: 2026-10-12, round 5

This file holds the volatile state the graph does not. The durable record is `docs/plan/graph.json`. `VERSION` on main is 0.29.0.

## Merged this round (squash SHA, PR)

| Node / change | Issue | PR | SHA |
|---|---|---|---|
| Graph: w3-merge-busy node; photo-ocr, cook-mode and settings lanes | — | #96 | 7651122 |
| w3-journal | #21 | #97 | 0a2f2c0 |
| Graph: sqlc lanes (owner option A), append-only queries, single-writer generated code | — | #101 | 5059fc8 |
| Fix: ingest e2e specs no longer depend on run order (photo-ocr lane) | — | #105 | cba7827 |
| w3-merge-busy (in-process write queue in `db.WithTx`) | #93 | #103 | d23e4e3 |
| w6-cook-mode, plus the `internal/recipe/display` extraction | #33 | #100 | 16fc2b7 |
| Graph: photo-ocr lane narrowed to the pipeline wiring | — | #102 | b9a66a1 |

## Open PRs

- None besides this handoff. #106 (the w3-store-atomic-ingest lane, plus `internal/web/ingest/ingest_test.go`) merged at 65e25f6, so #66's ownership check now passes on main.

## In flight (worktrees on disk)

| Worktree | Node | State |
|---|---|---|
| `wt-20` | w3-history (#20) | **Worker done at 9426dcc**: rebased, using `internal/recipe/display`, e2e port 38471, VERSION 0.30.0, gates green. Not yet verified, pushed or opened as a PR. Behaviour change: to-taste lines now read "... to taste", from the shared line builder. A time-rows helper is still local to history; `display` has none. |
| `wt-30` | w5-photo-ocr (#30) | **Worker done at 06b6a45**: rebased, VERSION 0.30.0, gates green (71 e2e passed), failing proofs reported. Not yet verified, pushed or opened as a PR. Decisions to check in review: (1) a separate `PhotoDrafter` interface instead of extending `Drafter`, because `fakeDrafter` lives in `ingest_test.go` outside the lane; (2) each pending draft holds its photo in memory, up to 10 MB × 20 drafts, about 200 MB worst case. Consider a lower cap. |
| `wt-66` | w3-store-atomic-ingest (#66, plus #104) | **Worker done at 0d45c69**, gates green, failing proofs reported. It covers `UpdateIfVersion` (409 `ErrStaleVersion`), `CreateWithSnapshots`, single-call `Save`, and `Delete` inside `withTx`. Not yet verified, pushed or opened as a PR. Its ownership check passes only after #106 merges. VERSION says 0.29.0, which equals main, so the worker must re-bump to the next free minor before merge. |
| `wt-65` | w3-unit-owner (#65, migration 00003) | Committed at 3e42aa2 (`refactor`, 0.28.3, needs a re-bump). **Blocked**, see below. |

VERSION collides: history, photo-ocr and atomic-ingest all claim the next minor. Merge one at a time and re-bump each through its worker (lessons.md).

## Blocked, with the reason

- **w3-unit-owner (#65).**
  - **Tests:** two tests expect `bag` to be a proposed unit, `internal/ingest/pipeline_test.go:101` and `internal/ingest/save_test.go:98`, and #65 adds `bag` to the catalog. `pipeline_test.go` is in photo-ocr's lane and `save_test.go` in atomic-ingest's; both are in flight.
  - **Decision:** owner item `ib-acf65b06cee6`. A: drop proposed units (recommended). B: keep them.
  - **Then:** once #30 and #66 merge, widen #65's lane to those two test files (or the dead proposed-units code, if A) and resume it.
  - **Stale comments** the worker reported: `internal/ingest/resolve.go:66-70`, `draft.go:94-96`, `fallback.go:204`, `assemble.go:82`, `ingest.templ:157-169`, `save.go:181`.
- Record the block in the graph with `node.ts blocked w3-unit-owner "<reason>"` if it outlives this round.

## Migration and ADR numbers

- `schema`: `00003` went to w3-unit-owner, which has not merged. The next free number is `00004`.
- The ADR sequence is at `00016`.

## Issues filed this round

- #98: recipeview should embed the journal, link history and clone, and show the clone origin.
- #99: a shared e2e seed helper (recipeview, journal, cook and history seeds all duplicate it).
- #104: `store.Delete` outside the write gate. Folded into w3-store-atomic-ingest's note.

## Owner decisions this round

- **New standing authority** (item `ib-613cab711dc7`): the dispatcher may merge its own graph PRs that widen a lane **onto files no live node owns**, once they are review-clean, and must report each one. A widening that touches a live node's lane still needs the owner (escalation trigger 2).
- **#65 proposed units: option A** (item `ib-acf65b06cee6`). Drop proposed units, and flag unknown units on review. The dead `ProposedUnits` code, the review-screen section, the unit creation on save, and the two `bag` tests are removed once #66 and #30 merge. Widen #65's lane onto those files under the new standing authority if no live node owns them by then.

- sqlc lanes: option A (#101). Queries stay per package. Each node adds its own `.sql` file. The generated `storedb` and `catalogdb` have one writer per round, and the graph note names the writers.
- The owner merged #96 and #102 and granted the #101 merge.

## UI mockup (not in the repo)

The claude.ai artifact https://claude.ai/artifact/Maaf6isAd66jbzvgmtkz1g is at v9. The owner's choices are recorded in `.memory/gobbler-mockup.md`. Adopting it needs a design ADR plus new recipe fields: meal type, spice level, and stage kinds (ADR-00004 only has active/passive). That is uncovered, so discuss it with the owner before writing the ADR.

## Things a fresh dispatcher must know

- **e2e ports collide across concurrent worktrees.** Specs that start their own server use fixed default ports: recipeview 38431, retry 38441, journal 38451, cook 38461, history 38471, photo 38481. Give every concurrent worker distinct override values (`GOBBLER_E2E_<NAME>_PORT`) as well as `GOBBLER_E2E_PORT` and `GOBBLER_E2E_ACCESS_PORT`.
- **Rebasing dispatcher branches:** a bookkeeping commit on main conflicts with any open graph PR. Rebuild the PR with `git checkout -B <b> origin/main && git cherry-pick <commits>` and force-push.
- **The worker contract tail** is in the session scratchpad (`common.md`), which a new session will not have. Rebuild it from `worker_contract.md`, including the host-bridge environment notes and the gate command from the round-4 notes below.
- **Gates on the host:** `make fmt-check test test-scripts e2e-routes e2e version-check generate-check`, then `GOTOOLCHAIN=go1.26.4 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run ./...` and `go vet ./...`.
- **Worktree setup** (not in config): on the host, `go mod download && cd e2e && npm ci && npx playwright install chromium`.
- **Host plugin path:** `/Users/timothygunter/.odette/base/.marketplace-cache/market/plugins/overseer/...`.
- **Version check:** test-only changes still need a bump.

## Next to dispatch, and why

1. **Merge in order, as each goes review-clean:** history (#20), photo-ocr (#30), atomic-ingest (#66, after #106). Re-bump VERSION between merges.
2. **Then w3-unit-owner (#65)**, after the owner's proposed-units answer, with its lane widened as above.
3. **w3-search-list (#17)** takes storedb and migration `00004`. Only after #66 merges, because it writes the same generated package.
4. **w3-catalog-page (#22)** writes storedb, so not alongside #17 or #66. **w3-editor (#19)** needs #66 merged. **w7-settings (#35)** is dispatchable now (lane fixed in #96: it wires `HealthDetails` from `cmd/gobbler`).
