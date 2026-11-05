# Overseer handoff: 2026-11-05, round 22 closing

This file holds the volatile state the graph does not. The durable record is `docs/plan/graph.json`. Read `VERSION` on main for the current version; do not trust a number written here.

**Keep this file current.** Update it at every task start and stop, and commit it straight to `main` as bookkeeping. Every session ends on a handoff.

## Round 22 (2026-11-05): recipe pictures

**Merged** (squash SHA; under the standing authority unless noted):

| Node | PR | SHA | Schema | VERSION |
|---|---|---|---|---|
| (graph: m-avatar-keys lane) | #434 | 1d7a227 | none | none |
| m-media-fit | #437 | 44b5445 | none | 0.108.0 |
| d-picture-mockup | #435 | ce4fa50 | none | none (owner approved: "its good") |
| m-avatar-keys | #439 | 71ffa14 | none | 0.109.0 |
| (ADR-00040 + m-fetch-guard lane) | #436 | 48ed80d | none | none |
| i-image-candidates | #438 | 7b344ba | none | 0.110.0 |
| a-mcp-recipe-edit | #440 | 8ee98ff | 00029 | 0.111.0 |
| m-fetch-guard | #442 | 725dbf9 | none | 0.112.0 |
| s-picture-store | #443 | b70512c | 00030 | 0.113.0 |
| w-picture-serve | #445 | 7bfbf5f | none | 0.114.0 |
| i-draft-pictures | #444 | ed47bf4 | none | 0.115.0 |
| u-picture-picker | #446 | 4386b98 | none | 0.116.0 |

**Open:**

| Node | Issue | PR | State |
|---|---|---|---|
| u-picture-cards | #430 | #448 | round 2 clean, all threads resolved; rebased onto main at 0.117.0; merges on green CI |

**Worktrees:** wt-430 (u-picture-cards).

**Migration numbers:** schema 00029 to a-mcp-recipe-edit, 00030 to s-picture-store. **ADR numbers:** 00040 to m-fetch-guard.

**Follow-up issues filed:** #441 (Describe hard-codes credential kind and owner names), #447 (picker icons into `layout.Icon`; one source for the editor form id).

