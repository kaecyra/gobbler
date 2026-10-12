# Overseer handoff: 2026-10-12, round 6

This file holds the volatile state the graph does not. The durable record is `docs/plan/graph.json`. `VERSION` on main is **0.34.1**.

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
| w3-editor | #19 | #119 | c8d8233 |
| w3-unit-owner (schema 00003) | #65 | #118 | efb658d |
| Graph: w4-shopping-store lane gains `merge_test.go` and `doc.go`, and node w3-edit-link (#120) is added | — | #122 | aa1007e |

#107 (the round-5 handoff PR) was closed as superseded.

## In flight

| Worktree | Node | State |
|---|---|---|
| `wt-22` | w3-catalog-page (#22), **PR #121** | Approved, every thread resolved, CI green. GitHub refused the merge because the branch predates #118 and conflicts on VERSION. The worker is rebasing; VERSION stays 0.35.0. Merge once it is green. |
| `wt-17` | w3-search-list (#17, schema `00004`), **PR #113** | A fresh worker is rebasing it onto main and fixing the 3 NITs from 5491507666: rewrap doc comments, `sqlc.arg(tag_count)`, and the helper `five()`. Then resolve the threads, check the delta and merge. Next in migration order. |
| `wt-24` | w4-shopping-store (#24, schema `00005`), **PR #125** | Opened; first review running. It merges **after #113**, then rebases, runs `make generate` and re-bumps. `RegisterMergeHooks` stays unwired until #25. |
| `wt-120` | w3-edit-link (#120) | Worker dispatched. |
| `wt-124` | #124, deploy: drop the cloudflared sidecar | Worker dispatched. Owner decision: cloudflared runs on the VM guest from the owner's IaC. Publish gobbler on `127.0.0.1:8080:8080`; the guest's tunnel URL is `http://127.0.0.1:8080`. No graph node. |

Every open branch bumps VERSION over main, so each merge forces the rest to re-bump. Re-bump against main at merge time.

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
- #120: Edit link from the recipe page (node w3-edit-link).
- #123: share the quantity/unit DB codec between store and shopping. It runs after #24, and not in a round with any node that holds `internal/store/**`.
- #124: drop the cloudflared sidecar.
- #112: gate to fail `make check` on stale templ output. `make generate` runs sqlc only. `journal_templ.go` (and others, per the #113 worker) on main are already stale, and every `go tool templ generate` rewrites them. Workers revert that drift out of lane.

## Owner decisions this round

- Handoffs are maintained throughout and committed to main (`.memory/feedback-handoff-maintained.md`).
- #65 option A, recorded in #65's body: drop proposed units and flag unknown ones on review.
- #24 option A: narrow `TestPackageIsPure` to the pure files instead of moving the store to a subpackage (graph PR #122).
- No cloudflared sidecar. The tunnel runs on the guest and is configured by the owner's IaC (#124).
- Ship fast: w4-shopping-store was started before #113 merged, accepting a `make generate` on rebase.

## Migration and ADR numbers

- `schema`: `00003` is merged (w3-unit-owner). `00004` belongs to w3-search-list (#113) and `00005` to w4-shopping-store (#125). The next free number is `00006`. Merge order: #113, then #125.
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
- **Review agents** share `.cc-scratch/`, so tell each one to suffix its files with the PR number. **Tell them never to check out a branch in the primary checkout.** One did this round; nothing was lost.
- **sqlc packages share one schema.** Any migration also regenerates the other package's `models.go`. Nodes that add migrations therefore can't share a round, whatever their single-writer notes say.

## Next to dispatch, and why

1. **Merge #121** once the rebase is green: it is approved and has no migration.
2. **Merge #113**, then rebase #125 onto it (`make generate`, re-bump) and merge it once it is review-clean.
3. **w4-shopping-ui (#25)** after #125. It must wire the shopping store's `RegisterMergeHooks` and usage counters in `mountCatalog`. Then **w7-todoist (#36)**.
4. **#123** (codec) after #125.
