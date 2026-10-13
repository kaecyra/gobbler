# Overseer handoff: 2026-10-13, round 8 (late)

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

Round 8 merged: #140 tenancy split (ea28f9b), #142 w7-nav (78b7af9), #143 w5-codec (c9fae2e), #144 t-household-core (55c2c71, schema 00007), #145 graph node t-operator-nav (ab21550), #146 t-operator-nav (3ba7487), #147 t-scope-recipes lane widening (743c9f5), #148 graph nodes for every open issue (f9ea8e7). VERSION on main is 0.42.0.

**Owner direction (2026-10-13):** every open issue that needs doing has a graph node, and dispatch runs as wide as the graph allows (`.memory/feedback-graph-holds-all-work.md`).

| Node | Issue | PR | Worktree | State |
|---|---|---|---|---|
| t-scope-recipes (schema 00008) | #135 | #149 | wt-135 | review fixes done, threads resolved; merge when CI is green. VERSION 0.43.0 |
| g-image-check | #78 | #150 | wt-78 | review fixes done, not pushed. Re-bump after #149 merges (0.43.1). Merge before #151 |
| g-templ-gate | #112 | #151 | wt-112 | fixing review findings. After #150 merges: add `check-image_test.sh` to `make test-scripts` and the git-workflow.md row, then re-bump |

**Merge order:** #149, then #150, then #151.

**Next to dispatch once #149 merges** (lanes are disjoint): w3-display-times (#111), e2e-seed-helper (#99), w2-proposed-dedup (#88). After e2e-seed-helper: t-scope-shopping (#136, schema 00009). Then w3-recipeview-links (#98).

**Before dispatching l-invitations:** widen its lane onto `internal/web/layout/layout_operator.go`. `layout.OperatorPaths` there is the one list of operator pages, read by both the middleware lock and the nav (#146).

**Blocked:** w3-equipment-size (#85) needs an owner decision on how equipment gets a size (ADR-00005).

**Notes:**
- `dev@localhost` is an operator on the shared e2e server; `other@localhost` is a member who is not.
- Dot-segment paths get a 404 before admission (#144).
- The interim e2e seed helper lives at `e2e/recipeview/seed/devhousehold`; #99 moves it.

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

## ADR-00018: households as tenants (owner decision, accepted)

- Settled with the owner on 2026-10-12. Multiple users means households as tenants. A household has several members, and an email belongs to at most one household. Every member has the same powers. Access admits any email that authenticates, and the app's membership check is the only lock. Nobody joins without an invitation. Only recipes are shared, and only as a copy sent to another household. The catalog is a central layer plus a per-household overlay. Only the operator promotes, and promotion merges exact duplicates automatically. Meal plans are household data.
- ADR-00003 and ADR-00011 are amended. `AGENTS.md` and `README.md` no longer say single-user.
- The graph attaches 00018 to `l-invitations`, and the ADR sequence's `last_used` is `00018`. The tenancy work is split into the `tenancy` track: t-household-core, t-scope-recipes, t-scope-shopping, t-catalog-overlay, l-invitations, t-recipe-sharing and t-cross-household-gate (issues #134 to #139, plus #39). Obligations 4 and 6 fall to t-household-core, and obligation 5 to t-cross-household-gate.
- Owner rulings while splitting: `/settings` is operator-only, and Todoist push is deferred. Both are recorded in ADR-00018, which also amends ADR-00009. The owner deferred Todoist partly to hold scope: nobody has asked for it, and whatever integration returns may not be Todoist.
- `node.ts` has no verb to attach an existing ADR to a node or to clear `needs_migration`. The owner approved hand-editing those two fields on #140 (tenancy `adrs` gain `00018`; l-invitations `needs_migration` is null).
- No deployment holds data, so tenancy needs no backfill.

## Things a fresh dispatcher must know

- **The worker contract tail** is session scratch: `common.md` in the session scratchpad, gone with the session. Rebuild it from `worker_contract.md` with these points:
  - The toolchain is on the host, through `mcp__exec-daemon__execute_command`, and so are git and gh.
  - The gate list is below.
  - Use per-worker e2e port blocks: `GOBBLER_E2E_{,ACCESS_,RECIPEVIEW_,RETRY_,JOURNAL_,COOK_,HISTORY_,PHOTO_,SETTINGS_,RECIPES_}PORT`. The spec defaults are recipes 38491 and settings 38501.
  - **Generated code:** run `make generate` from the worktree root. Once #151 lands it runs templ, goimports and sqlc. Never run `go tool templ generate` inside a package directory, because it writes the wrong `FileName`.
- **Gates on the host:** `make fmt-check test test-scripts e2e-routes e2e version-check generate-check`, then `GOTOOLCHAIN=go1.26.4 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run ./...` and `go vet ./...`.
- **Worktree setup** (not in config): on the host, run `go mod download && cd e2e && npm ci && npx playwright install chromium`.
- **Host plugin path:** `/Users/timothygunter/.odette/base/.marketplace-cache/market/plugins/overseer/...`. `bun` and `gh` exist only on the host.
- **Graph PRs:** a bookkeeping commit on main conflicts with any open graph PR. Rebuild with `git checkout -B <b> origin/main && git cherry-pick <c>` and force-push. Concurrent git on the primary checkout can hit `index.lock`. After a failed checkout, check that `graph.json` edits didn't carry over to `main`.
- **Review agents** share `.cc-scratch/`, so tell each one to suffix its files with the PR number. **Tell them never to check out a branch in the primary checkout.** One did this round; nothing was lost.
- **sqlc packages share one schema.** Any migration also regenerates the other package's `models.go`. Nodes that add migrations therefore can't share a round, whatever their single-writer notes say.

## Next to dispatch, and why

All three are dispatchable now. Their lanes don't overlap, and only t-household-core has a migration. Dispatch them in one message:

1. **t-household-core (#134), keystone:** household, membership, invitation and share tables (draw `schema` 00007 with `node.ts migration`). Membership middleware, the not-a-member page, operator emails in config, `/settings` made operator-only, and switchable dev/e2e identity.
2. **w7-nav (#130):** fill the empty layout nav (`internal/web/layout/layout*`, `e2e/layout/**`).
3. **w5-codec (#123):** move the quantity/unit DB codec into `internal/quantity`. Decoding must stay byte-compatible with what is already stored.

After that the tenancy chain (ADR-00018) runs one node at a time, because every link adds a migration and all the sqlc packages share one schema: t-scope-recipes (#135, also waits on w5-codec), then t-scope-shopping (#136), then t-catalog-overlay (#137). After the overlay, l-invitations (#39) and t-recipe-sharing (#138) can run in parallel; neither adds a migration. t-cross-household-gate (#139) is last.

**Deferred:** w7-todoist (#36) is in wave 9, deferred by ADR-00018, and returns only with its own ADR. l-meal-planning (#38) is in wave 9 too and still needs its own ADR.

## Lessons from this round (for the next dispatcher)

- **Migration tests that build "the schema minus the newest migration" break whenever a newer migration lands.** This happened on #113, #125 and #133. The fix each time was to skip from that migration upward. Expect it again.
- **Reviewers checked out branches in the primary checkout** and ran `validate.ts` from an in-repo copy, which silently validates main's graph. Every review prompt now says to use `git show` or `git diff`, and a standalone `/tmp` copy for `validate.ts`.
- **Host `sed` is BSD.** Edit files in the container and run only `gh` and `git` through the bridge.
