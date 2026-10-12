# Overseer handoff — 2026-10-11

Volatile state the graph does not hold. `docs/plan/graph.json` is the durable record.

## Merged this session (squash SHA, PR)

| Node | Issue | PR | SHA |
|---|---|---|---|
| w5-usda-import | #31 | #54 | 61b2bfe |
| w0-web-shell | #5 | #53 | aa7d1f3 |
| w4-shopping-merge | #23 | #51 | 18fb4a2 |
| w2-structured-extract | #14 | #52 | 954c71f |

`VERSION` on main is 0.13.0. Earlier merges are in the graph's `done[]`.

## Open PRs — none merge under the standing authority yet

| PR | Node | Head | State | Next step |
|---|---|---|---|---|
| #57 | dispatcher (no node) | chore/plan-round2-lanes | Review-clean (APPROVED, threads resolved), plus two later commits: `e2e/.node-version` added to the w0-e2e-harness lane, and this handoff | **Held for the owner**: it widens a lane (escalation trigger 2). Owner merges, or rejects — then #58 must drop `e2e/.gitignore` and `e2e/.node-version` |
| #56 | w1-store (#11) | 69bdf52, VERSION 0.14.0 | Round-4 fixes pushed, **not re-reviewed**: rowid/_rowid_/oid re-key guard (`BEFORE UPDATE ON … WHEN NEW.id IS NOT OLD.id`) and Create taking the write lock first (concurrent-create test). Open thread PRRT_kwDOU-iivM6rP0eh, PRRT_kwDOU-iivM6rP5Y1, PRRT_kwDOU-iivM6rP5Y2 | Re-review (`/sdlc-review-pr 56`), resolve threads. Also carries an **escalated question** (below), so it goes to the owner even when clean |
| #58 | w0-e2e-harness (#6) | 0910ac7, VERSION 0.15.0 | Round-1 fixes pushed, **not re-reviewed**: route coverage now walks the real router (`e2e/fixtures/routes`, `chi.Walk`), version script reads `HEAD:VERSION` with `--no-renames` and strict SemVer, health tested against a second server with Access on. 12 threads from review 5484272074 still open | Re-review (`/sdlc-review-pr 58`); merge only after #57; check ownership from a checkout that has #57's graph |

Versions are assigned by planned merge order (#56 0.14.0, #58 0.15.0). The new version gate compares against the tip of main, so if the order changes, re-bump.

## Escalated questions waiting on the owner

1. **#57 lane change** — merge to give w0-e2e-harness `e2e/.gitignore` and `e2e/.node-version`, and to make `internal/web/layout/httperr.go` shared append-only. Recommended: merge.
2. **#56 DB-level immutability** (inbox item `ib-8e7251c15b0e`) — four review rounds on raw-SQL paths that overwrite or delete history (INSERT OR REPLACE, REPLACE on the parent, UPDATE OR REPLACE, rowid re-key). Options: A) stop after the rowid fix, triggers are best-effort defence in depth (recommended); B) also enable `PRAGMA recursive_triggers` in `internal/db` (separate change, outside the lane); C) keep hardening each path found. If the next re-review finds yet another path, do not send it back — wait for this answer.

## Worktrees on disk

- `.claude/worktrees/wt-11` — w1-store, branch `feat/11-store`
- `.claude/worktrees/wt-6` — w0-e2e-harness, branch `feat/6-e2e-harness` (has `e2e/node_modules` and Playwright installed)

Container git refuses both ("dubious ownership"); run git there through the host bridge. Their workers belonged to this session and cannot be messaged from a new one: send review fixes to a **fresh** worker, pointing it at the branch, the PR's open threads and this table.

## Migration numbers

`schema`: `00001` w1-catalog (merged), `00002` w1-store (#56, open). Next is `00003`.

## Issues opened this session

- #55 — one shared SQLite transaction / write-error helper (store duplicates catalog's `withTx`/`wrapWrite`). Needs a node.
- #59 — `catalog.MergeIngredients` reads before its first write (SQLITE_BUSY under concurrency). Needs a node; #55's helper is the natural home for a "begin with the write lock" option.

## Things a fresh dispatcher must know

- **origin is HTTPS** with the host `gh` credential helper (switched from SSH this session because the SSH agent kept refusing to sign). Push and fetch via the host bridge.
- **Gates until #58 merges:** `make fmt-check`, `make test`, and lint as `GOTOOLCHAIN=go1.26.4 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run ./...` plus `go vet ./...`, all on the host. After #58: `make check` (needs golangci-lint v2.14.0 on PATH — the host's Homebrew one is 2.13.2), and set `worktree_setup_command` in `.claude/overseer/config.json` to `go mod download && cd e2e && npm ci && npx playwright install chromium`.
- **Scratchpad paths are not visible on the host.** Pass PR bodies to `gh` on stdin (`--body-file -`).
- `node.ts status <id> in_review` can race GitHub's closing-issue link; wait a few seconds and retry.
- Reviews posted by `/sdlc-review-pr` keep finding something new each round on large PRs. Verify small, narrow fixes yourself, then reply on and resolve the thread, instead of running another full round.
- Carry-overs still open: w2-ingest-pipeline (#15) must give unit aliases one owner (`internal/parse/line_units.go` vs catalog aliases); w0-container (#7) must keep `e2e/` out of the image (ADR-00013 obligation 1).

## Next three to dispatch, and why

1. **w0-container (#7)** — next on the serial foundation track once #58 merges; deployable artefact.
2. **w7-backup (#34)** — independent lane, dependencies merged; personal data needs backups early.
3. **A node for #55 + #59** (shared transaction helper with write-lock-first) — two concurrency/duplication defects now sit in catalog and store; every later store package will copy whichever pattern exists.

Also dispatchable once #56 merges: everything that depends on w1-store (search, recipe UI, ingest pipeline persistence).
