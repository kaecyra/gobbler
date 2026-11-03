# gobbler

A self-hosted recipe manager for households. It imports recipes from URLs,
pasted text and photos, stores them as structured data, and builds merged,
aisle-grouped shopping lists. One Go binary over a SQLite file.

| To learn about | Read |
|---|---|
| Deploying, configuring, backups and restore | [docs/operations.md](./docs/operations.md) |
| How it fits together | [docs/design.md](./docs/design.md) |
| What was decided, and why | [docs/adr/](./docs/adr/README.md) |
| Rules for contributors and agents | [AGENTS.md](./AGENTS.md) |

## Run

```sh
cp .env.example .env   # set GOBBLER_VERSION and the GOBBLER_OIDC_* settings
docker compose pull && docker compose up -d
```

Port 8080 is published on every interface of the host. Restrict it in the
firewall. See [docs/operations.md](./docs/operations.md) for the rest.

## Develop

You need Go at the version in `go.mod`, Node 22 or later for the end-to-end
suite, and [lefthook](https://lefthook.dev/). Set up once per clone:

```sh
make tools                                               # pinned golangci-lint
(cd e2e && npm ci && npx playwright install chromium)    # end-to-end suite
lefthook install                                         # git hooks
```

Run it locally without sign-in, as a seeded household member, at
http://127.0.0.1:8080. The database goes in `./data/`.

```sh
GOBBLER_DEV_AUTH_BYPASS=true go run ./cmd/gobbler serve
```

| Command | Does |
|---|---|
| `make fmt` | `gofmt` and `goimports`, rewriting files |
| `make generate` | Regenerates the sqlc and templ output |
| `make lint` | `golangci-lint` and `go vet` |
| `make test` | `go test`, failing below 70% line coverage |
| `make e2e` | Builds gobbler and runs the Playwright suite |
| `make check` | Everything above, plus the staleness and version checks |

The pre-commit hook runs `make fmt`. The pre-push hook runs `lint`, `test`,
`e2e` and `version-check`. A source change needs a `VERSION` bump. Docs and
ADRs don't.
