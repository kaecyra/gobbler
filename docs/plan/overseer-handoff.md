# Overseer handoff: 2026-11-11, wave 13: #615 and #606 in flight, plan PR #625 in review

This file holds the volatile state the graph does not. The durable record is `docs/plan/graph.json`. Read `VERSION` on main for the current version; do not trust a number written here.

**Keep this file current.** Update it at every task start and stop, and commit it straight to `main` as bookkeeping. Every session ends on a handoff.

## Wave 13 round 1 (2026-11-10, evening)

**Merged this round** (standing authority, each review-clean):

| What | PR | SHA |
|---|---|---|
| chore(plan): c-cuisine-asian-bbq owns internal/store/recipe_fields_test.go | #622 | 71f4a36 |
| feat(recipe): Asian and BBQ cuisines (c-cuisine-asian-bbq, migration 00039) | #623 | ae72d3d |
| feat(settings): integration settings store (s-settings-store, migration 00040) | #624 | ce5a56e |

**In flight:** e-invite-email (#615), worktree `.claude/worktrees/wt-615`, branch `feat/615-invite-email`, migration placeholder `99999_e-invite-email.sql` (numbered at merge; 00041 if nothing else lands first), VERSION 0.145.0.

**Also in flight:** p-photo-vision-ocr (#606), worktree `.claude/worktrees/wt-606`, branch `feat/606-photo-vision-ocr`, migration placeholder `99999_p-photo-vision-ocr.sql`, writes ADR-00053. The owner answered on 2026-11-10: store the tesseract text as its own snapshot kind (comment on #606). Its lane widening (`internal/store/**`, `internal/api/recipes/types.go`, needs_migration schema) and #616's (`internal/web/ingest/**`, `e2e/ingest/**`) are in dispatcher PR #625. Run ownership checks for #606 only after #625 merges.

**Scope move (dispatcher):** removing `GOBBLER_LLM_*`, `GOBBLER_ANTHROPIC_API_KEY` and `GOBBLER_GEMINI_API_KEY` (ADR-00057 obligation 6, LLM half of 7) moved from #618 to #616. Comments are on both issues and notes are on both nodes. #616's lane widening is in PR #625.

**Next three:** p-photo-vision-ocr (#606) once answered; then l-llm-provider-config (#616) after #606 and the lane PR (it shares `internal/settings/**`, `internal/web/settings/**` and `e2e/operator/**` with #615, so it goes after #615); then u-llm-usage-cost (#619).

**Notes:** PR bodies carry no AI attribution (git-workflow rule), and the review prompt template is at `.cc-scratch/review-prompt-template.md` (fill `{{PR_NUMBER}}`). `gh pr merge --delete-branch` run host-side also removes the local worktree.

## Wave 13 decisions and landings (2026-11-10, late)

**Merged** since `d893933` (read VERSION on main; it was 0.142.1 at this writing):

| What | PR | SHA |
|---|---|---|
| chore(plan): nodes p-photo-vision-ocr (#606), y-youtube-description (#607) | #608 | 8edbc50 |
| chore(plan): node c-cuisine-asian-bbq (#609) | #610 | 7da8cd0 |
| fix(editor): labels for scaling rules and qualifier kinds (#611) | #612 | 851650a |
| feat(layout): centred error pages with a mascot, explained invitation 404 | #614 | 7885b21 |
| chore(plan): blocked nodes e-invite-email (#615), l-llm-provider-config (#616) | #617 | 6f3720f |
| chore(web): ship every mascot drawing, keep their source art; ignore .DS_Store, /feedback/ | #621 | 387ad30 |
| docs(adr): ADR-00055 to ADR-00058, nodes s-settings-store and u-llm-usage-cost | #620 | c3caf06 |

**In flight:** see round 1 above.

**Owner decisions this session (via /overseer:decide on #615 and #616; don't re-ask):**
- **ADR-00055, invitation email:** SMTP submission; TLS mandatory (port 465 implicit, any other port must offer STARTTLS, certificate always verified, no off switch); HTML plus a plain-text part, no image of any kind; tracking off at the provider as a written deploy step; send status stored per invitation with a Resend button on every pending invitation; per-sender leaky bucket shared by invite and Resend, rate and burst from the environment.
- **ADR-00056, LLM providers:** text and vision each pick provider and model; models chosen from the provider's listing API (Claude filtered by `active`, `structured_outputs` as a stand-in for tool use, plus `image_input` for vision; Gemini by content generation); set on the Deployment page, live without restart. Failover lists rejected.
- **ADR-00057, integration settings:** mail and LLM settings live in the database; secrets AES-256-GCM under an HKDF-derived key from one environment variable, field names as associated data; write-only on the page; a missing or wrong key starts the app with those integrations off. `GOBBLER_LLM_*` and the provider key variables are removed, not imported. Per deployment, not per household. Amends ADR-00007 and the AGENTS.md secrets boundary (the rewording is #618's obligation 1). Modelled on pennyroyal's `packages/crypto/src/local_key_manager.ts`.
- **ADR-00058, usage and cost:** every LLM call recorded with cost (integer millionths of a dollar, at the price in force); prices from a daily OpenRouter sync (exact `anthropic/<id>` / `google/<id>` match only) with operator overrides; its own operator page. Modelled on trellis `packages/ai/src/pricing/` and `apps/web/src/routes/_app/admin.ai-costs.tsx`.
- **Mascots:** every drawing ships in `internal/web/static/img/`, used or not. Source sheets are `docs/ui/mascots/source-{heads,bodies}.jpeg` (six heads, three bodies; header `gob` is not on them).

**Graph (wave 13), dispatch order:**
1. Ready now, in parallel: **c-cuisine-asian-bbq (#609)** and **s-settings-store (#618**, ADR-00057, needs a schema migration number).
2. **p-photo-vision-ocr (#606)** after #609. It writes ADR-00053 (reserved), which must settle what a failed vision call does.
3. **e-invite-email (#615)** after #618. Needs a migration number.
4. **l-llm-provider-config (#616)** after #606 and #618.
5. **u-llm-usage-cost (#619)** after #616. Needs a migration number. Token counts do not cross `ingest.Provider` today, so this node changes both providers and the `llmtest` contract.
- `e-invite-email` and `l-llm-provider-config` both own `internal/settings/**`, `internal/web/settings/**` and `e2e/operator/**`; validate.ts warns. Sequence them.

**Waiting on the owner:** y-youtube-description (#607): YouTube Data API (key) or the watch page's player JSON. It also waits on #606.

**Still parked:** l-meal-planning (#38), i-site-extractors (#193), o-demo-reseed (#432), unchanged.

**ADR sequence:** last used 00058 (00053 reserved for #606, 00054 for #607).

**Lessons:**
- **Pre-push hooks did not run on the host** (#621 pushed in 1.6s). CI's `check` job is the only gate on a push from this setup; wait for it before merging.
- **`gh pr checks` right after a push reports "no checks reported"** until the run registers. Sleep, then `--watch`.

## After round 28 (2026-11-07 to 2026-11-10): landed without a handoff update

These merged after round 28 closed, but this file was not updated as they landed. This section was rebuilt on 2026-11-10 from `git log d2bf9e05..origin/main`.

| What | PR | SHA |
|---|---|---|
| chore(plan): o-demo-reseed deferred to the owner | none | 80a47dca |
| chore(plan): nodes for #554, #555, #557, #558, #559, #561 | #562 | 1fee3823 |
| feat(web): show how many recipes use each ingredient on /catalog | #556 | 8dcc4c74 |
| feat(web): operator page to edit central catalog ingredients | #560 | fcfe07f8 |
| feat(web): comma between qualifiers in the recipe ingredient list | #563 | 53bcc98f |
| feat(recipe): add mix as a step stage kind | #564 | 51a6efac |
| feat(web): render step and author-note text as a safe markdown subset | #565 | 03c8f4dd |
| feat(web): cook mode drawn after the approved mockup | #566 | 15c87534 |
| chore(plan): record #556, #560, #563, #564, #565 and #566 merged | none | d88ae468 |
| Operator area: one menu entry, an overview and a section rail | #569 | c936f839 |
| feat(web): wording and layout tweaks across home, library, recipe and operator pages | #572 | 95786326 |
| feat(web): show the winking mouse in the suggestion block | #575 | 4f98532d |
| feat(web): recipe view pages at /recipes/{id}-{slug} | #574 | fdd5c1a4 |
| docs(adr): ADR-00048 agent-driven import | #577 | bddb54d6 |
| feat(mcp): preview_import and fuller get_recipe (ADR-00048) | #578 | a2de6a48 |
| feat(household): house style for agents (ADR-00048) | #579 | 4a2f2014 |
| chore(plan): record migration 00035 used by #579 | none | 01089051 |
| feat(mcp): revise held recipe drafts (ADR-00048) | #580 | f665b60b |
| feat(mcp): report duplicate source addresses on import and preview (ADR-00048) | #582 | 207e25a1 |
| fix(web): let the house style textarea span its panel | #584 | b9637cce |
| feat(web): list pending drafts on /ingest and count them in the menu (ADR-00049) | #587 | dd034002 |
| fix(web): drop the review page's narration and count every flagged item | #589 | fa5f3553 |
| fix(picture): fall back to a 1x size floor when no candidate meets 2x (ADR-00050) | #590 | 240df56d |
| feat(web): mark the LLM's values with glitter instead of a text chip | #592 | 09213b95 |
| feat(web): choose a saved recipe's picture on its edit page (ADR-00051) | #593 | 7034dab0 |
| fix(e2e): discard the drafts each test leaves on the shared server | #597 | 7cd45e8e |
| fix(ingest): pick a draft's picture candidate by ID, not position (ADR-00052) | #595 | a50b3549 |

**Graph:** `adr_sequence.last_used` had stayed at 00046 while ADRs 00047 to 00052 landed. It was moved to 00052 through `graph-node adr-sequence set`.

**In flight:** nothing. No PRs are open.

**New nodes** (#598, 971e543), which also moved the schema migration mark from 00035 to 00037:
- e-flake-avatar-crop (#567): dispatchable.
- e-flake-ingest-cancel (#581): dispatchable.
- d-reimport-reason (#586): blocked on a schema decision. It depends on e-flake-ingest-cancel, since both own `internal/web/ingest/**` and the ingest e2e spec.

**Merged later on 2026-11-10:** e-flake-avatar-crop (#600, f1b52b7, 0.141.3) and e-flake-ingest-cancel (#601, 6b0d983, 0.141.4). #601 also fixed the same race in `openSaved`, after a lane widening (#602).

**#586 ruled** (#599): a proposal's reason is only ever an agent's own words. ADR-00048 and ADR-00049 are amended. d-reimport-reason merged as #603 (1960be7, 0.141.5), after two review rounds: the migration's filter now matches only the exact sentence the app wrote, and a test pins History's host to the saved version. Lane corrections #604 and #605 (the latter: I left reimport_test.go out when writing the lane). Nothing in flight.

**Known test weakness, no issue yet:** "a proposed new ingredient is offered as new…" (`e2e/ingest/ingest.spec.ts:335`) fails under `--repeat-each`, because its first run creates the catalog ingredient. A single run passes.

**Blocked nodes:** l-meal-planning (#38), i-site-extractors (#193) and o-demo-reseed (#432), unchanged from round 28.

**Cleanup (2026-11-10):** all agent worktrees and merged local branches are removed, along with the stray `refs/remotes/pr/593`. `origin/pr-assets` is kept because PR bodies link to its screenshots.

## Round 28 (2026-11-07): notes apart, baked, library search, the API split

**Merged** under the standing authority (read VERSION on main):

| What | PR | SHA |
|---|---|---|
| graph: node u-suggest-paths | #539 | a290f0b |
| ADR-00021 and design.md: video at the top of the steps column (#540) | #541 | cf13689 |
| u-suggest-paths (#522) | #542 | 01c20db |
| u-notes-apart (#527, folds #537 and #540) | #543 | 4619245 |
| graph and ADR-00022: node u-meal-baked | #545 | 9ac3d28 |
| a-api-catalog (#514), including operator ops | #546 | 86b59da |
| ADR-00043: operator operations check operators, not membership | #547 | 5677062 |
| u-meal-baked (#544): baked type, meal words in `internal/recipe`, migration 00033 | #548 | 30dba64 |
| library search box at full width (owner request, no issue) | #549 | c382673 |
| a-api-households (#515) | #550 | 41435c3 |
| a-api-ingest (#518) | #551 | 9e49949 |
| a-api-profile (#516) | #552 | b7256d8 |
| a-api-status-table (#517) | #553 | 15a9695 |

**In flight:** nothing. a-api-status-table merged as #553 (15a9695), which completes the API split.

**After it:** the graph has only blocked nodes left:
- l-meal-planning (#38) needs an ADR.
- i-site-extractors (#193) is parked.
- o-demo-reseed (#432) is deferred: the owner will do it.

**Owner rulings this round** (memory: feedback-speed-no-duplicate-gates):
- Each gate runs once: the worker's make check, then CI. The dispatcher does not re-run it.
- Owner-ruled doc, ADR and graph PRs merge without a review agent.
- Never park in-scope work in a follow-up node. Rule on it and finish it in the PR.

**Lessons:**
- Workers still add `Co-Authored-By` despite the brief (#551's MUST). Check `git log origin/main..<branch> --format=%B` before every push.
- A `git pull --rebase` on main with local bookkeeping commits went wrong. Use `git reset --hard origin/main` and redo the `node.ts` writes; they are idempotent.

## Round 27 (2026-11-06): landed the round 26 leftovers, then stopped

The owner said: "Just finish up what's in flight now (fully) but don't dispatch more." Nothing was dispatched beyond the two open PRs.

**Merged** (squash SHA, under the standing authority; read VERSION on main):

| Node / PR | PR | SHA | VERSION |
|---|---|---|---|
| (graph: u-save-errors-visible owns `internal/web/editor/errors_test.go`) | #538 | 1f899fa | none |
| a-api-journal (#511) | #532 | cd31e01 | 0.123.12 |
| u-save-errors-visible (#524) | #535 | 749324e | 0.123.13 |

- #532: a worker merged main (conflicts in VERSION, docs/design.md's not-migrated sentence and the `.golangci.yml` journal exceptions). I verified the review fixes in 42aa5aa and resolved all four threads by ID. CI was green.
- #535: I verified fix commit 88eb622 against all four threads and resolved them. The lane widening #538 cleared ownership (0 outside). A worker merged main cleanly and bumped to 0.123.13. CI was green.
- `record-done` is committed for both. The shared stash leftover ("restored: popped by mistake from wt-528") is dropped, and the stash stack is empty.

**In flight:** nothing. No worktrees, no open worker or dispatcher PRs, and the inbox is clear.

**Next to dispatch, when the owner restarts the fleet:**
1. **u-notes-apart (#527)**, ADR-00045. It is the only ready unblocked UI node. Fold #537 into it, as round 26 noted.
2. **o-demo-reseed (#432)**, ready, but it needs the owner to deploy first. The VM last ran 0.123.3, and main is at 0.123.13.
3. **a-api-catalog (#514)**, the largest API node. It waits only on u-notes-apart now. Give it a round of its own. Then a-api-households (#515), a-api-ingest (#518), a-api-profile (#516) and a-api-status-table (#517) follow in dependency order.
- Still blocked on owner decisions: l-meal-planning (#38), i-site-extractors (#193).
- #522 (a named constant for the suggest asset paths) still has no node.

**Lessons this round:**
- The harness's attribution reminder conflicts with the repo rule again. #538's first push carried a Co-Authored-By trailer and the "Generated with" line, and review flagged it as a MUST. Check commit messages and PR bodies before every push (memory: no-ai-attribution-in-prs).
- A mechanical lane widening needs no review agent (memory: feedback-mechanical-graph-prs). #538 got one anyway. Read `.memory/MEMORY.md` before the first graph PR of a session.
- `gh pr merge --delete-branch`, run from the primary checkout through the bridge, also removed the merged branch's local worktree and branch. `git worktree remove` afterwards reports "not a working tree". That is expected, not a fault.

## Round 26 (2026-11-06): API split, note length, recipe page polish (owner away)

**Merged** under the standing authority. Read VERSION on main; do not trust a number written here.

| Node / PR | PR | SHA | VERSION |
|---|---|---|---|
| u-qualifier-chip | #505 | e738243 | 0.123.3 |
| a-mcp-caller | #520 | 2073cc4 | 0.123.4 |
| i-pasted-numbers (owner ruled next-number) | #506 | 6081c12 | 0.123.5 (ParserVersion 18) |
| u-aisle-suggest | #519 | c242d26 | 0.123.6 |
| a-api-pictures | #530 | 12f4c0f | 0.123.7 |
| (graph) a-api-rest split into 8 nodes, #511–#518; #471 closed | #521 | 6c12162 | none |
| (ADR-00044, nodes r-note-length #523, u-save-errors-visible #524, u-qualifier-style #526) | #525 | d859bb1 | none |
| (ADR-00045, nodes u-notes-apart #527, u-card-total-time #528) | #529 | 918ee00 | none |

**Also merged since:** #533 r-note-length (ec2509a, 0.123.8). This is the owner's confirm fix. The owner should redeploy 0.123.8 and retry the sallysbakingaddiction import.

**Open worker PRs, in merge order.** Each worker that wrote one was told what to do next, but those workers die with this session. If a report never arrived, read the branch:
1. **#531 a-api-sharing (#512): MERGED** (5d8d4e9, 0.123.9).
2. **#534 u-qualifier-style (#526): MERGED** (0.123.10).
3. **#532 a-api-journal (#511)**, wt-511. Review approved, and its findings are fixed (42aa5aa, pushed). It still needs to merge origin/main: conflicts on design.md's "not migrated yet" sentence (after #531 and #530 it lists only "the status table") and the `.golangci.yml` journal exceptions. Re-bump to the next free patch, then merge.

**Also open, worker PRs from late in the round** (reviews launched in background agents; read what is posted on GitHub):
4. **#535 u-save-errors-visible (#524)**, wt-524. Review findings are fixed and pushed (88eb622; merge 2dec5f7; VERSION 0.123.12 at ef6fc8f). Screenshots are in .cc-scratch/shots-524-after2-*.png. **Blocked on ownership:** the worker added `internal/web/editor/errors_test.go`, which is outside the lane. First open a dispatcher graph PR: `node.ts owns u-save-errors-visible --add internal/web/editor/errors_test.go` (a-api-catalog, later, also owns editor/**, and the overlap is ordered). Review it and merge it, re-run check_ownership, then verify the fix diff, resolve the review threads by ID, wait for CI, merge.
5. **#536 u-card-total-time (#528): MERGED** (0.123.11). u-notes-apart (#527) is now dispatchable; fold #537 into it.
- **Stash incident:** wt-524 and wt-528 both used `git stash`, and each pop took the other's changes. Both workers repaired their trees, and ownership holds on both branches. One labelled leftover stays on the shared stack: "restored: popped by mistake from wt-528". It is wt-524's already-committed WIP; drop it once #535 merges. Worker briefs now forbid `git stash`, via the scratchpad common-tail. Add the same rule to the worker contract and lessons.

**Next to dispatch:** u-notes-apart (#527), after #534 and #528 merge; then a-api-catalog (#514) once sharing, save-errors, qualifier-style and notes-apart have all merged. #522, a named constant for the suggest asset paths, has no node.

**Owner context:**
- Deployed 0.123.3. "Confirm and save" on sallysbakingaddiction soft dinner rolls did nothing: the draft's 700- and 550-character notes were refused, and the refusal rendered below the fold. The owner ruled ADR-00044: length is never refused, and the existing low-confidence flag is enough.
- The owner's design calls, all recorded as nodes: qualifiers muted italic with no commas (#526); notes apart from the steps with no connector lines (ADR-00045, #527); cards show total time only, not the ribbon (#528). The card shows the figure `timeToTable` groups by, which can differ from the recipe page's "Ready in about…"; say so if the owner asks.
- The inbox is clear.

**Lessons this round:**
- `node.ts` writes the graph on the primary checkout's current branch. Run `record-done` and `status` only with main checked out.
- Bulk thread replies matched on text patterns hit the wrong thread. Match on thread IDs.
- Several a-api PRs edit the same design.md sentence and the same `.golangci.yml` block. Each merge forces a conflict-resolving re-bump on the next. Next time, sequence them, or keep the shared sentence out of the node lanes.

## Round 25 (2026-11-06): recipes API, step splitting, editor fixes

**Merged** (squash SHA; under the standing authority):

| Node | PR | SHA | VERSION |
|---|---|---|---|
| a-api-recipes | #494 | cdadce1 | 0.122.1 |
| (graph: u-qualifier-chip) | #497 | 90e90da | none |
| (graph: i-line-qualifiers) | #499 | 65290d4 | none |
| i-split-steps | #500 | c5ec13c | 0.123.0 (ParserVersion 16) |
| u-editor-suggest-list | #501 | d5064c5 | 0.123.1 |
| (graph: i-pasted-numbers) | #503 | 4d1228e | none |
| i-line-qualifiers | #504 | 06625bc | 0.123.2 (ParserVersion 17) |

**Also merged late in the round:** #507 (76f010f, i-pasted-numbers lane gains `internal/parse/meta_test.go`) and #510 (4bd1719, nodes u-aisle-suggest #508 and a-mcp-caller #509, both now `dor:ready`).

**Open at handoff. Read the PR threads and inbox before acting:**
- **#505 u-qualifier-chip (#496)**, branch fix/496-qualifier-chip, wt-496, VERSION 0.123.3.
  - Review is complete: three rounds, then the owner's ruling (ib-12a9cff30e05, "shrink the text box on phones", done in 1047770 and verified). Every thread is resolved.
  - **CI `check` is red.** At 360px chip 3's × measures 300.78 against a limit of 300.5. CI fonts render wider than the container's, so the 5ch floor has no margin.
  - The worker (in wt-496) was sent to give the `@media (max-width: 400px)` rule ~4px of headroom without loosening the test. If its report is lost, brief a fresh worker from that.
  - **Done in 4b8ddd1 (pushed):** the phone rule is now `min-width: 3ch; padding: 0 4px`; the test tolerance is unchanged; `make check` is green locally. Next: confirm CI is green, check `reviewThreads` (should be 0), then merge. No re-review is needed: it is a single-rule CSS change inside the ruling, and the test is unchanged.
- **#506 i-pasted-numbers (#502)**, branch fix/502-pasted-numbers, wt-502, head d29c9a5, VERSION 0.123.3, ParserVersion 18.
  - Three review rounds are done and the threads from all of them are resolved.
  - One SHOULD survives the cap, and it is text loss. The new trailing-enumerator strip in `stripStepNumber` (`internal/ingest/assemble.go:368`) drops a lone trailing number after a sentence end: "1. Bake. 350." gives "Bake.". It is escalated to the owner as ib-6cb95393a777, with options:
    - next-number (recommended): strip only the next number in the run;
    - drop-strip: remove the trailing strip;
    - merge-follow-up.
  - Act on the answer with a fresh worker in wt-502, a verification review, then resolve the open thread.
- #505 and #506 both hold 0.123.3. Whichever merges second re-bumps to 0.123.4 by merging origin/main.
- **Process miss:** #507 was merged with its declined-MUST thread still open. The thread was resolved after the merge, with evidence. Before any merge, query `reviewThreads` for unresolved threads, even when the decline was posted as a PR comment.

**Next to dispatch, in order:**
1. **Split a-api-rest (#471)**, a dispatcher graph PR. It is a placeholder (owns only `internal/api/README.md`) and is unblocked now that a-api-recipes has merged. ADR-00043: one node per resource, each creating `internal/api/<resource>`, switching web and MCP, and adding a depguard rule. Proposed nodes, from the depguard exceptions in `.golangci.yml` and a grep of domain imports in `internal/web` and `internal/mcp` on 2026-11-06:
   - **a-api-journal**: `internal/web/journal/{handler,view}.go` and `journal_templ.go` read `store` directly. Creates `internal/api/journal` and removes those three exceptions.
   - **a-api-sharing**: `internal/web/sharing/handler.go` reads `store` and `internal/sharing`. Probably extends `internal/api/recipes`, or adds `internal/api/sharing`; decide in the node.
   - **a-api-pictures**: `internal/web/picture/picture.go` calls the picture service directly. Route it through `apirecipes` (`NewPictures` exists) and remove its exception.
   - **a-api-catalog**: `internal/catalog` is imported by web/catalog, editor, cook, history, recipeview, ingest, operator and mcp. The largest one; make it its own round.
   - **a-api-households**: `internal/household` and `apikey` are imported across web/household, profile, the middleware, layout and mcp. The middleware's membership check stays at the adapter (ADR-00043 "Authentication stays at the adapters"), so scope this carefully.
   - **a-api-profile**: profile, session and oauth grants under web/profile.
   - **Last, a-api-status-table**: `internal/web/layout/httperr.go` names domain sentinels. Once every resource re-exports its errors, switch it to the `api` aliases and drop the exception. It depends on all the above.
   Before writing nodes, check each against the code. Give each a lane, a dependency on a-mcp-caller (#509) for any that touch `internal/mcp`, and the ADR-00043 acceptance (depguard rule plus web and MCP switch). Then close #471 or remap it to the first node. Ingest is already a library the adapters call (ADR-00043 note in docs/design.md); confirm with the owner that it needs no node.
2. **a-mcp-caller (#509)** and **u-aisle-suggest (#508)** once #510 merges (u-aisle-suggest waits on #505).
3. **o-demo-reseed (#432)** after the owner deploys.

**Owner, deployed:** the VM ran 0.121.1 at the start of this round, and main is at 0.123.2+. A deploy unblocks o-demo-reseed and lets the owner check step splitting and qualifier case on real imports.

**Lessons this round:**
- **Test against the real page.** The owner's browser-saved halfbakedharvest page caught a `9×13`/`9x13` mismatch that the hand-built fixture hid. When a site blocks scripted fetches, ask the owner for a browser save before accepting a synthetic fixture.
- **A worker reporting "waiting on monitor" in a loop spams the dispatcher.** Tell workers to run `make check` in the foreground with no monitors or poll loops; TaskStop a worker that keeps waking.
- **Review agents share `.cc-scratch/`.** One overwrote the dispatcher's PR-body file. Give dispatcher files a distinct prefix (`prbody-<n>.md`).
- **The host plugin mirror** is `/Users/timothygunter/.odette/home/default/gobbler-58ff2639/.claude/plugins/cache/market/overseer/0.20.1`. `bun` and `gh` exist only on the host.
- **Review a re-review against the pushed head, and say which head.** #507's MUST came from reviewing a head that predated the rework it described.

## Round 24 (2026-11-06): author notes, shopping API pilot, card width

**Merged** (squash SHA; under the standing authority):

| Node | PR | SHA | Schema | VERSION |
|---|---|---|---|---|
| (graph: a-api-shopping lane, i-reimport wave 13) | #474 | e5b7fc3 | none | none |
| (graph: a-api-shopping owns mcp_test.go) | #476 | 84671aa | none | none |
| (graph: picturedb shared generated) | #478 | 620d367 | none | none |
| u-card-width | #475 | f671446 | none | 0.119.1 |
| a-api-shopping | #477 | de00177 | none | 0.119.2 |

| r-author-notes | #479 | 05f2588 | 00031 | 0.120.0 |

| (ADR-00042 correction, owner ruling ib-89c94bbedfa8) | #480 | d031bc0 | none | none |
| (graph: i-reimport wiring test, web.go exception) | #482 | 60c51c9 | none | none |
| i-reimport | #481 | 1f37b72 | 00032 | 0.121.0 |

| (graph: m-picture-user-agent node) | #484 | ae45bab | none | none |
| m-picture-user-agent | #485 | 621c0b3 | none | 0.121.1 |
| (graph: a-api-recipes lane) | #486 | ebd89ca | none | none |

| (graph: m-picture-user-agent, a-api-recipes lane, caller, u-editor-suggest-list, i-step-numbers, i-split-steps) | #484 #486 #487 #489 #491 #493 | | none | none |

**Open PRs (review agents were running at handoff; read their posted reviews on GitHub):**
- **#494 a-api-recipes (#470)**, branch refactor/470-api-recipes, VERSION 0.121.2. Large: internal/api/caller
  extracted (shopping moved onto it), internal/api/recipes, all recipe web packages + MCP switched,
  depguard `recipes-through-api` with five file exceptions for a-api-rest (#471). Picture stays a domain
  package. Worker's out-of-lane home.go line sanctioned (#493). Follow-up for a-api-rest:
  tools_shopping.go shopCaller duplicates callerOf.
- **#495 i-step-numbers (#490): MERGED** eff2976, VERSION 0.122.0, ParserVersion 15. i-split-steps (#492) is now dispatchable.
- **Review state at handoff:**
  - #495: APPROVED with one SHOULD (fallback.go:297 strip untested); sent back to the worker for a test,
    worktree wt-490.
  - #494: CHANGES_REQUESTED, one MUST: golangci-lint v2 skips generated files, so depguard never sees
    *_templ.go; journal_templ.go imports store unexcepted and docs/design.md:107 overclaims. Sent back,
    worktree wt-470. NIT, out of lane: internal/web/reimport_wiring_test.go:27's comment still names
    mountMCP's store (now the router-built recipe service); fix it in a-api-rest or a small node.
  - If either worker's report did not arrive before this session ended, read the branch, re-brief a
    fresh worker from the review threads, and resolve each thread after verifying the fix.
- Both bump from 0.121.1: whichever merges second re-bumps against main (merge origin/main, no rebase).
- Triage: send findings to a fresh worker (the old ones are gone with this session); recreate the worktree
  from the pushed branch and `cd e2e && npm ci`.

**Queued (dispatch after the above):**
- i-split-steps (#492), after i-step-numbers: halfbakedharvest publishes its method as one HowToStep /
  one WPRM instruction of display-block spans with inline "1. … 2. …". Split by structure, then by an
  ascending enumerator run; reuse #490's pattern. Issue body has the evidence. (Sally's dinner rolls were
  a mix-up: that page parses into 10 steps fine.)
- u-editor-suggest-list (#488), after a-api-recipes: datalist arrow lists only the current value.

- **#496, no node yet:** the qualifier chip's × escapes its pill when two chips share a row (owner
  screenshot in feedback/, untracked). Editor lane, so add a node after u-editor-suggest-list. The same
  screenshot shows parser issues to triage into their own issue: "°F" lowercased, "(43°C)" and "whole"
  dropped from "1 cup (240ml) whole milk, warmed to about 110°F (43°C)", and the unrecognised-text
  warning firing although the text became qualifiers.
- #494 fix 1b3bc52 (depguard reads generated files via linters.exclusions.generated: disable plus an
  exclusion for every other linter on templ/sqlc output) is pushed; #495 has merged, so #494 must merge origin/main, re-bump VERSION to 0.122.1 and rewrap the long docs/design.md
  line, then push, resolve the MUST thread and merge.

**Owner, deployed:** VM runs 0.121.1 (GOBBLER_VERSION in /opt/gobbler/.env; nano needs
TERM=xterm-256color). Picture fix (#483) to be confirmed by re-importing the beer bread page.

**Lessons this round:**
- Tightening an auth check: list every credential kind first (ADR-00031). The dispatcher's own send-back
  broke household keys on #477; review caught it.
- The contract's shared-file list is now generated from graph.json into the scratchpad; hand-copying
  missed tenancy-support.ts and picturedb.
- Workers must not file owner inbox items; lane questions come to the dispatcher (a-api-recipes did).
- Check what the VM runs before debugging a deployed fix: `docker compose images` (#483 was 0.121.0).
- Confirm which URL the owner means before investigating; two imports close together got crossed.
- Probe the real pipeline offline with a throwaway _test.go in a detached worktree (newEnv/serve helpers
  in internal/ingest/helpers_test.go); container DNS blocks live fetches, so run live probes on the host.

- **a-api-shopping pattern** (for a-api-recipes): `Service.Op(ctx, Caller, ...)`; `Caller{Person,
  Household, HouseholdKey}`; `enter()` checks membership with `household.ForEmail` unless the caller is
  a household key (ADR-00031); `New` returns an error on a missing collaborator; errors map through
  `layout.statusTable` and MCP `toolError`. depguard `shopping-through-api` has a file exception for
  `internal/web/editor/build.go`, which a-api-recipes (#470) removes.
- The review caught a regression the dispatcher's own send-back caused: requiring a person refused
  household API keys. **Lesson:** when tightening an auth check, list every credential kind that
  reaches it (ADR-00031: personal key, household key, OAuth grant, session) before sending the brief.
- **Lesson:** the contract's shared-file list was hand-copied and missed `tenancy-support.ts` and
  picturedb. Generate it from `shared_files` instead.
- Labels: #469, #462 moved dor:blocked to dor:ready; #395 gained dor:ready.

**Next:** a-api-rest (#471) splits into per-resource nodes once a-api-recipes sets the pattern.
o-demo-reseed (#432) still waits on a deploy. Schema migrations last used 00032.

## Round 23 (2026-11-06): follow-ups, delete, suggestion cards, three decisions

Closed at the owner's request ("write a handoff, don't start net new work"); the in-flight PRs were
finished and merged, nothing new was dispatched.

**Merged** (squash SHA; under the standing authority):

| Node | PR | SHA | Schema | VERSION |
|---|---|---|---|---|
| (graph: #441/#447 nodes) | #450 | 2ca7ef0 | none | none |
| u-picker-icons | #451 | 3421fea | none | 0.117.1 |
| (ADR-00041 author notes, mockup variant A) | #458 | 5a4f0dd | none | none |
| r-credential-describe | #452 | 0f487cc | none | 0.117.2 |
| c-import-copy | #461 | e31e0bf | none | 0.117.3 |
| u-home-suggest-card | #465 | 0fcc0fc | none | 0.118.0 |
| u-recipe-delete | #460 | 0f33604 | none | 0.119.0 |
| (ADR-00042 re-import) | #466 | d9c5001 | none | none |
| l-discrete-api (ADR-00043) | #473 | 5270f0c | none | none |
| (graph: lanes, nodes) | #453, #457, #459, #463, #464, #467, #472 | | none | none |

- **#460** returns 409 "this recipe is on a shopping list and must be removed from it before it can be
  deleted" (`store.ErrOnShoppingList`, one row in httperr.go); one `.btn.danger` rule now lives in
  app.css (journal and recipeview copies removed); the confirmation script is `static/recipe-delete.js`.
- **#465** changed both suggestions (home and library side panel); `ribbon.Inline` deleted; the 360px
  home cap is deferred to u-card-width (#468).

**Open:** no PRs, no worktrees, no workers. `.claude/worktrees/mascot-check` is the owner's PNGs.

**Migration numbers:** schema last used 00030; none handed out. r-author-notes needs 00031 at dispatch.
**ADR numbers:** 00041, 00042, 00043 all merged.

**Decisions this round (owner, via /overseer:decide; don't re-ask):**
- **Author notes, ADR-00041** (`docs/proposals/author-notes.md` D1–D5): ordered list on the recipe,
  recipe level only; one paragraph, at most 400 characters, enforced in validation, long or junk
  candidates flagged low-confidence at ingest; they end the step chain, each credited "From the author"
  (mockup variant A in `docs/ui/mockup.html`); "Author's Notes" for the cook-mode button and sections.
- **Re-import, ADR-00042** (`docs/proposals/reimport.md` D1–D5): runs the whole ingest flow, only the
  save differs (new revision of the existing recipe, refused if it moved on); no pairing or change
  summary; picker defaults to "Keep current picture"; "Re-import from source" POST button with the
  action links, URL recipes only; MCP tool may start one, draft held for review.
- **Discrete API, ADR-00043** (`docs/proposals/discrete-api.md` D1–D5): in-process
  `internal/api/<resource>` that web and MCP call; JSON API later as an adapter; auth at adapters,
  membership in the service; one resource per node fenced by depguard; shopping pilot, then recipes.
  Owner's "scaling" driver doesn't hold over one SQLite file; said so, owner accepted.
- **Recipe delete:** danger button below History/Edit/Share at the bottom of the recipe page; type
  `DELETE` to confirm; redirect to the library.
- **Suggestion cards:** both the home and the library side-panel suggestions are library cards, home
  at library-card width; voice line above, "Another idea" below.
- **Copy:** removed the two "you review the result before anything is saved" lines (#461).

**Next to dispatch** (all ready; lanes checked against each other by validate.ts):
1. r-author-notes (#395): assign migration 00031. Brief with ADR-00041's obligations and mockup variant
   A as the visual target, checked side by side.
2. a-api-shopping (#469): the ADR-00043 pilot; independent of r-author-notes' lane.
3. u-card-width (#468): small; app.css plus `internal/web/recipes/style.go`.
4. i-reimport (#462): unblocked by ADR-00042, but waits on r-author-notes.

**Unmapped issues:** none; every open issue has a node.

**For the owner:** o-demo-reseed (#432) still waits on a deploy; main is 0.119.0.

**Lessons:**
- **Graph PRs merge on `validate.ts` alone**, no review agent and no CI wait. The owner repeated this
  (round 11 memory `feedback-mechanical-graph-prs`). Read `.memory/` at session start.
- **The host has overseer 0.18.2, the container 0.20.1.** Run host-side scripts from
  `~/.claude/plugins/cache/market/overseer/0.18.2/`.
- **A long bridge command can outlive a branch switch.** A background `gh pr merge … && git pull` ran
  while the primary checkout sat on a graph branch. Don't pull in background jobs; pull after.
- **Workers stop silently mid-gate.** #460's worker went quiet after a red lint for 30 minutes; only
  its `.cc-scratch` log showed it. Have workers write gate output to `.cc-scratch/` and watch for `exit=`.
- **Two docs PRs that each add an ADR index row conflict.** Merge one, rebase the other.
- **Ask which instance the owner means.** "The suggestion" existed in two places; the worker changed
  one. Confirm the target before dispatching UI feedback.
- **ADR workers found real gaps** (pairing keys, lost confidence marks, ADR-00035 snapshot rules). The
  owner's answer reshaped re-import. Have the ADR worker report contradictions before the PR opens.

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
| u-picture-cards | #448 | 9b51b4a | none | 0.117.0 |

**Open:**

| Node | Issue | PR | State |
|---|---|---|---|
| (none) | | | |

**Worktrees:** none.

**Migration numbers:** schema 00029 to a-mcp-recipe-edit, 00030 to s-picture-store. **ADR numbers:** 00040 to m-fetch-guard.

**Follow-up issues filed:** #441 (Describe hard-codes credential kind and owner names), #447 (picker icons into `layout.Icon`; one source for the editor form id).

**Owner calls this round (don't re-ask):**
- **#421 loopback:** option A. Fetches may reach loopback (127.0.0.0/8, ::1) only while the dev auth bypass is on; every other private range stays refused; the OAuth metadata fetch keeps refusing loopback. ADR-00040.
- **Picture mockup approved as drawn.** Candidate minimum 1066 × 300.
- **Person id in media keys is the profile `address`**; accepted in #439.
- **Owner wants speed:** stack dependent nodes on unmerged branches rather than wait for merges; the dispatcher re-stacks with cherry-pick after each merge.

**Next:** the picture feature is complete; o-demo-reseed (#432) becomes dispatchable, and r-author-notes (#395). l-discrete-api needs its decide session; i-site-extractors and l-meal-planning stay parked.

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
