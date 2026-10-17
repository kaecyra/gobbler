# Overseer handoff: 2026-10-17, round 11

This file holds the volatile state the graph does not. The durable record is `docs/plan/graph.json`. Read `VERSION` on main for the current version; do not trust a number written here.

**Keep this file current.** Update it at every task start and stop, and commit it straight to `main` as bookkeeping. Every session ends on a handoff.

## Round 13 (2026-10-17), open: the UI track

**Why:** shown the running app, the owner found it nearly unstyled; the reviewed mockup was never adopted. ADR-00022 (#256, 867124c) adopts it, with `docs/ui/design.md` as the spec and `docs/ui/mockup.html` as the drawn reference. The owner accepted every recommendation ("just rock it, we'll tweak later").

**Nodes:** g-e2e-ports (#257) first, then u-shell (#246) and u-recipe-fields (#247, schema migration), then u-ingest-fields (#248), u-ribbon (#249), u-library (#250), u-recipe-view (#251), u-cook (#252), u-shopping (#253), u-ingest-look (#254), u-other-pages (#255).

**Demo server** on the owner's Mac: `/tmp/gobbler-demo/gobbler serve`, dev auth bypass on `127.0.0.1:8080`, data in `/tmp/gobbler-demo/data`, built from 1b5fdfc. Six ingested drafts await review there. Rebuild it once the UI nodes land.

## Round 12 (2026-10-17), closed

**Merged:** #231 r-write-lock-comments (c6f5b11); #232 i-step-graph (60fa063); #233 i-review-edit, also closed #228 (23f4929); #234 i-review-steps (8d54a9b); #238 r-thumbnail-everywhere (5c7736d); #241 i-video-extract (fb35b78); #244 g-thumbnail-fixture, #242 (46fd2b6). Graph PRs #229, #235, #236, #237, #239, #240, #243, #245. VERSION on main 0.61.0.

**Nothing is in flight:** no open PRs, no worktrees. Every dispatchable node is merged. Only two remain, both waiting on the owner:
- **i-site-extractors (#193):** parked until a URL still ingests badly after #186 and #191; that URL becomes the first extractor.
- **l-meal-planning (#38):** deferred; needs its own ADR first.

**Leftovers, no issue yet:** e2e specs that spawn their own server (e.g. editor, history) do not set `GOBBLER_VIDEO_THUMBNAIL_HOST`; none saves a video today. `.env.example` and `compose.yaml` do not list the new optional variable.

**Process note:** the #230 worker edited `internal/ingest/fallback.go` outside its lane before asking; the lane was widened after the fact (#236) since only merged nodes owned it.

**Unverified call:** the passive vocabulary in `internal/ingest/steps.go` took three review rounds; precision was favoured, so phrasings like "simmer gently" after "and" fall to the LLM as default steps.

**Process note:** worktrees are kept until merge, so review fixes go to a worker in the same worktree.

## Round 11 (2026-10-17)

**Merged:** #200 ADR-00021 (YouTube video on a recipe) and nodes; #201 i-text-meta (#185); #202 i-fallback-no-call-test (#188); #207 t-shopping-check-leak (#197); #208 i-plugin-extractors (#186); #210 g-e2e-flakes (#204, tenancy port collision); #213 r-sqlite-busy (#209, write transactions begin IMMEDIATE); #214 i-equipment-extract (#187); #218 r-write-queue-cleanup (#212); #220 r-issue-code-spelling (#205); #222 i-parser-version (#189, schema 00012). Graph PRs #203, #206, #211, #215, #216, #217, #219.

**In flight at handoff (check `gh pr list`; a fresh session cannot message the old workers, so re-dispatch a fresh worker into the existing worktree for any fix):**
- **Merged late in the round:** #224 (graph, owner-approved: retry spec added to i-llm-gap-fill's lane) and #225 r-recipe-video (#198, schema 00013), recorded at 6c71fce. VERSION on main is 0.55.0.
- #227 i-llm-gap-fill (#191) merged at 928b100. VERSION on main is 0.56.0. **Nothing is in flight: no open PRs, no worktrees.**

**Next after those:** i-step-graph (#190) after #191; read #191's PR and fill `Gaps.Steps` and the merge point in `fillGaps`. i-review-edit (#192) after #191 and #198. i-video-extract (#199) last. Issues without nodes: #221 (three stale "takes the write lock" comments), #226 (thumbnail download on restore and share accept), #228 (review banner and `UsedLLM` doc overstate the LLM's role; fold into i-review-edit with a `draft.go` grant).

**Owner decisions this round:**
- #193 parked: waits for a URL that still ingests badly after #186 and #191.
- YouTube video (ADR-00021): click-to-play from youtube-nocookie; thumbnail downloaded into its own SQLite BLOB table keyed by video ID (not MinIO, not files); ingest reads schema.org `video`.
- Mechanical graph PRs (lane widenings, nodes from issues, edges) merge on validator + CI with no review agent; batch graph changes; grep for tests and comments asserting old behaviour before writing a lane (`.memory/feedback-mechanical-graph-prs.md`). The owner wants speed.

**Unresolved:**
- #209's plain `database is locked (5)` in CI was not explained; #213 fixed the BUSY_SNAPSHOT path. Reopen if confirm 500s recur.
- `.claude/rules/git-workflow.md` names bumps only for feat/fix/refactor, but `check-version-bump.sh` also requires one for a test-only change. Workers follow the gate.
- i-plugin-extractors' plugin selectors were written from memory, unverified against live WPRM/Tasty/Create pages (stated in `plugins.go`).
- Other e2e specs and `fixtures/serve.mjs` still default to fixed 38xxx ports in the ephemeral range (#204 comment).

**Process notes:**
- Worker VERSION bumps collide on every merge: whoever merges second rebases and re-bumps. Merge the cheapest first.
- `version-check` only sees committed changes; workers who run it before committing report a false pass. Re-run gates after the final commit.
- Concurrent git commands on the primary checkout hit `index.lock`; a failed graph-branch commit then leaks the graph edit onto main. Check `git status docs/plan` after any failed graph write.

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

**Round 8 merged:**

| PR | What | SHA |
|---|---|---|
| #140 | tenancy split | ea28f9b |
| #142 | w7-nav | 78b7af9 |
| #143 | w5-codec | c9fae2e |
| #144 | t-household-core (schema 00007) | 55c2c71 |
| #145, #146 | t-operator-nav | 3ba7487 |
| #147 | lane widening | 743c9f5 |
| #148 | graph fill | f9ea8e7 |
| #149 | t-scope-recipes (schema 00008) | f979813 |
| #150 | g-image-check | 26de55f |
| #151 | g-templ-gate | 396d7c1 |
| #154 | w2-proposed-dedup | 57686f7 |
| #152 | e2e-seed-helper | 29944a4 |

VERSION on main is 0.43.4. The next schema number is 00009.

**Owner direction (2026-10-13):** every open issue has a graph node, and dispatch runs as wide as the graph allows (`.memory/feedback-graph-holds-all-work.md`).

**Owner decisions (2026-10-13):**
- **l-invitations runs in parallel** with the shopping scope.
- **#36 Todoist** is closed as not planned.
- **#38 meal planning** stays deferred.
- **#85 equipment size:** option 1, a `sized` flag on catalog equipment, set during review (recorded on #85).
  - Recorded as ADR-00019 in #159, which also re-derives the w3-equipment-size lane and unblocks it.

**Round 9 (2026-10-13, resumed session), closed. Nothing is in flight; no worktrees on disk.**

| PR | What | SHA |
|---|---|---|
| #153 | w3-display-times | 377f838 |
| #155 | graph: l-invitations parallel, Todoist dropped, one sqlc writer rule | 441094d |
| #156 | graph: t-scope-shopping lane gains `internal/store/migration_test.go` | d89c27b |
| #157 | w3-recipeview-links | d6fe731 |
| #159 | ADR-00019 sized equipment flag; w3-equipment-size unblocked | 016033c |
| #158 | t-scope-shopping (schema 00009) | 814f195 |
| #160 | l-invitations | bf5b204 |
| #163 | graph: nodes r-proposed-wrapper (#161), g-image-check-json (#162) | 98d4157 |
| #164 | r-proposed-wrapper | a5260c1 |
| #165 | g-image-check-json | f953e76 |
| #166 | graph: overlay lane widened (tests, write paths, promotion seed, design.md) | 089ab17 |
| #167 | graph: `internal/shopping/queries/**` shared, append-only | 71940c0 |
| #168 | t-catalog-overlay (schema 00010) | e3cdd65 |

VERSION on main is 0.47.0. The next free schema number is 00011; the next ADR is 00020.

**Calls the dispatcher made under ADR text, for the owner to check:**
- #160: a departing or removed member's pending join invitations are revoked.
- ADR-00019: the operator sets `sized`, because equipment is central (ADR-00018 Catalog).
- #168 migration 00010: an unreviewed ingredient referenced by exactly one household moves to that household's overlay; anything else stays central.
- #168 also adds store-level checks that refuse an ingredient id outside central or the writer's overlay (shopping manual items, pantry, recipe lines).

**Round 10 (2026-10-13), closed at the context ceiling. Nothing is in flight: no worktrees, no open PRs. VERSION on main is 0.50.0. The next schema number is 00012; the next ADR is 00021.**

Merged this round: #174 r-tenancy-test-gaps, #173 r-web-household-operator-shared, #179 r-operator-promotions-link, #175 t-recipe-sharing, #182 t-sharing-entry-points, #183 r-drop-todoist-config (owner: Todoist dropped), #184 t-cross-household-gate, #196 w3-equipment-size (schema 00011). Graph/ADR PRs #172, #177, #178, #181, #194, #195.

**Owner decisions this round:**
- Todoist is gone (config, compose, .env.example, operations.md, design.md). Two design.md lines still cite ADR-00018's deferral; dropping them needs an ADR.
- ADR-00020 (#194): ingest builds the step graph by rules then LLM (default vs cue-backed steps); the LLM fallback fills only gaps (amends ADR-00006 step 5); the review screen reuses the recipe editor on the draft.
- ADR-00019 corrected in place (pre-deploy window): the larger-pan hint shows only when scaled up.

**Next to dispatch (ingest track, from #194; all wave 9):**
1. **i-text-meta (#185)** and **i-fallback-no-call-test (#188)**: dispatchable now, disjoint lanes. Dispatch both in one message.
2. Then the chain: i-plugin-extractors (#186) → i-equipment-extract (#187) → i-parser-version (#189, draws schema 00012) → i-llm-gap-fill (#191) → i-step-graph (#190); i-review-edit (#192) after i-parser-version. Read each node's note (ADR-00020) and #191's last comment before dispatch.
- **Blocked:** i-site-extractors (#193) waits on the owner naming the sites that ingest badly. l-meal-planning (#38) is deferred and needs an ADR.

**Filed, not yet a node:** #197: a shopping check from another household returns 303 instead of 404 when the key is already checked (leaks checked state). Add a node (lane `internal/shopping/**check*`, `internal/web/shopping/**`, `e2e/tenancy/tenancy.tenancy.spec.ts`) through a graph PR, then dispatch; the tenancy spec's comment steering around it goes with the fix.

**Small leftovers, no issue yet:** journal and sharing e2e specs both default to port 38451; `.claude/rules/git-workflow.md` "every route has a test" now also means a cross-household test (route-coverage gate, #184); `/operator/equipment` has no nav link.

**For the owner to check (calls made under ADR text):**
- #175: shared-recipe provenance is a note on the copy (no column); a member can edit or delete it.
- #175: a sender-overlay ingredient's name reaches the recipient as a new recipient-owned unreviewed entry (ADR-00018 Sharing), against #138's literal acceptance line.
- #184: route rules where ADR-00018 is silent: operator/settings 403 for both identities; /healthz and /static 200; /not-a-member 200/303; foreign invitation 404.
- graph-node has no verb to attach an existing ADR to a node; SHOULDs asking for it were declined (#172, #177).

**Process notes from this round:**
- Name dispatcher graph branches without an issue number (`docs/plan-<slug>`): the board maps `<prefix>/<N>-…` branches to node N.
- Two PRs that each bump VERSION conflict on merge; the worker rebases and re-bumps (main+1), then pushes once itself.
- A new route now needs probes in `e2e/tenancy/tenancy.tenancy.spec.ts`; give the node that file in its lane (only merged nodes own it).

**Follow-ups from #160 and #168 reviews (now nodes #169, #170, #171):**
- `fail`, the time layout and the pending view are duplicated between `internal/web/household` and `internal/web/operator`; a shared helper belongs in `internal/web/layout`.
- No nav link from `/operator` to `/operator/promotions` (`layout.OperatorPaths`, `internal/web/operator/invitation.templ`).
- `internal/household/delete_test.go` seeds no overlay entries, aliases or overrides, so its schema-wide "no rows left" check passes trivially for those tables.
- The migration 00010 tests reference ingredients only through the pantry and recipe lines; the revision-index and manual-item branches of the `refs` CTE are not exercised.

**Worker brief rule (new, 2026-10-13):** workers must never spawn their own subagents. The t-household-core worker left a helper ("Proving dot-segment guard fails") running and burning tokens after its PR had merged. Say so in every worker prompt.

**Ordering rule (owner, 2026-10-13):** serialize only for real file overlap, or where one node's code needs another's. Never for main's state between merges. See `.memory/project-never-deployed.md`.

**Next to dispatch, after #155 merges, all in one round (afterburners):**
- **t-scope-shopping** (#136): draw schema 00009 with `node.ts migration`.
- **l-invitations** (#39), reduced as above.
- **w3-recipeview-links** (#98), after #153 merges.


**Notes:**
- `dev@localhost` is an operator on the shared e2e server.
- Run `make generate` from the repository root.
- Workers on parallel runs use port blocks in steps of 100 from 41100.
- The worker brief tail is `worker_contract.md` plus the gate list below.

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
- **sqlc packages share one schema.** Any migration also regenerates the other package's `models.go`. Nodes that add migrations therefore can't share a round. Nodes that only add queries to one package can: the later merge rebases and runs `make generate` (graph notes, from #155).

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
