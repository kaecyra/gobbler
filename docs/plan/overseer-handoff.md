# Overseer handoff — 2026-10-12

Volatile state the graph does not hold. `docs/plan/graph.json` is the durable record. `VERSION` on main is 0.22.0.

## Merged this session (squash SHA, PR)

| Node | Issue | PR | SHA |
|---|---|---|---|
| w1-store | #11 | #56 | ff3a996 |
| w7-backup | #34 | #64 | becbbb7 |
| w2-ingest-pipeline | #15 | #67 | f89e03e |
| w5-llm-contract | #26 | #68 | d3e6c08 |
| w5-llm-gemini | #28 | #69 | b26190c |
| w5-llm-claude | #27 | #70 | 989e23f |
| w0-e2e-harness | #6 | #58 | 1962cb1 |
| w5-llm-unify | #71 | #75 | b223254 |
| w0-container | #7 | #76 | f81208f |

Dispatcher PRs merged:
- #61: ADR-00014 (sqlc) and ADR-00015 (immutability lives at the store API; no triggers).
- #62: added the w2-sqlc node.
- #63: made w4-shopping-store depend on w2-sqlc.
- #72: e2e-only Go tooling lives under `e2e/` (ADR-00013 corrected, ADR-00002 scoped).
- #73: added the w5-llm-unify, w3-unit-owner and w3-store-atomic-ingest nodes.
- #74: VERSION lesson.

## Open PRs. None are reviewed yet.

| PR | Node | Branch / worktree | Next step |
|---|---|---|---|
| #79 | w2-sqlc (#60, closes #55, #59) | feat/60-sqlc, `.claude/worktrees/wt-60` | `/sdlc-review-pr 79`. At merge, rebase and re-bump VERSION to one patch over main |
| #80 | w2-review-screen (#16) | feat/16-review-screen, `wt-16` | Review. Carries an **owner question: CSRF** (below). Rebase; re-bump to one minor over main |
| #81 | w3-recipe-view (#18) | feat/18-recipe-view, `wt-18` | Review. Rebase; re-bump. #80 and #81 both append to `internal/web/routes.go` and `layout/httperr.go`, so the second one to merge rebases |
| #77 | dispatcher: ignore `.env` | chore/gitignore-env (no worktree) | Review found 1 MUST: the version gate counts `.gitignore` as source. Bump VERSION one patch over main, or exempt `.gitignore` in `check-version-bump.sh` (that file is outside the dispatcher's lane, so it would need its own change) |

The workers for #79, #80 and #81 belonged to this session. Send any review fixes to a **fresh** worker, pointed at the branch and the PR threads.

## Owner questions

1. **CSRF.** The web layer has no CSRF protection, and the ingest POSTs (#80) inherit that. A cross-site POST could trigger a fetch, even behind Access. This needs an ADR before more POST routes land (editor, journal, settings).

## Worktrees on disk

`wt-60` (w2-sqlc), `wt-16` (w2-review-screen), `wt-18` (w3-recipe-view). All three are provisioned with Go modules, e2e `node_modules` and Playwright chromium.

## Migration numbers

`schema`: `00002` was the last used (w1-store, merged). `w3-unit-owner` will draw `00003`.

## Follow-up issues filed this session (no node unless stated)

- #65: unit-alias owner. Node `w3-unit-owner`, after w2-sqlc.
- #66: atomic create-with-snapshot. Node `w3-store-atomic-ingest`, after w2-sqlc.
- #78: scripted check that the image has no Node and no `e2e/` (ADR-00013 obligation 1). Lives in Makefile/CI. Needs a node.

## Stale claims reported by workers and not yet fixed

- `README.md:22`: the `make check` description omits `generate-check` (after #79).
- `.claude/rules/git-workflow.md`: the Gates table has no `generate-check` row (after #79).
- `internal/db/db.go`: the package doc omits the tx helpers (after #79; this file is in w2-sqlc's lane, so it can be fixed in #79).
- `internal/web/routes.go`: the "this node imports none" comment above `mountFeatures` is false.

## Things a fresh dispatcher must know

- **VERSION.** Each PR is one bump over main *at merge*. Never reserve numbers ahead. See `.claude/rules/lessons.md`.
- **Review findings** need a named obligation (lessons.md). Decline anything that hardens against hypothetical code.
- **Gates on the host:** `make fmt-check test test-scripts e2e-routes e2e version-check` (plus `generate-check` once #79 merges), then pinned lint `GOTOOLCHAIN=go1.26.4 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run ./...` and `go vet ./...`. `make lint` refuses the host's 2.13.2.
- **Worktree setup.** `.claude/overseer/config.json` still has an empty `worktree_setup_command`. Until it is set, provision by hand: `go mod download && cd e2e && npm ci && npx playwright install chromium`, run on the host.
- **e2e ports.** Concurrent workers clash on the default ports 38417/38418. Give each worker distinct `GOBBLER_E2E_PORT` and `GOBBLER_E2E_ACCESS_PORT` values.
- **Host `sed` is BSD.** Workers editing files by script through the bridge should use perl or python.
- **Dispatcher approvals.** For narrow fixes, the dispatcher may verify the diff and post its own `<!-- self-review-verdict: APPROVED -->` COMMENT review, instead of running another full review round.

## Next to dispatch, and why

1. **Merge #79 (w2-sqlc).** It unblocks w3-search-list, w4-shopping-store, w3-unit-owner and w3-store-atomic-ingest.
2. **w3-editor, w3-history, w3-journal, w3-catalog-page.** All are ready now, and their lanes don't overlap. Ask the owner about CSRF first, because they add POST routes.
3. **w7-settings (#35).** Ready. It needs the live backup Runner from `cmd/gobbler` (`backups.Runner`).
