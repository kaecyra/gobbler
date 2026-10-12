# Overseer handoff: 2026-10-12, round 6

This file holds the volatile state the graph does not. The durable record is `docs/plan/graph.json`. `VERSION` on main is **0.33.0**.

**Keep this file current.** Update it at every task start and stop, and commit it straight to `main` as bookkeeping. Every session ends on a handoff.

## Merged this round (squash SHA, PR)

| Node / change | Issue | PR | SHA |
|---|---|---|---|
| w3-history | #20 | #108 | 4b7571e |
| w5-photo-ocr | #30 | #109 | 709103f |
| w3-store-atomic-ingest (also closes #104) | #66 | #110 | bf4d0b9 |
| Graph: w7-settings lane widened onto `internal/web/web.go` and `cmd/gobbler/serve.go` | — | #114 | c7e5ba8 |
| w7-settings | #35 | #115 | 26fbe6c |
| Graph: w3-search-list lane widened onto `internal/db/tx*.go` and `internal/store/store.go` | — | #116 | a93962e |
| Graph: w3-unit-owner lane widened to `internal/ingest/*.go` and `internal/web/ingest/**`, block cleared | — | #117 | 006760c |

#107 (the round-5 handoff PR) was closed as superseded.

## In flight

| Worktree | Node | State |
|---|---|---|
| `wt-65` | w3-unit-owner (#65, schema `00003`), **PR #118** | Rebased on main, VERSION 0.33.1, ownership and gates verified by the dispatcher. Added `0939d4a`: `ingest.ErrUnknownUnit` returns the user to the review screen (422). No unit picker, because drafting cannot produce an unknown unit (the parser flags the word at line scoring, and the LLM path goes through the same parser). The reasoning is in the PR body. First review is running. |
| `wt-17` | w3-search-list (#17, schema `00004`), **PR #113** | Its 4 threads are resolved. Re-review 5491507666 found 0 MUST, 0 SHOULD and 3 NIT: rewrap two doc comments, use `sqlc.arg(tag_count)` in `search.sql`, and the test helper `five()` returns 4. **After #118 merges**, a worker rebases, re-bumps VERSION (feat, minor over main), fixes the 3 NITs, and then the threads are resolved. The original worker is gone, so this needs a fresh dispatch. |
| `wt-19` | w3-editor (#19) | Worker dispatched this session. VERSION is set to 0.34.0 and is re-bumped at merge. |

## Side task: mockup mascots (owner request), done

- **Mascot SVGs.** The 9 new SVGs are in `mascots/` at the repo root, untracked, along with the two `Gemini_Generated_Image_*.jpeg` sources. Known small flaws:
  - stair-steps on three bust edges
  - a pale patch on `mouse-dough`
  - grey flecks on the bandana knot
  - pie slits drawn in the wrong brown
  - a red patch in the mixing bowl
- **Mockup v20 is published** at https://claude.ai/artifact/Maaf6isAd66jbzvgmtkz1g. The source is `.memory/mockup/gobbler-mockup-v20.html`.
  - The header mascot is unchanged.
  - Suggestion: full rolling pin, flipped. Forgot tile: mixing bowl, flipped. Tablet timer peek: whisk, flipped. Phone peek: herbs. Basket full: pie. Search no-results: tasting.
  - No CSS changed.
  - Open points for the owner: the no-results mouse looks small in its 110×141 box; the suggestion mouse has lost its ground shadow; `mouse-wink` and `mouse-dough` are unused.

## Issues filed this round

- #111: extract the document time rows and total-source note into `internal/recipe/display`. History duplicates recipeview's strings until then.
- #112: gate to fail `make check` on stale templ output. `make generate` runs sqlc only. `journal_templ.go` (and others, per the #113 worker) on main are already stale, and every `go tool templ generate` rewrites them. Workers revert that drift out of lane.

## Owner decisions this round

- Handoffs are maintained throughout and committed to main (`.memory/feedback-handoff-maintained.md`).
- #65 option A, recorded in #65's body: drop proposed units and flag unknown ones on review.

## Migration and ADR numbers

- `schema`: `00003` belongs to w3-unit-owner (wt-65) and `00004` to w3-search-list (#113). The next free number is `00005`. Merge order must be 00003 then 00004.
- The ADR sequence is at `00016`.

## Things a fresh dispatcher must know

- **The worker contract tail** is session scratch: `common.md` in the session scratchpad, gone with the session. Rebuild it from `worker_contract.md` with these points:
  - The toolchain is on the host, through `mcp__exec-daemon__execute_command`, and so are git and gh.
  - The gate list is below.
  - Use per-worker e2e port blocks: `GOBBLER_E2E_{,ACCESS_,RECIPEVIEW_,RETRY_,JOURNAL_,COOK_,HISTORY_,PHOTO_,SETTINGS_,RECIPES_}PORT`. The spec defaults are recipes 38491 and settings 38501.
  - **`make generate` is sqlc only. Run `go tool templ generate` by hand**, then commit the `_templ.go` for your own `.templ` and revert out-of-lane drift.
- **Gates on the host:** `make fmt-check test test-scripts e2e-routes e2e version-check generate-check`, then `GOTOOLCHAIN=go1.26.4 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run ./...` and `go vet ./...`.
- **Worktree setup** (not in config): on the host, run `go mod download && cd e2e && npm ci && npx playwright install chromium`.
- **Host plugin path:** `/Users/timothygunter/.odette/base/.marketplace-cache/market/plugins/overseer/...`. `bun` and `gh` exist only on the host.
- **Graph PRs:** a bookkeeping commit on main conflicts with any open graph PR. Rebuild with `git checkout -B <b> origin/main && git cherry-pick <c>` and force-push. Concurrent git on the primary checkout can hit `index.lock`. After a failed checkout, check that `graph.json` edits didn't carry over to `main`.
- **Review agents** share `.cc-scratch/`, so tell each one to suffix its files with the PR number.
- **sqlc packages share one schema.** Any migration also regenerates the other package's `models.go`. Nodes that add migrations therefore can't share a round, whatever their single-writer notes say.

## Next to dispatch, and why

1. **Verify and open #65's PR (wt-65), then merge it**, so #113 can merge.
2. **Finish #113:** resolve threads, re-review, re-bump, merge after #65.
3. **w3-editor (#19):** dependencies are merged (w3-csrf, w3-store-atomic-ingest). It uses `UpdateIfVersion` and the 409. Check its lane against the issue first.
4. **w3-catalog-page (#22):** it regenerates storedb, so not in the same round as #113 or #65.
5. **w4-shopping-store (#24):** a new sqlc package plus a migration (`00005`).
