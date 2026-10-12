# Overseer handoff: 2026-10-12, round 7

This file holds the volatile state the graph does not. The durable record is `docs/plan/graph.json`. `VERSION` on main is **0.39.0**.

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
| w3-catalog-page | #22 | #121 | ed82088 |
| Graph: w3-search-list lane gains the 00003 migration test | — | #128 | 1b411b7 |
| w3-search-list (schema 00004) | #17 | #113 | e83eafa |
| w3-edit-link | #120 | #126 | 45981eb |
| Graph: w4-shopping-store lane gains `internal/store/search_test.go` | — | #129 | a62c316 |
| w4-shopping-store (schema 00005) | #24 | #125 | 8ce630e |
| #124 deploy: no sidecar; ADR-00017 amends ADR-00002 | #124 | #127 | f4e1aa9 |
| Graph: widen w4-shopping-ui; add w7-nav, w5-codec; correct schema and ADR sequences | — | #131 | 58db291 |
| Graph: w4-shopping-ui gains `internal/web/server_stream_test.go` | — | #132 | 0b00455 |
| w4-shopping-ui (schema 00006) | #25 | #133 | d10779e |

#107 (the round-5 handoff PR) was closed as superseded.

## In flight

Nothing. No worktrees on disk and no open PRs from this fleet. The session handed off at about 450k context, before the next dispatch.

Every open branch bumps VERSION over main, so the second PR to merge always re-bumps. A migration test that leaves out "the latest" migration breaks when a newer one lands. #128 and #129 widened lanes for that.

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
- #124: drop the cloudflared sidecar (PR #127).
- #112: gate to fail `make check` on stale templ output. `make generate` runs sqlc only. `journal_templ.go` (and others, per the #113 worker) on main are already stale, and every `go tool templ generate` rewrites them. Workers revert that drift out of lane.

## Owner decisions this round

- Handoffs are maintained throughout and committed to main (`.memory/feedback-handoff-maintained.md`).
- #65 option A, recorded in #65's body: drop proposed units and flag unknown ones on review.
- #24 option A: narrow `TestPackageIsPure` to the pure files instead of moving the store to a subpackage (graph PR #122).
- #25 option A: finish the optional-lines toggle, the usage counter and the shutdown fix in #25 itself, and make a node for anything else (w7-nav #130, w5-codec #123).
- No cloudflared sidecar. The tunnel runs on the guest and is configured by the owner's IaC (#124).
- Ship fast: w4-shopping-store was started before #113 merged, accepting a `make generate` on rebase.

## Migration and ADR numbers

- `schema`: `00003` to `00006` are all merged. The graph's `schema` sequence was corrected in #131 and is the source of truth: hand out numbers with `node.ts needs-migration` and `node.ts migration`, never by hand. The next free number is `00007`.
- ADR-00017 is merged (#127), and the graph's ADR sequence was corrected to `00017` in #131. Draw the next number with `node.ts adr`.

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

All three are dispatchable now. Their lanes don't overlap, and none has a migration. Dispatch them in one message:

1. **w7-todoist (#36):** one-way, idempotent Todoist push of a shopping list. Lane: `internal/todoist/**` and `internal/web/shopping/todoist*`. Config keys `GOBBLER_TODOIST_TOKEN` and `GOBBLER_TODOIST_PROJECT` already exist in `compose.yaml`. Check how it mounts. Its lane has no `routes.go` entry, but `routes.go` is append-only shared, so it can add one.
2. **w7-nav (#130):** fill the empty layout nav (`internal/web/layout/layout*`, `e2e/layout/**`). This is what makes `/recipes`, `/catalog`, `/shopping` and the other pages reachable from the home page.
3. **w5-codec (#123):** move the quantity/unit DB codec into `internal/quantity`, shared by store and shopping. Its lane includes the callers. Decoding must stay byte-compatible with what is already stored.

Every one bumps VERSION. Whichever merges second and third re-bumps after rebasing.

The `later` nodes, `l-meal-planning` (#38) and `l-invitations` (#39), each need an ADR first. That is the owner's call.

## Lessons from this round (for the next dispatcher)

- **Migration tests that build "the schema minus the newest migration" break whenever a newer migration lands.** This happened on #113, #125 and #133. The fix each time was to skip from that migration upward. Expect it again.
- **Reviewers checked out branches in the primary checkout** and ran `validate.ts` from an in-repo copy, which silently validates main's graph. Every review prompt now says to use `git show` or `git diff`, and a standalone `/tmp` copy for `validate.ts`.
- **Host `sed` is BSD.** Edit files in the container and run only `gh` and `git` through the bridge.
