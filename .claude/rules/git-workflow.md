# Git workflow

> **Always on** for anything that becomes a branch, a commit or a pull request.

## Trunk-based, short-lived branches

`main` is the trunk. Work happens on a short-lived branch off `main` and lands
through a pull request on `kaecyra/gobbler`, merged by squash. Rebase on `main`
before starting and again before opening the pull request; parallel work is
the default here.

- One pull request is one coherent concern — a module, a boundary, a behaviour
  — never a whole feature. The reviewer has to hold a complete mental model of
  how the system works, and a large diff erodes it. This bounds *scope*, not
  repairs: a broken window you fix on the way past belongs in the branch, in
  its own commit — see
  [collaboration](./collaboration.md#broken-windows-get-fixed).
- Create the branch **before** making any commits.
- Delete the branch after merge, remote **and** local.

## Branch naming

`<type>/<issue>-<slug>`, where `<type>` is a Conventional Commits type and
`<issue>` is the GitHub issue number: `feat/12-ingredient-parser`,
`fix/31-range-scaling`.

## Commits

[Conventional Commits](https://www.conventionalcommits.org/): `feat`, `fix`,
`refactor`, `test`, `docs`, `chore`. Use the Go package as the scope where one
applies: `feat(ingest): parse unicode fractions`.

Write the body for someone reading it in six months with no memory of the
conversation: what changed, and why this way rather than the obvious
alternative. A message that only restates the diff is wasted.

No AI attribution anywhere: no `Co-Authored-By` trailer, no "Generated with"
line, no session link — not in commits, pull request bodies or issue comments.

## Committing and pushing are user-initiated

Do not stage, commit, push or open a pull request unless asked, or unless an
active skill's pipeline does it. Local work in progress is yours; publishing is
a decision someone else makes.

The moment this goes wrong is not when you disagree with the rule. It is when
the next change is finished and asking feels redundant — "the tests pass, ready
to commit" is the whole move. Permission for one push does not carry forward to
the next change, however ready that change is.

## Gates

> The `Makefile` targets and hooks below are created by the foundation (W0)
> work. Until it lands they do not exist; do not report them as passing.

| Target | Runs | When |
|---|---|---|
| `make fmt` | `gofmt`, `goimports` (auto-fix) | pre-commit hook |
| `make lint` | `golangci-lint`, `go vet` | pre-push hook |
| `make test` | `go test -cover ./...`, failing below 70% line coverage | pre-push hook |
| `make e2e` | Playwright against a running binary; every route has a test | pre-push hook |
| `make check` | all of the above plus the version-bump check | before every push |

Hooks are managed by `lefthook`. No `--no-verify`.

A red gate is a reason to stop, not to bypass. If a gate is wrong, fix the
gate.

## Versioning

SemVer, in a `VERSION` file at the repository root, starting at `0.1.0`.

- Bump **once per shipped unit** — a pull request, or a standalone commit
  pushed straight to `main` — inside the branch, not as a follow-up.
- While at `0.x`: a `feat` bumps minor, a `fix` or `refactor` bumps patch.
- Docs, ADRs, `.memory/` and other non-source changes never bump.
- The pre-push hook blocks a push that changes source without a bump.