**Owner calls this round (don't re-ask):**
- **#421 loopback:** option A. Fetches may reach loopback (127.0.0.0/8, ::1) only while the dev auth bypass is on; every other private range stays refused; the OAuth metadata fetch keeps refusing loopback. ADR-00040.
- **Picture mockup approved as drawn.** Candidate minimum 1066 × 300.
- **Person id in media keys is the profile `address`**; accepted in #439.
- **Owner wants speed:** stack dependent nodes on unmerged branches rather than wait for merges; the dispatcher re-stacks with cherry-pick after each merge.

**Next:** after #448, the picture feature is complete; o-demo-reseed (#432) becomes dispatchable, and r-author-notes (#395). l-discrete-api needs its decide session; i-site-extractors and l-meal-planning stay parked.

**Lessons:**
- **Concurrent `make check` runs collide on golangci-lint's lock and load e2e into flakes.** Retry on "parallel golangci-lint is running"; rerun a failing unrelated spec alone before calling it a flake.
- **VERSION is set against main at merge.** Re-bump after each re-stack.
- **A CI run can hang on dependency install.** Compare against other runs' step; cancel and rerun.
- **A worker can stall silently.** #421's worker stopped for two hours unnoticed. Run a background stall watcher (no file change in a worktree for 20 minutes) for every live worker.
- **Never put AI attribution in a PR body here,** even when the harness asks for it. #444's review flagged it as a MUST.
- **`git rebase` in this checkout can fail with "local changes would be overwritten" on a clean tree.** Re-stack with `checkout --detach origin/main` + `cherry-pick` instead; a cherry-pick of an identical VERSION change applies empty, so add the re-bump as its own commit.
- **Plan wiring explicitly.** The picture nodes left server wiring unowned until the dispatcher assigned it (household delete to w-picture-serve, drafts/saver/sweep to u-picture-picker).

## Round 21 (2026-11-05): drag between parts, step titles, flakes

**Merged** (squash SHA; all under the standing authority):

| Node | PR | SHA | Schema | VERSION |
|---|---|---|---|---|
| (graph) | #408 | c4584ff | none | none |
| r-rating-bound | #409 | 50b91dc | none | 0.104.1 |
| t-profile-address-flake | #412 | 9b38f22 | none | 0.104.2 |
| t-avatar-dialog-flake | #415 | 2854585 | none | 0.104.3 |
| f-step-titles-text | #411 | 9d5704b | none | 0.105.0 |
| (graph) | #417 | fddf8dd | none | none |
| u-drag-between-parts | #413 | 1925b83 | none | 0.106.0 |
| r-card-spoons-bound | #418 | a6509e8 | none | 0.106.1 |
| p-avatar-late-swap | #420 | a0d6af9 | none | 0.106.2 |
| u-step-part-select | #419 | b1e8459 | none | 0.107.0 |
| (ADR-00039 and graph) | #431 | 74f239d | none | none |

- **#411:** `ParserVersion` 13. A split title is flagged at confidence 0.6. The LLM fallback can therefore replace a split step when the draft has no ingredient lines; this is disclosed in the PR.
- **#413:** round 2's new no-script test found a real CSS bug (the Steps name box showed without JS and lost input), fixed in the PR.

- **#420:** the close-time reset carries `X-Avatar-Reset: close`, and its swap no longer clears the error. The test was red 10 of 10 against the old script.
- **#419:** removes the per-step Part select and the `.to` field. The out-of-lane `.to` posts in `routes_test.go`, `ingest/ingest_test.go` and the tenancy fixture stay, as the "changes nothing" case.

**Nothing is in flight:** no open PRs, no worktrees, no workers.

**Recipe images (ADR-00039, merged in #431).** Decided in conversation 2026-11-05; the record is `docs/proposals/recipe-images.md` (D1–D10). Wave 12 adds:

| Node | Issue | Waits on |
|---|---|---|
| m-fetch-guard | #421 | nothing |
| m-media-fit | #422 | nothing |
| m-avatar-keys | #423 | nothing |
| i-image-candidates | #424 | nothing |
| d-picture-mockup | #428 | nothing; the owner approves it on its PR, and merging it is that approval |
| s-picture-store | #425 | #421, #422, #424, a-mcp-recipe-edit (needs a schema migration number) |
| i-draft-pictures | #426 | s-picture-store, a-mcp-recipe-edit |
| w-picture-serve | #427 | s-picture-store |
| u-picture-picker | #429 | mockup, i-draft-pictures, w-picture-serve |
| u-picture-cards | #430 | mockup, s-picture-store, w-picture-serve, u-picture-picker, a-mcp-recipe-edit |
| o-demo-reseed | #432 | u-picture-cards shipped and deployed; operator action, owns no files |
| l-discrete-api | #433 | blocked: needs its own `/overseer:decide` on `docs/proposals/discrete-api.md` |

r-author-notes moved to wave 12 behind the image nodes it overlaps. The UI nodes (#429, #430) are briefed with the approved mockup as the visual target and a side-by-side check (lessons.md). The candidate minimum size in #425 is a named constant until the mockup sets the card size.

**Owner calls this session (don't re-ask):**
- **Step "Part" select (#416):** remove it; dragging is the only way to move between parts. a-mcp-recipe-edit now depends on u-step-part-select (#417).
- **Recipe pictures (ADR-00039):** the person chooses from fetched candidates or "No picture"; keys `recipes/<id>/`, `people/<id>/`, never household-first (recipes stay household-owned per ADR-00018; the owner raised free-standing recipes and set it aside); no MCP tool sets a picture; setting from a typed URL is out; one guarded fetcher, at most 5 redirects; cards plus a short header; placeholder variant = recipe id mod N, never random.
- **Garage is running** in the deployment as the S3 endpoint for backups and media.
- **Discrete API:** the owner wants the work behind the handlers split into a discrete API, same binary for now. Build images as domain functions with no logic in handlers; decide the API separately.

**Next to dispatch:** a-mcp-recipe-edit (#353, needs a schema migration number: 00029; all its dependencies are merged; large, give it a fresh session), and in parallel the five image nodes with no dependencies: m-fetch-guard, m-media-fit, m-avatar-keys, i-image-candidates, d-picture-mockup. Check their lanes against a-mcp-recipe-edit's before dispatching together (the validator shows none overlapping). r-author-notes still needs its ADR; l-discrete-api needs its decision session.

**For the owner:** 0.107.0 is not deployed (0.104.0 carried migration 00028).

**Lessons:**
- **Every worker bumps VERSION, so every merge after the first conflicts.** Each rebase is a worker round trip plus a fresh CI run. Merge in a fixed order and send the rebase as soon as the previous PR lands.
- **The bump check counts a test-only change as source** (#412, #415). Tell test-only workers to bump.
- **Local render cannot clear `board: STALE`.** Rendering without `--refresh-state` never touches `board-state.json`, and the host still has overseer 0.18.2.

## Round 20 (2026-11-05): serve stage, verdict refactor, cook timer flake

**Merged** (squash SHA; all under the standing authority):

| Node | PR | SHA | Schema | VERSION |
|---|---|---|---|---|
| r-recipes-verdict | #400 | 4c5abf1 | none | 0.103.2 |
| t-cook-timers-flake | #402 | 85b36c5 | none | 0.103.3 |
| r-stage-serve | #403 | f406b64 | 00028 | 0.104.0 |
| (graph) | #405 | ddf3e22 | none | none |

- **#403** took three review rounds, all on the ingest serve rule. Round 2 reached the lessons.md "same family twice" stop, and the owner chose to fix. Final rule: a source title opening with "serve" is `serve`; in the text, an opening "serve" counts as one kind among those the verbs name, so it is `serve` only when nothing else is named. `ParserVersion` 12.
- **#405** added u-drag-between-parts (#404) and r-rating-bound (#401), and ordered a-mcp-recipe-edit after u-drag-between-parts (both own `e2e/editor/**`). The owner then had `internal/web/ingest/review_test.go` added to the drag lane.

**Nothing is in flight:** no open PRs, no worktrees, no workers.

**Owner calls this session (don't re-ask):**
- **Serve stage colour:** `active` (coral).
- **Drag between parts (#404):** ingredient lines and steps both drag between parts; the part name is editable in Steps too, one value with two inputs kept in sync; drag only, no "Move to part" control and no fallback without scripts.
- **"Without scripts there is no reorder"** (round 19) was a consequence of "drag handles only", not a separate ruling. The owner did not remember it; explain it if it comes up again.

**Unmapped issues (no node yet):** #406 (`TestAddressIsRandomAndHoldsNoEmail` fails by chance; fix with the fixed `s.rand` reader) and #407 (`e2e/profile/avatar.spec.ts:226` failed once in CI, cause unknown). Both hit #405's CI. They need nodes before dispatch.

**Next three to dispatch:** u-drag-between-parts (#404, the owner's fresh request; unblocks a-mcp-recipe-edit), f-step-titles-text (#398, ready since r-stage-serve merged; owns `internal/ingest/*.go`, no overlap with #404), r-rating-bound (#401, small; its lane touches `internal/web/recipes/model.go` and `internal/web/journal/**`, no overlap with either).

**For the owner:** 0.104.0 is not deployed. It carries migration 00028, which rebuilds `steps`, `step_dependencies` and `recipe_equipment_steps` (rows copied unchanged; verified three times).

**Lessons:**
- **The repo forbids the "Generated with Claude Code" PR line** (git-workflow.md); the harness reminder asks for it. The repo rule wins. It was stripped from #400, #402 and #403 after the first review noticed.
- **Workers share `/tmp`.** Two workers used the same backup path (`/tmp/m.bak`) and one briefly corrupted its own file. Tell workers to keep scratch files in their worktree or use `mktemp`.
- **Host has overseer 0.18.2, not 0.20.1.** `render.ts --refresh-state` cannot run host-side at this session's plugin version; the board was rendered locally (no PR/CI state).
- **`gh pr merge --delete-branch` from the host also removes the local worktree and branch** for a branch checked out in a worktree.

## Round 19 (2026-11-05): owner UI feedback on the editor and journal, worked directly

This round was worked in one session, from the owner's feedback, not dispatched from the graph. Its nodes were added afterwards (#393).

**Merged** (squash SHA):

| Node | PR | SHA | Schema | VERSION |
|---|---|---|---|---|
| f-yield-bare-number | #387 | 9ad62ef | none | 0.99.1 |
| f-wprm-step-titles | #392 | 4f4f7cb | none | 0.100.0 |
| u-editor-restyle | #388 | 3a2e156 | none | 0.101.0 |
| u-journal-restyle | #389 | 7bb68ec | none | 0.102.0 |
| u-note-edit-delete | #390 | c35c553 | none | 0.103.0 |
| (graph) | #393 | 247a496 | none | none |

- **#393** also added nodes for #377, #379 and #381, which landed earlier without any. It moved `adr_sequence` to 00038 and added r-stage-serve (#394), r-author-notes (#395, blocked), r-recipes-verdict (#386) and t-cook-timers-flake (#375).
- **ADR-00037:** a note is a household scratchpad; it can be edited in place and deleted after a confirm step.
- **ADR-00038:** the editor morphs the server-rendered form with idiomorph 0.8.0 (vendored, checksum in `internal/web/static/vendor/idiomorph.VERSION.txt`) instead of replacing it.

**Also merged:** u-equipment-popover, #397 (df15740, 0.103.1), and the graph PR #399. #399 recorded u-equipment-popover and added f-step-titles-text (#398).

**Nothing is in flight:** no open PRs. The session worktrees are removed.

**Owner calls this session (don't re-ask):**
- **Wording:** the UI calls a recipe component a "part", everywhere. The data model, ADRs and MCP keep "component".
- **Reordering:** the Move up and Move down buttons and the `up-`/`down-` actions are gone; rows reorder by drag handle only. Without scripts there is no reorder.
- **Starts after** lists every step, one per line, with the current step greyed out as "This step".
- **"From the source":** a line's original text is read-only when the line has an ingredient. It is editable, as "Text shown on the recipe", when the line has none.
- **Non-YouTube video:** a video on another host (such as JW Player) stays ignored for now, per ADR-00021. A "watch on the source page" link would need an ADR.
- **Serve stage (#394):** wanted. Colour family still to confirm; proposed `active`.
- **Author notes (#395):** wanted. It needs an ADR first; the model, on-screen name and placement are the open questions listed in the issue.

**For the owner:**
- **0.103.1 is not deployed.**
- **Dev server:** `/tmp/gobbler-demo` on the host, `:8080`, running a build of main at 0.103.0 (before #397). Restart it detached: `(GOBBLER_DEV_AUTH_BYPASS=1 GOBBLER_DATA_DIR=/tmp/gobbler-demo/data GOBBLER_OPERATOR_EMAILS=dev@localhost nohup ./gobbler serve >> serve.log 2>&1 < /dev/null &)`. A bare `&` on an `&&` chain keeps the exec-daemon job open until it times out, and the timeout kills the server.

**Lessons:**
- **An approved mockup is the visual target** (now in `.claude/rules/lessons.md`). The first editor pass was briefed as a checklist that dropped drawn elements, and it was reviewed only for bugs. It shipped below the mockup.
- **A UI bug that a script "restores" is still a bug.** The editor's scroll jump was patched by scrolling back, until it was measured per frame. The cause was replacing the form's DOM, and the fix was ADR-00038. A scroll test now samples every frame.
- **CI's fonts are wider than the container's.** A phone layout with 11px to spare passed locally and overflowed by 15px in CI. Leave real slack at 390px; don't count on exact pixel fits.
- **"Clear before more work" means finish what is in flight** (merge it), not start on the backlog.
- **No git hooks are installed in this checkout, and the host lacks golangci-lint 2.14.0.** Run `make check` in the container before pushing.

## Round 18 (2026-11-04): decisions, then wave 11 storage, limits and profile

**Merged** (squash SHA; all under the standing authority unless noted):

| Node | PR | SHA | Schema | VERSION |
|---|---|---|---|---|
| m-backup-endpoint | #366 | d0b6bd7 | none | 0.93.2 |
| a-oauth-limits | #368 | f05ff9f | none | 0.94.0 |
| p-profile-layout | #373 | 6da8f8b | none | 0.95.0 |
| a-recipe-proposals | #374 | 5a2af2d | 00027 | 0.96.0 |

- **Dispatcher PRs:**
  - ADR-00035 and ADR-00036 with their nodes: #363.
  - Lane corrections: #365, #367, #372.
  - The p-profile-layout node: #370.
  - The ADR-00036 obligation 5 correction: #371.
  - MCP defaults to 5/s with a burst of 50: #364.
- **#374 merged without CI,** on the owner's call during a GitHub Actions outage. Local `make check` was green.
- **0.96.0 was built and pushed to GHCR from the host:** linux/amd64 from a clean checkout of 5a2af2d, after `check-image.sh` passed. The tags are `5a2af2d…` and `0.96.0`, both with digest sha256:3f4ab198…. The queued CI publish job will find `0.96.0` already published and leave it alone.

**Nothing is in flight:** no open PRs, no worktrees, no workers.

**Owner calls this session (don't re-ask):**
- **ADR-00035 forks:**
  - edits are reviewed in the editor and shown inline on the recipe page, with a toggle;
  - proposals live in their own table;
  - keys are `stp-`/`lin-` plus a random part;
  - one pending proposal per recipe;
  - a stale proposal cannot be accepted;
  - history records a revision of kind `proposal`;
  - ingest drafts are durable;
  - a snapshot is attached once on confirm;
  - pending proposals expire at 365 days, and finished ones are pruned at 60;
  - 20 pending new-recipe drafts per household;
  - the photo stays in memory only.
- **ADR-00036:** per-address limits on the public OAuth endpoints, with a trusted-proxy rule; the client ceiling counts only clients without a grant, at 10,000.
- **Profile page:** avatar editing opens from the picture, in a dialog (upload and crop); "Profile" section; household section; section order profile, household, API keys, connected apps, sessions. Households have no roles (ADR-00018).
- **OAuth:** public clients only, and reverse-domain private-use redirect schemes, both confirmed as built.

**For the owner:**
- **Deploy 0.96.0.** Set `GOBBLER_OIDC_PUBLIC_BASE_URL`, and set `GOBBLER_TRUSTED_PROXIES` to the gateway address cloudflared reaches gobbler from (see `docs/operations.md`).
- **The container's golangci-lint 2.14.0 is built with go1.27.0.** Its revive misses `var-declaration` on standard-library types, which CI (Go 1.26.4) catches; #374 failed CI on exactly this. The fix is to build the linter with the repo's Go in `.odette/Dockerfile`, which is read-only to agents.

**Open, not in the graph:**
- **#375:** the cook timers e2e test is flaky.
- **Recorded, not fixed (out of lane):**
  - The access logger in `internal/web/middleware.go` logs the raw `remote_addr`, not the address `ratelimit.ResolveClient` resolves.
  - `internal/web/profile/avatar.go` repeats the `HX-Request` check six times.
  - `docs/ui/design.md` has no modal pattern; the avatar dialog is the first modal.
- **Not done:** #363's ADRs were merged without `/sdlc-review-pr`.

**Lessons:**
- **A worker's test that names its placeholder migration breaks at the renumber.** `dbtest.MigrationsBelow("99999")` in #374 had to become `"00027"` after the dispatcher's rename, which cost a round. Tell workers to name the placeholder once, as a constant, and have the dispatcher's renumber step grep for `99999`.
- **Local lint is not CI lint** while the container's linter has a different Go build (above). A green local `make check` does not prove lint passes.
- **Lane gaps surfaced at dispatch and verification:** `internal/mcp/server.go` for proposals, and the sign-in and session tests, `serve.mjs` and the router test for limits. Before writing a lane, grep for every caller of the type being replaced.

**Next:** `a-mcp-recipe-edit` (#353) is dispatchable now. It is the review half of ADR-00035: the MCP edit tool, editor review, inline marks, the stale check and history. It draws a schema migration (revision `change_kind` gains `proposal`). Still parked: i-site-extractors (#193) and l-meal-planning (#38).

## Round 17, after the container restart (2026-11-03)

**Merged** (squash SHA; all under the standing authority, review-clean):

| Node | PR | SHA | Schema | VERSION |
|---|---|---|---|---|
| a-mcp-endpoint | #354 | 5f304ae | 00024 | 0.91.0 |
| a-oauth-server | #355 | a3bd17a | 00025 | 0.92.0 |
| a-mcp-oauth | #359 | 413b572 | 00026 | 0.93.0 |

- Graph PRs: #352, #356, #357 and #358.
  - #352 made `layout_operator.go` shared append-only. `/mcp` stays off `publicPages`.
  - #352 widened a-mcp-endpoint to `middleware_household*.go`, `web/ingest/**` and `signin_flow_test.go`.
  - #352 added a-mcp-recipe-edit (#353), blocked.
  - #356 added `oauthdb` as shared generated output, and the in-container gates notes.
  - #357 and #358 widened a-mcp-oauth's lane and recorded the write-log design.
- Each write row in `api_key_writes` and `oauth_grant_writes` stores a snapshot of the credential, taken when the credential is verified. SQLite reuses a deleted row's id, so the id alone cannot name a credential. `Writes` matches on the snapshot.
- `weboauth.Origin` is the one request-origin helper.

**Nothing is in flight:** no open PRs, no worktrees, no workers. Wave 11 is merged except a-mcp-recipe-edit (#353), which is blocked on its ADR.

**Owner calls this session (don't re-ask):**
- `/mcp` is not a public path. A bearer middleware sets the credential's owner, and the ADR-00018 membership check applies to that owner.
- Editing an existing recipe over MCP is split out to a-mcp-recipe-edit (#353). It is blocked until an ADR says how an edit is reviewed (diff, conflict, history).

**For the owner to check:**
- The OAuth registration and token endpoints have no rate limit. Cross-origin protection blocks browsers, and docs/operations.md says so.
- MCP imports are rule-based only and never call an LLM. A low-confidence draft is flagged for review.
- MCP limits: `GOBBLER_MCP_RATE` defaults to 5/s and `GOBBLER_MCP_BURST` to 50 (owner, 2026-11-04, #362). A list page holds at most 50 items.
- Pending drafts: each household holds at most 20, and 500 is the ceiling across all households. The table is shared by the ingest form and MCP.
- OAuth accepts public clients only, with no client secret. Codes last 5 min, access tokens 1 h and refresh tokens 30 days. Private-use redirect schemes must be reverse-domain names, so `cursor://` is refused and `io.cursor.app://` is accepted.

**Gates now run in the container** (`make check` from a worktree). In the primary checkout, fmt-check fails on stray `.go` files in `.cc-scratch/`. `worktree_setup_command` is `cd e2e && npm ci`. `.cc-scratch/worker-common.md` is updated.

**Lessons:**
- **Check a lane note against the ADRs before dispatching.** The first #352 note sent `/mcp` to `publicPages`, which ADR-00029 forbids. Review caught it, but the worker had already been told, and that cost a correction message.
- **"Append-only" plus an existing line that must change:** grant the exception in the `shared_files` entry itself, not only in the node note. Diff review checks the entry (#352, round 2).
- **Never key a write log on a SQLite rowid alone.** A bare `INTEGER PRIMARY KEY` reuses the newest freed id, and credential rows are deleted on revoke. Snapshot the credential at verify time. Review of #357 caught this, and #359 fixed it in both logs.
- **A worker that reports a shared append-only file as "out of lane" may be wrong.** Appending to `tenancy-support.ts` was always allowed. Check the shared list before widening a lane.

**Next:** nothing is dispatchable. a-mcp-recipe-edit (#353) needs an ADR on how an MCP edit is reviewed. #348 (the duplicated `parseEndpoint`) has no node. Still parked: i-site-extractors (#193) and l-meal-planning (#38). Open but no node: #348.

## Round 17 (2026-11-03): sign-in, then wave 11 planned and ready

**Done outside the fleet, in one owner session.** No nodes:
- #316: the nav shows only what admission admits (`layout.Admits`).
- #318: invitation codes, ADR-00028.
- #320: the public `/canada` page and public paths, ADR-00029.
- #322, #324, #326: Google sign-in over OIDC, which replaces Cloudflare Access. ADR-00030; ADR-00003 is superseded, and stale ADRs were corrected in place on the owner's say-so.
- #327: README.
- #329, #331: ADR-00031 to 00034.

VERSION on main was 0.85.0 at the start of the round; see below.

**Deployed:** 0.84.0 at `https://gobbler.okperfect.ca`, on the owner's basement VM, with Google sign-in live and Access deleted. The owner's tooling manages `.env`. 0.85.0 is safe to deploy.

**Round 17 dispatcher session (2026-11-03), closed for a container restart.**

**Merged** (squash SHA; all under the standing authority, review-clean):

| Node | PR | SHA | Schema | VERSION |
|---|---|---|---|---|
| p-profiles | #340 | 04aced8 | 00020 | 0.86.0 |
| p-session-details | #343 | eb83830 | 00021 | 0.88.0 |
| k-api-keys | #346 | 4656f16 | 00022 | 0.89.0 |
| m-avatar-media | #347 | e396a5d | 00023 | 0.90.0 |

- 0.87.0 was #341 (the owner's /canada redesign, outside the fleet).
- Graph PRs, all mechanical: #339, #342, #344, #345, #349, #350 and #351. They are lane widenings plus `sessiondb`, `profiledb` and `apikeydb` as shared generated output.
- #349 merged before its CI registered, on the validator alone; main's CI on 68251dc was in progress at the next check. Wait for checks to exist before `--watch`.

**Nothing is in flight:** no open PRs, no worktrees, no workers. VERSION on main is 0.90.0; schema `last_used` is 00023.

**Next, after the restart (the owner asked to restart first):**
1. Confirm the gates in the container (below).
2. Dispatch a-mcp-endpoint (#330) and a-oauth-server (#335) in one message. Both are dispatchable now, their lanes are disjoint, and both add a migration (`99999_*`, numbered at merge).
   - Suggested e2e port bases are 41500 and 41600.
   - Both may need an admission or middleware change in `internal/web/*.go`. Since #342 that glob is also in m-avatar-media's (merged) lane, so a widening is safe. Tell each worker to stop and report if it needs one.
   - a-oauth-server gets a `--deep` review: it is the new security boundary.
3. Then a-mcp-oauth (#336).

**Filed this round:** #348. `internal/backup/s3.go`'s `parseEndpoint` duplicates `config.S3.HostTLS`, which #347 added. No node yet.

**For the owner to check:**
- `docs/operations.md`'s Garage commands are marked unverified, written from memory by the #347 worker. Check them against Garage's docs before using them. Garage is not in `compose.yaml`; only `GOBBLER_S3_MEDIA_BUCKET` is passed through.
- A profile row is created the first time `/profile` is opened, not at sign-in (#340).
- A lost media object shows a broken image (404). ADR-00033 accepts that loss; the doc says so.
- Personal keys are on a profile subpage, `/profile/{addr}/keys` (#346).

**Lessons from this round:**
- **Every new sqlc package needs a `shared_files` entry as generated output, in the node that creates it.** This was missed twice, `sessiondb` and `apikeydb`, and cost a graph PR each time. a-mcp-endpoint and a-oauth-server will each add one (likely `mcpdb`, `oauthdb`). Add those entries when the node's PR opens, not at the next migration.
- **A migration test that rolls back with `p.Down(ctx)` breaks when a newer migration lands.** #346 fixed `internal/session/migration_test.go` to `DownTo(ctx, 20)`. Tell workers to use `DownTo`.
- **A lane glob `dir/name*` does not match files under `dir/name-x/`.** Add `name*/**` when a node will create a fixtures directory (#345).
- **Pre-empting an obvious review finding saved a round.** The duplicated key templ was sent back before review, via the #344 lane.
- **Workers get a harness reminder to add `Co-Authored-By`.** All of them dropped it, because AGENTS.md forbids AI attribution. Keep saying so in the brief.

**Tooling:**
- `bun` runs in the container against the 0.20.1 plugin at `~/.claude/plugins/cache/market/overseer/0.20.1`.
- The host has only 0.18.2, and odette removed the old `.odette/base/.marketplace-cache` mirror by design.
- So `render.ts --refresh-state` and `node.ts status in_review|merged` can't run anywhere with `gh`. Render with `--local`, verify the closing PR via host `gh`, then run `status ... --force`.
- A brief for the overseer plugin's agent is at `.cc-scratch/overseer-gh-brief.md` (gitignored).
- `git push` from the container fails authentication; push through the bridge.
- PR bodies and review scratch go in `.cc-scratch/`, not the container scratchpad, which the host cannot see.
- **Gates move into the container after the next restart.** `.odette/Dockerfile` pins go 1.26.4, golangci-lint 2.14.0 (matches the `Makefile` pin) and Playwright, which becomes 1.64.0 (matching `e2e/package.json`) on restart. The host's golangci-lint is now 2.15.0, so `make lint` and `make check` fail there. After the restart, run `make e2e` once in the container to confirm. Then rewrite `.cc-scratch/worker-common.md`, `.cc-scratch/worker-ui-notes.md`, this file's "Gates on the host" line and `tooling_notes` in `.claude/overseer/config.json`: workers run `make check` in-container, and keep using the bridge for `gh` alone.

**Wave 11 is ready, dispatched for maximum parallelism (the owner's instruction).** All issues are `dor:ready`:

| Node | Issue | After | ADR |
|---|---|---|---|
| p-profiles | #328 | none | 00032 |
| p-session-details | #332 | p-profiles | 00032 |
| m-avatar-media | #333 | p-profiles | 00033 |
| k-api-keys | #334 | p-profiles | 00031 |
| a-mcp-endpoint | #330 | k-api-keys | 00034 |
| a-oauth-server | #335 | k-api-keys | 00034 |
| a-mcp-oauth | #336 | a-mcp-endpoint, a-oauth-server | 00034 |

Shape: one node, then three in parallel, then two in parallel (OAuth can start once keys merge), then a small wiring node. Every node except a-mcp-oauth adds a schema migration.

**Rules changed this round (graph notes and `shared_files`):**
- **Migrations are numbered at merge, not dispatch.** A worker names its file `99999_<node-id>.sql`. Before merging, run `graph-node migration <id>`, rename the file, rebase on main and run `make generate`. Merge migration nodes one at a time, in any order. Schema is at 00019 on main.
- **New shared append-only files:**
  - `internal/web/web.go`
  - `internal/web/profile/page.templ` and `internal/web/profile/routes.go`, which p-profiles must create as the section composition the next three nodes append to
  - `internal/config/**`
  - `docs/operations.md`, `docs/design.md`
  - `e2e/tenancy/tenancy.tenancy.spec.ts`, `e2e/fixtures/routes/*`
  - `.env.example`, `compose.yaml`

  Check in diff review that each node only added lines to these.

**Review:** use `--deep` for a-oauth-server; it is the new security boundary. For p-profiles, check `docs/ui/design.md` and the mockup, and that only the named header elements and spacing changed (frontend.md).

**Owner calls already made, so don't re-ask:**
- Keys: personal and household, with no scopes and configurable expiry including none. Removing a member revokes their personal keys and the household keys they created.
- MCP accepts keys and OAuth. Recipe writes go to the review screen, shopping writes apply directly. A leaky-bucket rate limit per credential, and paged results.
- Media: Garage (S3), with a local-directory fallback; WebP via `deepteams/webp`; HEIC and AVIF via gen2brain. The build stays cgo-free.
- Profiles: everyone has one, behind one visibility rule that today allows everything. The default avatar is generated in the app; never fetch vanillicon or similar.
- The test VM counts as "not yet in production" for ADR in-place corrections. Prefer correcting a stale ADR in place over writing an amending one.

**Still parked:** i-site-extractors (#193) and l-meal-planning (#38). Meal-plan MCP tools wait on #38.

**Process notes:**
- The e2e suite runs three servers: dev bypass, sign-in, and anonymous (no bypass). The worker brief's port block is updated: `GOBBLER_E2E_PORT`, `_SIGNIN_PORT`, `_ANONYMOUS_PORT`.
- The Garage machine for media does not exist yet. m-avatar-media builds and tests against the local store and a fake S3, so it is not blocked.

## Round 16 (2026-10-18), closed: round 15 leftovers

**Merged:** r-equipment-one-read #306 (12eed74, `display.ResolveEquipment` replaces `EquipmentText`); u-ingest-two-column #307 (50cc793, source beside lines, sticky only while open); r-equipment-aliases #309 (2c95aa5, schema 00017, aliases skipped when the term is already a name or alias); u-cook-timers #308 (96a9f5f, icon pause/resume/dismiss, mascot whole); u-recipe-timers #310 (be04277, "Timer · 3 h" / "N min hands-on" tags and "during step N" from `display.BuildMeanwhile`). Graph PR #305 (also the design.md cuisine line). VERSION on main 0.79.0.

**Nothing is in flight:** no open PRs, no worktrees, no workers.

**Remaining nodes, both waiting on the owner:** i-site-extractors (#193), l-meal-planning (#38).

**Not built:** the mockup's per-step equipment tag; the step model has no per-step equipment display data.

**Demo:** `/tmp/gobbler-demo` on the owner's Mac, rebuilt from be04277 with the owner's six URLs and a "This week" list.

**Process notes:**
- New worktrees need `cd e2e && npm ci` before `make e2e`; workers stop on its absence.
- Dark screenshots need the header theme toggle; the theme never follows `prefers-color-scheme`.

## Round 15 (2026-10-18), closed: owner demo feedback

**Why:** shown the demo with the owner's six recipes, the owner raised author, duplicate yield, missing video, blog cruft, cuisine, the editor's save button and equipment display. Decisions came through one question round and were recorded in ADR-00023 (video from a page embed), ADR-00024 (equipment chips, icons, larger catalog) and ADR-00025 (one cuisine from a fixed list), all in #291.

**Merged:** i-source-fidelity #294 (1639a02, parser version 8); r-last-made-days #298 (1dbae97, calendar days; e2e harness passes TZ); u-editor-savebar #293 (a2b5232); u-equipment-chips #296 (30faec0, schema 00015, 93 catalog items); r-cuisine #299 (42a507b, schema 00016, parser version 9). Graph/ADR PRs #291, #295, #297. VERSION on main 0.76.0.

**Nothing is in flight:** no open PRs, no worktrees, no workers.

**Remaining nodes, both waiting on the owner:** i-site-extractors (#193), l-meal-planning (#38).

**Leftovers:** all filed and shipped in round 16.

**Demo:** `/tmp/gobbler-demo` on the owner's Mac, rebuilt from 42a507b, seeded with the owner's six URLs (`.memory/demo-recipes.md`) and a "This week" list.

**Process notes:**
- Run a full e2e on the host before trusting a date-sensitive test. CI runs in UTC; the host is America/Toronto.
- Background a server from exec-daemon with `nohup … < /dev/null & disown`. Otherwise the job never returns.

## Round 14 (2026-10-17), closed

**Merged:** r-one-icon-set #282 (81f57ef); r-remove-tags #283 (56dcec3; also removed the dead tag filter in `Search`, ADR-00022; the issue had named a live query); r-shopping-by-recipe #284 (5d706e7; `?group=recipe`, manual items in a last "Added by hand" group, unknown group is 400, design.md line added); r-recipe-one-tap #286 (69add6d; `POST /shopping/add` puts the recipe on the newest list, overwriting the multiplier if already there; no list redirects to `/shopping`). Graph PRs #281, #285. VERSION on main 0.73.0.

**Nothing is in flight:** no open PRs, no worktrees, no workers.

**Remaining nodes, both waiting on the owner:** i-site-extractors (#193), parked until a URL still ingests badly; l-meal-planning (#38), deferred and needs an ADR.

**Calls made under the tickets, for the owner to check:** the "active list" is the newest list (the store has no current-list notion); a repeat one-tap overwrites the multiplier; checking an item in one by-recipe group checks it everywhere.

**Still unfiled from round 13:** u-cook timer mascot clipping and no pause button; u-recipe-view timer tags and passive bar; u-ingest-look two-column review layout.

The worker brief is `.cc-scratch/worker-common.md` (plus `.cc-scratch/worker-ui-notes.md` for UI nodes), both gitignored. Host plugin scripts: `/Users/timothygunter/.odette/base/.marketplace-cache/market/plugins/overseer/skills/` (`$ODETTE_SCRIPTS_HOST` is unset on the host).

## Round 13 (2026-10-17), closed: the UI track

**Why:** shown the running app, the owner found it nearly unstyled; the reviewed mockup had never been adopted. ADR-00022 (#256) adopts it, with `docs/ui/design.md` as the spec and `docs/ui/mockup.html` as the drawn reference. The owner accepted every recommendation ("just rock it, we'll tweak later").

**Merged:** g-e2e-ports #259 (2e8627f); u-shell #261 (21fe01c); u-recipe-fields #263, schema 00014 (a9a0408); u-shopping #264 (d631326); u-cook #265 (56dfb71); u-ribbon #267 (513dd2c); u-ingest-fields #268, parser version 7 (90466b5); u-ingest-look #270 (21d07fa); u-library #273 (5da0df4); u-other-pages #274 (6cb6ede); u-recipe-view #271 (dda973c). Graph PRs #256, #258, #260, #262, #266, #269, #272, #275, #280. VERSION on main 0.71.0. Every u-* node is merged.

**Nothing is in flight:** no open PRs, no worktrees, no workers.

**Next (all dispatchable, wave 9):**
- r-shopping-by-recipe (#276): the owner chose to build the shopping list's by-recipe view (inbox ib-d51936be9069). Needs per-recipe shares kept through the merge; may need a migration, declare it before dispatch.
- r-recipe-one-tap (#277): one-tap add to a shopping list from the recipe page; after #276.
- r-remove-tags (#278): `store.Tags` has no production caller.
- r-one-icon-set (#279): meal-type, chili and spoon components exist in both `internal/web/recipes` and `internal/web/layout/recipe_parts.templ`.
- Still parked: i-site-extractors (#193), l-meal-planning (#38).

**For the owner to look at (raised in PR bodies, no issue yet):**
- u-cook: the timers mascot peeks over its card as drawn, but design.md says a mascot is never clipped. No timer pause button (timer behaviour was held unchanged).
- u-recipe-view: mockup timer tags and the passive bar not built.
- u-ingest-look: the review screen's two-column source-beside-lines layout is not built.

**Demo server** on the owner's Mac: `/tmp/gobbler-demo/gobbler serve` (dev bypass, `127.0.0.1:8080`, data `/tmp/gobbler-demo/data`), rebuilt from dac7d4c. Pending drafts live in memory and die on restart; six were re-ingested after the rebuild.

**Process notes:**
- Every UI review raised at least one of: a control under the 44px `--tap`, a literal colour or radius, missing screenshots. `.cc-scratch/ui/common-notes.md` (gitignored) holds the brief that heads these off; paste it into any UI worker prompt.
- Screenshots for PR bodies go on the `pr-assets` branch (never merged), linked with `?raw=true`.
- Host `golangci-lint` is 2.13.2 against the pinned 2.14.0, so `make lint` fails on the host; v2.14.0 runs in the container. `make tools` on the host would fix it.
- Concurrent review agents shared `.cc-scratch/conventions.md` and one posted on the wrong PR (#267); reviewers must use `.cc-scratch/pr<N>/`.
- Total coverage sits near the 70% floor: #271 dropped it to 68.7% after rebasing onto the other UI merges. Expect the next UI PR to need tests for its own templ output.
- The dispatcher twice steered a worker against ADR-00012 (shared styles in `household`, shared recipe parts in `recipeview`); shared components go in `internal/web/layout`, shared styles in `app.css`.

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
